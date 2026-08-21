package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/rhgs/crewai-go"
)

// CallStream implements crewai.StreamingLLM. Uses NDJSON with stream:true.
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

	if c.baseURL == CloudBaseURL && c.apiKey == "" {
		sendErr(fmt.Errorf("ollama: Ollama Cloud requires a token (set OLLAMA_API_KEY or use WithAPIKey)"))
		return
	}

	reqBody := chatRequest{
		Model:    c.model,
		Stream:   true,
		Options:  chatOptions{Temperature: c.temperature},
		Messages: make([]chatMessage, len(messages)),
	}
	for i, m := range messages {
		reqBody.Messages[i] = chatMessage{Role: string(m.Role), Content: m.Content}
	}
	buf, err := json.Marshal(reqBody)
	if err != nil {
		sendErr(fmt.Errorf("ollama: encoding request: %w", err))
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewReader(buf))
	if err != nil {
		sendErr(fmt.Errorf("ollama: creating request: %w", err))
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		sendErr(fmt.Errorf("ollama: sending request: %w", err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		sendErr(fmt.Errorf("ollama: unexpected status %d", resp.StatusCode))
		return
	}

	limited := &countingLimitReader{r: resp.Body, limit: crewai.MaxProviderResponseBytes}
	sc := bufio.NewScanner(limited)
	sc.Buffer(make([]byte, 0, 64*1024), crewai.MaxProviderResponseBytes)

	var textLen int
	var sawDone bool
	for sc.Scan() {
		if err := limited.Err(); err != nil {
			sendErr(err)
			return
		}
		line := sc.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var parsed chatResponse
		if err := json.Unmarshal(line, &parsed); err != nil {
			sendErr(fmt.Errorf("ollama: decoding stream chunk: %w", err))
			return
		}
		if parsed.Error != "" {
			sendErr(fmt.Errorf("ollama: API error: %s", parsed.Error))
			return
		}
		if parsed.Message.Content != "" {
			textLen += len(parsed.Message.Content)
			select {
			case <-ctx.Done():
				return
			case ch <- crewai.StreamChunk{Delta: parsed.Message.Content}:
			}
		}
		if parsed.Done {
			sawDone = true
			select {
			case <-ctx.Done():
			case ch <- crewai.StreamChunk{Done: true}:
			}
			return
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
		sendErr(fmt.Errorf("ollama: reading stream: %w", err))
		return
	}
	if err := limited.Err(); err != nil {
		sendErr(err)
		return
	}
	if !sawDone && textLen > 0 {
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
