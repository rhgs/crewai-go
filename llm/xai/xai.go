// Package xai implements crewai.LLM for the xAI API (Grok models).
//
// The xAI API is compatible with the OpenAI Chat Completions API, so this
// package reuses the llm/openai client pointed at the xAI endpoint. There are
// two authentication modes:
//
//   - API key (XAI_API_KEY): conventional use, billed per token — New.
//   - Subscription OAuth (SuperGrok / X Premium): no API key, using the token
//     from your subscription via the Device Flow — NewWithOAuth + oauth.go.
package xai

import (
	"context"
	"os"

	"github.com/rhgs/crewai-go"
	"github.com/rhgs/crewai-go/llm/openai"
)

// DefaultBaseURL is the OpenAI-compatible xAI endpoint.
const DefaultBaseURL = "https://api.x.ai/v1"

// Client is an xAI (Grok) LLM. It wraps an OpenAI-compatible client.
type Client struct {
	inner *openai.Client
}

// Option configures the Client.
type Option func(*config)

type config struct {
	apiKey      string
	baseURL     string
	tokenSource openai.TokenSource
	extra       []openai.Option
}

// WithAPIKey sets the API key explicitly.
func WithAPIKey(key string) Option { return func(c *config) { c.apiKey = key } }

// WithBaseURL overrides the endpoint (useful for proxies/gateways).
func WithBaseURL(url string) Option { return func(c *config) { c.baseURL = url } }

// WithTemperature adjusts the sampling temperature.
func WithTemperature(t float64) Option {
	return func(c *config) { c.extra = append(c.extra, openai.WithTemperature(t)) }
}

// WithTokenSource uses a dynamic Bearer token (subscription OAuth).
func WithTokenSource(ts openai.TokenSource) Option {
	return func(c *config) { c.tokenSource = ts }
}

// New creates a Grok client authenticated by API key. If no key is passed, the
// XAI_API_KEY environment variable is used.
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

// NewWithOAuth creates a Grok client authenticated by subscription OAuth
// (SuperGrok / X Premium), with no per-token API key. ts usually comes from a
// Device Flow (see NewDeviceFlow / LoadTokenSource in this package).
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

// Call implements crewai.LLM.
func (c *Client) Call(ctx context.Context, messages []crewai.Message) (string, error) {
	return c.inner.Call(ctx, messages)
}

// Model implements crewai.LLM.
func (c *Client) Model() string { return c.inner.Model() }
