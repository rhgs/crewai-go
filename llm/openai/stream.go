package openai

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

// streamChatRequest extends chatRequest with stream:true.
type streamChatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	Stream      bool          `json:"stream"`
}

// streamDelta is a partial OpenAI chat completion chunk.
type streamDelta struct {
	Choices []struct {
		Delta struct {
			Content *string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// CallStream implements crewai.StreamingLLM (D-S1). The channel is never nil.
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

	token, err := c.authToken(ctx)
	if err != nil {
		sendErr(err)
		return
	}

	reqBody := streamChatRequest{
		Model:       c.model,
		Temperature: c.temperature,
		Stream:      true,
		Messages:    make([]chatMessage, len(messages)),
	}
	for i, m := range messages {
		reqBody.Messages[i] = chatMessage{Role: string(m.Role), Content: m.Content}
	}
	buf, err := json.Marshal(reqBody)
	if err != nil {
		sendErr(fmt.Errorf("openai: encoding request: %w", err))
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(buf))
	if err != nil {
		sendErr(fmt.Errorf("openai: creating request: %w", err))
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		sendErr(fmt.Errorf("openai: sending request: %w", err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Drain a small prefix for status only; do not echo body.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		sendErr(fmt.Errorf("openai: unexpected status %d", resp.StatusCode))
		return
	}

	limited := &countingLimitReader{r: resp.Body, limit: crewai.MaxProviderResponseBytes}
	sc := bufio.NewScanner(limited)
	// SSE lines can be long; raise token size within the same overall body cap.
	sc.Buffer(make([]byte, 0, 64*1024), crewai.MaxProviderResponseBytes)

	var textLen int
	for sc.Scan() {
		if err := limited.Err(); err != nil {
			sendErr(err)
			return
		}
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			select {
			case <-ctx.Done():
			case ch <- crewai.StreamChunk{Done: true}:
			}
			return
		}
		var parsed streamDelta
		if err := json.Unmarshal([]byte(data), &parsed); err != nil {
			sendErr(fmt.Errorf("openai: decoding stream chunk: %w", err))
			return
		}
		if parsed.Error != nil {
			sendErr(fmt.Errorf("openai: API error: %s", parsed.Error.Message))
			return
		}
		if len(parsed.Choices) == 0 {
			continue
		}
		content := parsed.Choices[0].Delta.Content
		if content == nil || *content == "" {
			continue
		}
		textLen += len(*content)
		select {
		case <-ctx.Done():
			return
		case ch <- crewai.StreamChunk{Delta: *content}:
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
		sendErr(fmt.Errorf("openai: reading stream: %w", err))
		return
	}
	if err := limited.Err(); err != nil {
		sendErr(err)
		return
	}
	// Clean EOF without [DONE]: still signal Done if we got any text, else incomplete via bare close.
	if textLen > 0 {
		select {
		case <-ctx.Done():
		case ch <- crewai.StreamChunk{Done: true}:
		}
	}
}

// countingLimitReader counts bytes read and fails after limit.
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

// Compile-time check.
var _ crewai.StreamingLLM = (*Client)(nil)
