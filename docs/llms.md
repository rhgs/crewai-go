# LLMs

> **Languages:** **English** (current) · [Português](pt-BR/llms.md)

> **Languages:** [**English**](pt-BR/llms.md) · [Português](pt-BR/llms.md)


Every agent needs an **LLM**. The framework doesn't tie you to a provider: just
implement a small interface.

## The interface

```go
type LLM interface {
	Call(ctx context.Context, messages []Message) (string, error)
	Model() string
}
```

`Message` has only `Role` and `Content`:

```go
type Message struct {
	Role    Role   // RoleSystem, RoleUser, RoleAssistant, RoleTool
	Content string
}
```

## OpenAI (and compatible endpoints)

```go
import "github.com/rhgs/crewai-go/llm/openai"

llm := openai.New("gpt-4o-mini") // uses OPENAI_API_KEY from the environment
```

Options:

```go
llm := openai.New("gpt-4o",
	openai.WithAPIKey("sk-..."),
	openai.WithTemperature(0.2),
	openai.WithBaseURL("https://api.openai.com/v1"),
	openai.WithHTTPClient(&http.Client{Timeout: 60 * time.Second}),
)
```

Since it uses the standard _chat completions_ API, it works with many providers:

```go
// Ollama (local)
openai.New("llama3",
	openai.WithBaseURL("http://localhost:11434/v1"),
	openai.WithAPIKey("ollama"))

// Groq
openai.New("llama-3.1-70b-versatile",
	openai.WithBaseURL("https://api.groq.com/openai/v1"),
	openai.WithAPIKey(os.Getenv("GROQ_API_KEY")))
```

## Anthropic (Claude)

```go
import "github.com/rhgs/crewai-go/llm/anthropic"

llm := anthropic.New("claude-sonnet-5") // uses ANTHROPIC_API_KEY
```

Options: `WithAPIKey`, `WithMaxTokens`, `WithTemperature`, `WithBaseURL`,
`WithHTTPClient`. The client handles the _system prompt_ in the separate field
required by the Anthropic API.

## Ollama (local and cloud)

The `llm/ollama` package uses the native `/api/chat` API and covers both modes.

**Local** — no authentication, at `http://localhost:11434`:

```go
import "github.com/rhgs/crewai-go/llm/ollama"

llm := ollama.New("llama3.2")
// another machine on the network:
llm := ollama.New("llama3.2", ollama.WithBaseURL("http://192.168.0.10:11434"))
```

**Cloud** — models hosted at `https://ollama.com`, authenticated by token:

```go
llm := ollama.NewCloud("gpt-oss:120b") // uses OLLAMA_API_KEY
// or explicitly:
llm := ollama.NewCloud("gpt-oss:120b", ollama.WithAPIKey("..."))
```

Common options: `WithBaseURL`, `WithAPIKey`, `WithTemperature`, `WithHTTPClient`.

> You can also use Ollama through the `openai` client (compatible endpoint at
> `/v1`), but the `ollama` package is more direct and requires no key in local
> mode.

## xAI (Grok) — API key or subscription OAuth

The xAI API is OpenAI-compatible (`https://api.x.ai/v1`). The `llm/xai` package
offers two authentication modes.

**API key** (billed per token):

```go
import "github.com/rhgs/crewai-go/llm/xai"

llm := xai.New("grok-4") // uses XAI_API_KEY
```

**Subscription OAuth** (SuperGrok / X Premium) — no per-token key. Since May
2026 xAI offers OAuth login for agents: you authenticate your subscription via
the _Device Flow_ (RFC 8628 + PKCE) and use the subscription token.

```go
// 1) Set up the Device Flow (client_id provided by xAI).
df := xai.NewDeviceFlow(os.Getenv("XAI_CLIENT_ID"))

// 2) Reuse a saved token (auto-renews) or do the first login.
ts, err := xai.LoadTokenSource("~/.crewai-xai-token.json", df)
if err != nil {
	tok, err := df.Authorize(context.Background()) // prints URL + code
	if err != nil { log.Fatal(err) }
	xai.SaveToken("~/.crewai-xai-token.json", tok)
	ts = df.TokenSource(tok, func(t xai.Token) error {
		return xai.SaveToken("~/.crewai-xai-token.json", t)
	})
}

// 3) Use the OAuth-authenticated LLM.
llm := xai.NewWithOAuth("grok-4", ts)
```

The `TokenSource` auto-renews the access token using the refresh token and
rewrites the file. See the complete example in `examples/xai_oauth`.

> ⚠️ xAI does **not yet officially publish** the `client_id` and the exact OAuth
> endpoint paths for third-party clients. This package implements the RFC 8628
> standard; provide the `client_id` and, if needed, override the endpoints with
> `WithDeviceCodeURL`/`WithTokenURL` (or `WithAuthServer`) per the official
> documentation. The base identity server is `https://accounts.x.ai`.

## Custom LLM

Any type satisfying the interface works. Minimal offline example:

```go
type MyLLM struct{}

func (MyLLM) Model() string { return "my-model" }

func (MyLLM) Call(ctx context.Context, messages []crewai.Message) (string, error) {
	// integrate with your backend, a local model, a queue, etc.
	return "answer", nil
}
```

This is ideal for:

- Integrating providers not yet included.
- Adding _caching_, _retries_, or _rate limiting_ around another LLM.
- Testing (see the `llm/mock` package).

## Mock LLM (for tests)

```go
import "github.com/rhgs/crewai-go/llm/mock"

// Sequential responses:
llm := mock.New("first answer", "second answer")

// Or full control with a handler:
llm := &mock.LLM{Handler: func(ctx context.Context, msgs []crewai.Message) (string, error) {
	return "deterministic answer", nil
}}
```

## Concurrency

A single `LLM` can be shared by multiple agents. The included implementations
are safe for concurrent use.