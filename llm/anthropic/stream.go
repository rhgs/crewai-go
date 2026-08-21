package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/rhgs/crewai-go"
)

type streamMessagesRequest struct {
	Model       string    `json:"model"`
	MaxTokens   int       `json:"max_tokens"`
	Temperature float64   `json:"temperature"`
	System      string    `json:"system,omitempty"`
	Messages    []anthMsg `json:"messages"`
	Stream      bool      `json:"stream"`
}

// CallStream implements crewai.StreamingLLM via Anthropic SSE.
func (c *Client) CallStream(ctx context.Context, messages []crewai.Message) <-chan crewai.StreamChunk {
	ch := make(chan crewai.StreamChunk, crewai.DefaultStreamChanBuffer)
	go c.runStream(ctx, messages, ch)
	return ch
}

func (c *Client) runStream(ctx context.Context, messages []crewai.Message, ch chan<- crewai.StreamChunk) {
	defer close(ch)

	sendErr := func(err error) {
		select {
		case <-ctx.Done():
		case ch <- crewai.StreamChunk{Err: err}:
		}
	}

	if c.apiKey == "" {
		sendErr(fmt.Errorf("anthropic: missing API key (set ANTHROPIC_API_KEY or use WithAPIKey)"))
		return
	}

	var systemParts []string
	var msgs []anthMsg
	for _, m := range messages {
		switch m.Role {
		case crewai.RoleSystem:
			systemParts = append(systemParts, m.Content)
		case crewai.RoleAssistant:
			msgs = append(msgs, anthMsg{Role: "assistant", Content: m.Content})
		default:
			msgs = append(msgs, anthMsg{Role: "user", Content: m.Content})
		}
	}

	reqBody := streamMessagesRequest{
		Model:       c.model,
		MaxTokens:   c.maxTokens,
		Temperature: c.temperature,
		System:      strings.Join(systemParts, "\n\n"),
		Messages:    msgs,
		Stream:      true,
	}
	buf, err := json.Marshal(reqBody)
	if err != nil {
		sendErr(fmt.Errorf("anthropic: encoding request: %w", err))
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/messages", bytes.NewReader(buf))
	if err != nil {
		sendErr(fmt.Errorf("anthropic: creating request: %w", err))
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", apiVersion)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		sendErr(fmt.Errorf("anthropic: sending request: %w", err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		sendErr(fmt.Errorf("anthropic: unexpected status %d", resp.StatusCode))
		return
	}

	limited := &countingLimitReader{r: resp.Body, limit: crewai.MaxProviderResponseBytes}
	sc := bufio.NewScanner(limited)
	sc.Buffer(make([]byte, 0, 64*1024), crewai.MaxProviderResponseBytes)

	var textLen int
	var eventName string
	for sc.Scan() {
		if err := limited.Err(); err != nil {
			sendErr(err)
			return
		}
		line := sc.Text()
		if line == "" {
			eventName = ""
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue // ping / comment
		}
		if strings.HasPrefix(line, "event:") {
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		switch eventName {
		case "content_block_delta":
			var payload struct {
				Delta struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"delta"`
			}
			if err := json.Unmarshal([]byte(data), &payload); err != nil {
				sendErr(fmt.Errorf("anthropic: decoding stream chunk: %w", err))
				return
			}
			if payload.Delta.Text == "" {
				continue
			}
			textLen += len(payload.Delta.Text)
			select {
			case <-ctx.Done():
				return
			case ch <- crewai.StreamChunk{Delta: payload.Delta.Text}:
			}
		case "message_stop":
			select {
			case <-ctx.Done():
			case ch <- crewai.StreamChunk{Done: true}:
			}
			return
		case "error":
			var payload struct {
				Error struct {
					Type    string `json:"type"`
					Message string `json:"message"`
				} `json:"error"`
			}
			_ = json.Unmarshal([]byte(data), &payload)
			msg := payload.Error.Message
			if msg == "" {
				msg = "stream error"
			}
			sendErr(fmt.Errorf("anthropic: API error: %s", msg))
			return
		default:
			// message_start, content_block_start/stop, message_delta, ping — ignore
		}
	}
	if err := sc.Err(); err != nil {
		if err := limited.Err(); err != nil {
			sendErr(err)
			return
		}
		if ctx.Err() != nil {
			return
		}
		sendErr(fmt.Errorf("anthropic: reading stream: %w", err))
		return
	}
	if err := limited.Err(); err != nil {
		sendErr(err)
		return
	}
	if textLen > 0 {
		select {
		case <-ctx.Done():
		case ch <- crewai.StreamChunk{Done: true}:
		}
	}
}

type countingLimitReader struct {
	r     io.Reader
	n     int
	limit int
	err   error
}

func (c *countingLimitReader) Read(p []byte) (int, error) {
	if c.err != nil {
		return 0, c.err
	}
	n, err := c.r.Read(p)
	c.n += n
	if c.n > c.limit {
		c.err = crewai.ErrStreamResponseTooLarge
		return n, c.err
	}
	return n, err
}

func (c *countingLimitReader) Err() error { return c.err }

var _ crewai.StreamingLLM = (*Client)(nil)
