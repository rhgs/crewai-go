// Package openai implementa crewai.LLM para a API de Chat Completions da
// OpenAI e de qualquer endpoint compatível (Azure OpenAI, Groq, Together,
// Ollama, LM Studio, etc.), usando apenas a biblioteca padrão.
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

// TokenSource fornece um token Bearer dinâmico (ex.: um access token OAuth
// renovável). Quando definido, tem prioridade sobre a chave estática.
type TokenSource interface {
	// Token devolve um token de acesso válido, renovando-o se necessário.
	Token(ctx context.Context) (string, error)
}

// Client fala com um endpoint compatível com a API da OpenAI.
type Client struct {
	apiKey      string
	tokenSource TokenSource
	model       string
	baseURL     string
	temperature float64
	httpClient  *http.Client
}

// Option configura o Client.
type Option func(*Client)

// WithBaseURL define uma URL base alternativa (ex.: "http://localhost:11434/v1"
// para Ollama). Não inclua a barra final.
func WithBaseURL(url string) Option { return func(c *Client) { c.baseURL = url } }

// WithTemperature ajusta a temperatura de amostragem.
func WithTemperature(t float64) Option { return func(c *Client) { c.temperature = t } }

// WithHTTPClient injeta um *http.Client customizado (timeouts, proxy, etc.).
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.httpClient = h } }

// WithAPIKey define a chave de API explicitamente.
func WithAPIKey(key string) Option { return func(c *Client) { c.apiKey = key } }

// WithTokenSource usa um token Bearer dinâmico (ex.: OAuth) em vez de uma
// chave de API estática. Útil para autenticação por assinatura/OAuth.
func WithTokenSource(ts TokenSource) Option { return func(c *Client) { c.tokenSource = ts } }

// New cria um cliente para o modelo informado. Se nenhuma chave for passada
// via WithAPIKey, a variável de ambiente OPENAI_API_KEY é usada.
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

// Model implementa crewai.LLM.
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

// authToken resolve o token de autenticação (OAuth dinâmico tem prioridade
// sobre a chave estática).
func (c *Client) authToken(ctx context.Context) (string, error) {
	if c.tokenSource != nil {
		return c.tokenSource.Token(ctx)
	}
	if c.apiKey == "" {
		return "", fmt.Errorf("openai: credencial ausente (defina OPENAI_API_KEY, use WithAPIKey ou WithTokenSource)")
	}
	return c.apiKey, nil
}

// Call implementa crewai.LLM.
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
		return "", fmt.Errorf("openai: codificando requisição: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(buf))
	if err != nil {
		return "", fmt.Errorf("openai: criando requisição: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("openai: enviando requisição: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("openai: lendo resposta: %w", err)
	}

	var parsed chatResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", fmt.Errorf("openai: decodificando resposta (status %d): %w", resp.StatusCode, err)
	}
	if parsed.Error != nil {
		return "", fmt.Errorf("openai: erro da API: %s", parsed.Error.Message)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("openai: status inesperado %d: %s", resp.StatusCode, string(data))
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("openai: resposta sem choices")
	}
	return parsed.Choices[0].Message.Content, nil
}
