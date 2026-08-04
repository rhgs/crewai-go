// Package xai implementa crewai.LLM para a API da xAI (modelos Grok).
//
// A API da xAI é compatível com a API de Chat Completions da OpenAI, então
// este pacote reutiliza o cliente de llm/openai apontando para o endpoint da
// xAI. Há dois modos de autenticação:
//
//   - Chave de API (XAI_API_KEY): uso convencional, cobrado por token — New.
//   - OAuth de assinatura (SuperGrok / X Premium): sem chave de API, usando o
//     token da sua assinatura via o Device Flow — NewWithOAuth + oauth.go.
package xai

import (
	"context"
	"os"

	"github.com/rhgs/crewai-go"
	"github.com/rhgs/crewai-go/llm/openai"
)

// DefaultBaseURL é o endpoint compatível com OpenAI da xAI.
const DefaultBaseURL = "https://api.x.ai/v1"

// Client é um LLM da xAI (Grok). Encapsula um cliente OpenAI-compatível.
type Client struct {
	inner *openai.Client
}

// Option configura o Client.
type Option func(*config)

type config struct {
	apiKey      string
	baseURL     string
	tokenSource openai.TokenSource
	extra       []openai.Option
}

// WithAPIKey define a chave de API explicitamente.
func WithAPIKey(key string) Option { return func(c *config) { c.apiKey = key } }

// WithBaseURL sobrescreve o endpoint (útil para proxies/gateways).
func WithBaseURL(url string) Option { return func(c *config) { c.baseURL = url } }

// WithTemperature ajusta a temperatura de amostragem.
func WithTemperature(t float64) Option {
	return func(c *config) { c.extra = append(c.extra, openai.WithTemperature(t)) }
}

// WithTokenSource usa um token Bearer dinâmico (OAuth de assinatura).
func WithTokenSource(ts openai.TokenSource) Option {
	return func(c *config) { c.tokenSource = ts }
}

// New cria um cliente Grok autenticado por chave de API. Se nenhuma chave for
// passada, usa a variável de ambiente XAI_API_KEY.
//
//	llm := xai.New("grok-4")
func New(model string, opts ...Option) *Client {
	cfg := &config{
		apiKey:  os.Getenv("XAI_API_KEY"),
		baseURL: DefaultBaseURL,
	}
	for _, o := range opts {
		o(cfg)
	}
	return build(model, cfg)
}

// NewWithOAuth cria um cliente Grok autenticado por OAuth de assinatura
// (SuperGrok / X Premium), sem chave de API cobrada por token. O ts costuma
// vir de um Device Flow (veja NewDeviceFlow / TokenSourceFromFile neste pacote).
//
//	ts, _ := xai.LoadTokenSource("~/.crewai/xai_token.json")
//	llm := xai.NewWithOAuth("grok-4", ts)
func NewWithOAuth(model string, ts openai.TokenSource, opts ...Option) *Client {
	cfg := &config{baseURL: DefaultBaseURL, tokenSource: ts}
	for _, o := range opts {
		o(cfg)
	}
	return build(model, cfg)
}

func build(model string, cfg *config) *Client {
	oopts := []openai.Option{openai.WithBaseURL(cfg.baseURL)}
	if cfg.tokenSource != nil {
		oopts = append(oopts, openai.WithTokenSource(cfg.tokenSource))
	} else {
		oopts = append(oopts, openai.WithAPIKey(cfg.apiKey))
	}
	oopts = append(oopts, cfg.extra...)
	return &Client{inner: openai.New(model, oopts...)}
}

// Call implementa crewai.LLM.
func (c *Client) Call(ctx context.Context, messages []crewai.Message) (string, error) {
	return c.inner.Call(ctx, messages)
}

// Model implementa crewai.LLM.
func (c *Client) Model() string { return c.inner.Model() }
