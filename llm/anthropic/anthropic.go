// Package anthropic implementa crewai.LLM para a API de Mensagens da
// Anthropic (Claude), usando apenas a biblioteca padrão.
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

// Client fala com a API de Mensagens da Anthropic.
type Client struct {
	apiKey      string
	model       string
	baseURL     string
	maxTokens   int
	temperature float64
	httpClient  *http.Client
}

// Option configura o Client.
type Option func(*Client)

// WithBaseURL define uma URL base alternativa.
func WithBaseURL(url string) Option { return func(c *Client) { c.baseURL = url } }

// WithMaxTokens define o máximo de tokens gerados por resposta.
func WithMaxTokens(n int) Option { return func(c *Client) { c.maxTokens = n } }

// WithTemperature ajusta a temperatura de amostragem.
func WithTemperature(t float64) Option { return func(c *Client) { c.temperature = t } }

// WithHTTPClient injeta um *http.Client customizado.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.httpClient = h } }

// WithAPIKey define a chave de API explicitamente.
func WithAPIKey(key string) Option { return func(c *Client) { c.apiKey = key } }

// New cria um cliente para o modelo Claude informado (ex.: "claude-sonnet-5").
// Se nenhuma chave for passada, usa a variável ANTHROPIC_API_KEY.
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

// Model implementa crewai.LLM.
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

// Call implementa crewai.LLM.
func (c *Client) Call(ctx context.Context, messages []crewai.Message) (string, error) {
	if c.apiKey == "" {
		return "", fmt.Errorf("anthropic: chave de API ausente (defina ANTHROPIC_API_KEY ou use WithAPIKey)")
	}

	// A API da Anthropic recebe o system prompt em um campo separado e só
	// aceita mensagens 'user' e 'assistant'.
	var systemParts []string
	var msgs []anthMsg
	for _, m := range messages {
		switch m.Role {
		case crewai.RoleSystem:
			systemParts = append(systemParts, m.Content)
		case crewai.RoleAssistant:
			msgs = append(msgs, anthMsg{Role: "assistant", Content: m.Content})
		default: // user e tool são mapeados para user
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
		return "", fmt.Errorf("anthropic: codificando requisição: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/messages", bytes.NewReader(buf))
	if err != nil {
		return "", fmt.Errorf("anthropic: criando requisição: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", apiVersion)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("anthropic: enviando requisição: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("anthropic: lendo resposta: %w", err)
	}

	var parsed messagesResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", fmt.Errorf("anthropic: decodificando resposta (status %d): %w", resp.StatusCode, err)
	}
	if parsed.Error != nil {
		return "", fmt.Errorf("anthropic: erro da API: %s", parsed.Error.Message)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("anthropic: status inesperado %d: %s", resp.StatusCode, string(data))
	}

	var b strings.Builder
	for _, blk := range parsed.Content {
		if blk.Type == "text" {
			b.WriteString(blk.Text)
		}
	}
	return b.String(), nil
}
