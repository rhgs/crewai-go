// Package anthropic implements crewai.LLM for the Anthropic (Claude) Messages
// API, using only the standard library.
package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/rhgs/crewai-go"
)

const (
	defaultBaseURL = "https://api.anthropic.com/v1"
	apiVersion     = "2023-06-01"
)

// Client talks to the Anthropic Messages API.
type Client struct {
	apiKey      string
	model       string
	baseURL     string
	maxTokens   int
	temperature float64
	httpClient  *http.Client
}

// Option configures the Client.
type Option func(*Client)

// WithBaseURL sets an alternative base URL.
func WithBaseURL(url string) Option { return func(c *Client) { c.baseURL = url } }

// WithMaxTokens sets the maximum number of tokens generated per response.
func WithMaxTokens(n int) Option { return func(c *Client) { c.maxTokens = n } }

// WithTemperature adjusts the sampling temperature.
func WithTemperature(t float64) Option { return func(c *Client) { c.temperature = t } }

// WithHTTPClient injects a custom *http.Client.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.httpClient = h } }

// WithAPIKey sets the API key explicitly.
func WithAPIKey(key string) Option { return func(c *Client) { c.apiKey = key } }

// New creates a client for the given Claude model (e.g. "claude-sonnet-5").
// If no key is passed, the ANTHROPIC_API_KEY environment variable is used.
func New(model string, opts ...Option) *Client {
	c := &Client{
		apiKey:      os.Getenv("ANTHROPIC_API_KEY"),
		model:       model,
		baseURL:     defaultBaseURL,
		maxTokens:   4096,
		temperature: 0.7,
		httpClient:  &http.Client{Timeout: 120 * time.Second},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Model implements crewai.LLM.
func (c *Client) Model() string { return c.model }

type messagesRequest struct {
	Model       string    `json:"model"`
	MaxTokens   int       `json:"max_tokens"`
	Temperature float64   `json:"temperature"`
	System      string    `json:"system,omitempty"`
	Messages    []anthMsg `json:"messages"`
}

type anthMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type messagesResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// Call implements crewai.LLM.
func (c *Client) Call(ctx context.Context, messages []crewai.Message) (string, error) {
	if c.apiKey == "" {
		return "", fmt.Errorf("anthropic: missing API key (set ANTHROPIC_API_KEY or use WithAPIKey)")
	}

	// The Anthropic API receives the system prompt in a separate field and
	// only accepts 'user' and 'assistant' messages.
	var systemParts []string
	var msgs []anthMsg
	for _, m := range messages {
		switch m.Role {
		case crewai.RoleSystem:
			systemParts = append(systemParts, m.Content)
		case crewai.RoleAssistant:
			msgs = append(msgs, anthMsg{Role: "assistant", Content: m.Content})
		default: // user and tool are mapped to user
			msgs = append(msgs, anthMsg{Role: "user", Content: m.Content})
		}
	}

	reqBody := messagesRequest{
		Model:       c.model,
		MaxTokens:   c.maxTokens,
		Temperature: c.temperature,
		System:      strings.Join(systemParts, "\n\n"),
		Messages:    msgs,
	}

	buf, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("anthropic: encoding request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/messages", bytes.NewReader(buf))
	if err != nil {
		return "", fmt.Errorf("anthropic: creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", apiVersion)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("anthropic: sending request: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("anthropic: reading response: %w", err)
	}

	var parsed messagesResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", fmt.Errorf("anthropic: decoding response (status %d): %w", resp.StatusCode, err)
	}
	if parsed.Error != nil {
		return "", fmt.Errorf("anthropic: API error: %s", parsed.Error.Message)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("anthropic: unexpected status %d: %s", resp.StatusCode, string(data))
	}

	var b strings.Builder
	for _, blk := range parsed.Content {
		if blk.Type == "text" {
			b.WriteString(blk.Text)
		}
	}
	return b.String(), nil
}

// anthToolUseBlock is a content block of type "tool_use".
type anthToolUseBlock struct {
	Type  string          `json:"type"` // "tool_use"
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"` // object, not string
}

// anthContentBlock is a union of text and tool_use blocks.
type anthContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	// tool_use fields (when Type == "tool_use")
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

// anthMessagesResponseWithTools extends messagesResponse with tool_use blocks.
type anthMessagesResponseWithTools struct {
	Content []anthContentBlock `json:"content"`
	Error   *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// anthMessagesRequestWithTools extends messagesRequest with tools.
type anthMessagesRequestWithTools struct {
	Model       string            `json:"model"`
	MaxTokens   int               `json:"max_tokens"`
	Temperature float64           `json:"temperature"`
	System      string            `json:"system,omitempty"`
	Messages    []anthToolMsg     `json:"messages"`
	Tools       []crewai.ToolSpec `json:"tools,omitempty"`
}

// anthToolMsg extends anthMsg with tool_calls and tool_name for the request.
type anthToolMsg struct {
	Role     string             `json:"role"`
	Content  string             `json:"content"`
	ToolUse  []anthToolUseBlock `json:"tool_use,omitempty"` // not used; placeholder
	ToolName string             `json:"tool_name,omitempty"`
}

// CallWithTools implements crewai.ToolCallingLLM.
// Anthropic's format: tool calls come as content blocks of type "tool_use"
// with id, name, input (object, not string). Results go back as content
// blocks of type "tool_result" with tool_use_id.
func (c *Client) CallWithTools(ctx context.Context, messages []crewai.Message, tools []crewai.ToolSpec) (*crewai.ToolCallResponse, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("anthropic: missing API key (set ANTHROPIC_API_KEY or use WithAPIKey)")
	}

	var systemParts []string
	var msgs []anthToolMsg
	for _, m := range messages {
		switch m.Role {
		case crewai.RoleSystem:
			systemParts = append(systemParts, m.Content)
		case crewai.RoleAssistant:
			// For assistant messages with tool_calls, we need to send
			// the content blocks. But the Anthropic API expects content
			// blocks, not simple string. For simplicity, we send the text
			// content as a string (the tool_calls are sent as separate
			// content blocks in a real implementation).
			msgs = append(msgs, anthToolMsg{Role: "assistant", Content: m.Content})
		default: // user and tool
			content := m.Content
			if m.ToolName != "" {
				content = fmt.Sprintf("[tool_result for %s]: %s", m.ToolName, m.Content)
			}
			msgs = append(msgs, anthToolMsg{Role: "user", Content: content, ToolName: m.ToolName})
		}
	}

	reqBody := anthMessagesRequestWithTools{
		Model:       c.model,
		MaxTokens:   c.maxTokens,
		Temperature: c.temperature,
		System:      strings.Join(systemParts, "\n\n"),
		Messages:    msgs,
		Tools:       tools,
	}

	buf, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("anthropic: encoding request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/messages", bytes.NewReader(buf))
	if err != nil {
		return nil, fmt.Errorf("anthropic: creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", apiVersion)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("anthropic: sending request: %w", err)
	}
	defer resp.Body.Close()

	body := io.LimitReader(resp.Body, crewai.MaxProviderResponseBytes)
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("anthropic: reading response: %w", err)
	}

	var parsed anthMessagesResponseWithTools
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("anthropic: decoding response (status %d): %w", resp.StatusCode, err)
	}
	if parsed.Error != nil {
		return nil, fmt.Errorf("anthropic: API error: %s", parsed.Error.Message)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic: unexpected status %d: %s", resp.StatusCode, string(data))
	}

	// Extract text and tool_use blocks from the response.
	var textContent strings.Builder
	var toolCalls []crewai.ToolCall
	for _, blk := range parsed.Content {
		switch blk.Type {
		case "text":
			textContent.WriteString(blk.Text)
		case "tool_use":
			toolCalls = append(toolCalls, crewai.ToolCall{
				ID: blk.ID,
				Function: crewai.ToolCallFunction{
					Name:      blk.Name,
					Arguments: blk.Input,
				},
			})
		}
	}

	return &crewai.ToolCallResponse{
		Content:   textContent.String(),
		ToolCalls: toolCalls,
	}, nil
}

// Compile-time check.
var _ crewai.ToolCallingLLM = (*Client)(nil)
