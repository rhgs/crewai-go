// Package ollama implements crewai.LLM for Ollama, both in local mode
// (http://localhost:11434, no authentication) and Ollama Cloud
// (https://ollama.com, token-authenticated), using the native /api/chat API
// and only the standard library.
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/rhgs/crewai-go"
)

const (
	// LocalBaseURL is the default address of a local Ollama instance.
	LocalBaseURL = "http://localhost:11434"
	// CloudBaseURL is the address of Ollama Cloud.
	CloudBaseURL = "https://ollama.com"
)

// Client talks to an Ollama server (local or cloud).
type Client struct {
	model       string
	baseURL     string
	apiKey      string // empty for local; Bearer token for cloud
	temperature float64
	httpClient  *http.Client
}

// Option configures the Client.
type Option func(*Client)

// WithBaseURL sets an alternative base URL (e.g. another machine on the
// network). Do not include a trailing slash.
func WithBaseURL(url string) Option { return func(c *Client) { c.baseURL = url } }

// WithAPIKey sets the token used for Ollama Cloud. Ignored in local mode.
func WithAPIKey(key string) Option { return func(c *Client) { c.apiKey = key } }

// WithTemperature adjusts the sampling temperature.
func WithTemperature(t float64) Option { return func(c *Client) { c.temperature = t } }

// WithHTTPClient injects a custom *http.Client.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.httpClient = h } }

// New creates a client for a LOCAL Ollama instance (no authentication).
//
//	llm := ollama.New("llama3.2")
func New(model string, opts ...Option) *Client {
	c := &Client{
		model:       model,
		baseURL:     LocalBaseURL,
		temperature: 0.7,
		httpClient:  &http.Client{Timeout: 300 * time.Second},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// NewCloud creates a client for Ollama Cloud. If no key is passed via
// WithAPIKey, the OLLAMA_API_KEY environment variable is used.
//
//	llm := ollama.NewCloud("gpt-oss:120b")
func NewCloud(model string, opts ...Option) *Client {
	c := &Client{
		model:       model,
		baseURL:     CloudBaseURL,
		apiKey:      os.Getenv("OLLAMA_API_KEY"),
		temperature: 0.7,
		httpClient:  &http.Client{Timeout: 300 * time.Second},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Model implements crewai.LLM.
func (c *Client) Model() string { return c.model }

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
	Options  chatOptions   `json:"options"`
}

type chatOptions struct {
	Temperature float64 `json:"temperature"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Message chatMessage `json:"message"`
	Done    bool        `json:"done"`
	Error   string      `json:"error"`
}

// Call implements crewai.LLM.
func (c *Client) Call(ctx context.Context, messages []crewai.Message) (string, error) {
	if c.baseURL == CloudBaseURL && c.apiKey == "" {
		return "", fmt.Errorf("ollama: Ollama Cloud requires a token (set OLLAMA_API_KEY or use WithAPIKey)")
	}

	reqBody := chatRequest{
		Model:    c.model,
		Stream:   false,
		Options:  chatOptions{Temperature: c.temperature},
		Messages: make([]chatMessage, len(messages)),
	}
	for i, m := range messages {
		reqBody.Messages[i] = chatMessage{Role: string(m.Role), Content: m.Content}
	}

	buf, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("ollama: encoding request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewReader(buf))
	if err != nil {
		return "", fmt.Errorf("ollama: creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama: sending request: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("ollama: reading response: %w", err)
	}

	var parsed chatResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", fmt.Errorf("ollama: decoding response (status %d): %w", resp.StatusCode, err)
	}
	if parsed.Error != "" {
		return "", fmt.Errorf("ollama: API error: %s", parsed.Error)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama: unexpected status %d: %s", resp.StatusCode, string(data))
	}
	return parsed.Message.Content, nil
}

// chatRequestWithTools extends chatRequest with a tools field.
type chatRequestWithTools struct {
	Model    string            `json:"model"`
	Messages []chatMessageFull `json:"messages"`
	Tools    []crewai.ToolSpec `json:"tools,omitempty"`
	Stream   bool              `json:"stream"`
	Options  chatOptions       `json:"options"`
}

// chatMessageFull extends chatMessage with tool_calls and tool_name.
type chatMessageFull struct {
	Role      string            `json:"role"`
	Content   string            `json:"content"`
	ToolCalls []crewai.ToolCall `json:"tool_calls,omitempty"`
	ToolName  string            `json:"tool_name,omitempty"`
}

// chatResponseFull extends chatResponse with tool_calls in the message.
type chatResponseFull struct {
	Message chatMessageFull `json:"message"`
	Done    bool            `json:"done"`
	Error   string          `json:"error"`
}

// CallWithTools implements crewai.ToolCallingLLM.
func (c *Client) CallWithTools(ctx context.Context, messages []crewai.Message, tools []crewai.ToolSpec) (*crewai.ToolCallResponse, error) {
	if c.baseURL == CloudBaseURL && c.apiKey == "" {
		return nil, fmt.Errorf("ollama: Ollama Cloud requires a token (set OLLAMA_API_KEY or use WithAPIKey)")
	}

	reqBody := chatRequestWithTools{
		Model:    c.model,
		Stream:   false,
		Options:  chatOptions{Temperature: c.temperature},
		Tools:    tools,
		Messages: make([]chatMessageFull, len(messages)),
	}
	for i, m := range messages {
		reqBody.Messages[i] = chatMessageFull{
			Role:      string(m.Role),
			Content:   m.Content,
			ToolCalls: m.ToolCalls,
			ToolName:  m.ToolName,
		}
	}

	buf, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("ollama: encoding request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewReader(buf))
	if err != nil {
		return nil, fmt.Errorf("ollama: creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama: sending request: %w", err)
	}
	defer resp.Body.Close()

	body := io.LimitReader(resp.Body, crewai.MaxProviderResponseBytes)
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("ollama: reading response: %w", err)
	}

	var parsed chatResponseFull
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("ollama: decoding response (status %d): %w", resp.StatusCode, err)
	}
	if parsed.Error != "" {
		return nil, fmt.Errorf("ollama: API error: %s", parsed.Error)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama: unexpected status %d: %s", resp.StatusCode, string(data))
	}

	return &crewai.ToolCallResponse{
		Content:   parsed.Message.Content,
		ToolCalls: parsed.Message.ToolCalls,
	}, nil
}

// Compile-time check.
var _ crewai.ToolCallingLLM = (*Client)(nil)
