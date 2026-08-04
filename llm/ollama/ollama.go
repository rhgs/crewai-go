// Package ollama implementa crewai.LLM para o Ollama, tanto na modalidade
// local (http://localhost:11434, sem autenticação) quanto no Ollama Cloud
// (https://ollama.com, autenticado por token), usando a API nativa /api/chat
// e apenas a biblioteca padrão.
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
	// LocalBaseURL é o endereço padrão de uma instância local do Ollama.
	LocalBaseURL = "http://localhost:11434"
	// CloudBaseURL é o endereço do Ollama Cloud.
	CloudBaseURL = "https://ollama.com"
)

// Client fala com um servidor Ollama (local ou cloud).
type Client struct {
	model       string
	baseURL     string
	apiKey      string // vazio para local; Bearer token para o cloud
	temperature float64
	httpClient  *http.Client
}

// Option configura o Client.
type Option func(*Client)

// WithBaseURL define uma URL base alternativa (ex.: outra máquina na rede).
// Não inclua a barra final.
func WithBaseURL(url string) Option { return func(c *Client) { c.baseURL = url } }

// WithAPIKey define o token usado no Ollama Cloud. Ignorado no uso local.
func WithAPIKey(key string) Option { return func(c *Client) { c.apiKey = key } }

// WithTemperature ajusta a temperatura de amostragem.
func WithTemperature(t float64) Option { return func(c *Client) { c.temperature = t } }

// WithHTTPClient injeta um *http.Client customizado.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.httpClient = h } }

// New cria um cliente para uma instância LOCAL do Ollama (sem autenticação).
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

// NewCloud cria um cliente para o Ollama Cloud. Se nenhuma chave for passada
// via WithAPIKey, usa a variável de ambiente OLLAMA_API_KEY.
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

// Model implementa crewai.LLM.
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

// Call implementa crewai.LLM.
func (c *Client) Call(ctx context.Context, messages []crewai.Message) (string, error) {
	if c.baseURL == CloudBaseURL && c.apiKey == "" {
		return "", fmt.Errorf("ollama: Ollama Cloud requer um token (defina OLLAMA_API_KEY ou use WithAPIKey)")
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
		return "", fmt.Errorf("ollama: codificando requisição: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewReader(buf))
	if err != nil {
		return "", fmt.Errorf("ollama: criando requisição: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama: enviando requisição: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("ollama: lendo resposta: %w", err)
	}

	var parsed chatResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", fmt.Errorf("ollama: decodificando resposta (status %d): %w", resp.StatusCode, err)
	}
	if parsed.Error != "" {
		return "", fmt.Errorf("ollama: erro da API: %s", parsed.Error)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama: status inesperado %d: %s", resp.StatusCode, string(data))
	}
	return parsed.Message.Content, nil
}
