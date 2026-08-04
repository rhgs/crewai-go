// Package openai implements crewai.LLM for the OpenAI Chat Completions API and
// any compatible endpoint (Azure OpenAI, Groq, Together, Ollama, LM Studio,
// etc.), using only the standard library.
package openai

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

const defaultBaseURL = "https://api.openai.com/v1"

// TokenSource provides a dynamic Bearer token (e.g. a renewable OAuth access
// token). When set, it takes precedence over the static key.
type TokenSource interface {
	// Token returns a valid access token, renewing it if necessary.
	Token(ctx context.Context) (string, error)
}

// Client talks to an OpenAI-compatible endpoint.
type Client struct {
	apiKey      string
	tokenSource TokenSource
	model       string
	baseURL     string
	temperature float64
	httpClient  *http.Client
}

// Option configures the Client.
type Option func(*Client)

// WithBaseURL sets an alternative base URL (e.g. "http://localhost:11434/v1"
// for Ollama). Do not include a trailing slash.
func WithBaseURL(url string) Option { return func(c *Client) { c.baseURL = url } }

// WithTemperature adjusts the sampling temperature.
func WithTemperature(t float64) Option { return func(c *Client) { c.temperature = t } }

// WithHTTPClient injects a custom *http.Client (timeouts, proxy, etc.).
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.httpClient = h } }

// WithAPIKey sets the API key explicitly.
func WithAPIKey(key string) Option { return func(c *Client) { c.apiKey = key } }

// WithTokenSource uses a dynamic Bearer token (e.g. OAuth) instead of a static
// API key. Useful for subscription/OAuth authentication.
func WithTokenSource(ts TokenSource) Option { return func(c *Client) { c.tokenSource = ts } }

// New creates a client for the given model. If no key is passed via WithAPIKey,
// the OPENAI_API_KEY environment variable is used.
func New(model string, opts ...Option) *Client {
	c := &Client{
		apiKey:      os.Getenv("OPENAI_API_KEY"),
		model:       model,
		baseURL:     defaultBaseURL,
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

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// authToken resolves the auth token (dynamic OAuth takes precedence over the
// static key).
func (c *Client) authToken(ctx context.Context) (string, error) {
	if c.tokenSource != nil {
		return c.tokenSource.Token(ctx)
	}
	if c.apiKey == "" {
		return "", fmt.Errorf("openai: missing credentials (set OPENAI_API_KEY, use WithAPIKey or WithTokenSource)")
	}
	return c.apiKey, nil
}

// Call implements crewai.LLM.
func (c *Client) Call(ctx context.Context, messages []crewai.Message) (string, error) {
	token, err := c.authToken(ctx)
	if err != nil {
		return "", err
	}

	reqBody := chatRequest{
		Model:       c.model,
		Temperature: c.temperature,
		Messages:    make([]chatMessage, len(messages)),
	}
	for i, m := range messages {
		reqBody.Messages[i] = chatMessage{Role: string(m.Role), Content: m.Content}
	}

	buf, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("openai: encoding request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(buf))
	if err != nil {
		return "", fmt.Errorf("openai: creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("openai: sending request: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("openai: reading response: %w", err)
	}

	var parsed chatResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", fmt.Errorf("openai: decoding response (status %d): %w", resp.StatusCode, err)
	}
	if parsed.Error != nil {
		return "", fmt.Errorf("openai: API error: %s", parsed.Error.Message)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("openai: unexpected status %d: %s", resp.StatusCode, string(data))
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("openai: response with no choices")
	}
	return parsed.Choices[0].Message.Content, nil
}
