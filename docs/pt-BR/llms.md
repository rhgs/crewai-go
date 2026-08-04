# LLMs

> **Languages:** [English](../llms.md) · **Português** (atual)

Todo agente precisa de um **LLM**. O framework não amarra você a um provedor:
basta implementar uma interface pequena.

## A interface

```go
type LLM interface {
	Call(ctx context.Context, messages []Message) (string, error)
	Model() string
}
```

`Message` tem apenas `Role` e `Content`:

```go
type Message struct {
	Role    Role   // RoleSystem, RoleUser, RoleAssistant, RoleTool
	Content string
}
```

## OpenAI (e endpoints compatíveis)

```go
import "github.com/rhgs/crewai-go/llm/openai"

llm := openai.New("gpt-4o-mini") // usa OPENAI_API_KEY do ambiente
```

Opções:

```go
llm := openai.New("gpt-4o",
	openai.WithAPIKey("sk-..."),
	openai.WithTemperature(0.2),
	openai.WithBaseURL("https://api.openai.com/v1"),
	openai.WithHTTPClient(&http.Client{Timeout: 60 * time.Second}),
)
```

Como usa a API padrão de _chat completions_, funciona com muitos provedores:

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

llm := anthropic.New("claude-sonnet-5") // usa ANTHROPIC_API_KEY
```

Opções: `WithAPIKey`, `WithMaxTokens`, `WithTemperature`, `WithBaseURL`,
`WithHTTPClient`. O client trata o _system prompt_ no campo separado que a API
da Anthropic exige.

## Ollama (local e cloud)

O pacote `llm/ollama` usa a API nativa `/api/chat` e cobre as duas modalidades.

**Local** — sem autenticação, em `http://localhost:11434`:

```go
import "github.com/rhgs/crewai-go/llm/ollama"

llm := ollama.New("llama3.2")
// outra máquina na rede:
llm := ollama.New("llama3.2", ollama.WithBaseURL("http://192.168.0.10:11434"))
```

**Cloud** — modelos hospedados em `https://ollama.com`, autenticados por token:

```go
llm := ollama.NewCloud("gpt-oss:120b") // usa OLLAMA_API_KEY
// ou explicitamente:
llm := ollama.NewCloud("gpt-oss:120b", ollama.WithAPIKey("..."))
```

Opções comuns: `WithBaseURL`, `WithAPIKey`, `WithTemperature`, `WithHTTPClient`.

> Também é possível usar o Ollama pelo cliente `openai` (endpoint compatível em
> `/v1`), mas o pacote `ollama` é mais direto e não exige chave no modo local.

## xAI (Grok) — chave de API ou OAuth de assinatura

A API da xAI é compatível com OpenAI (`https://api.x.ai/v1`). O pacote `llm/xai`
oferece dois modos de autenticação.

**Chave de API** (cobrada por token):

```go
import "github.com/rhgs/crewai-go/llm/xai"

llm := xai.New("grok-4") // usa XAI_API_KEY
```

**OAuth de assinatura** (SuperGrok / X Premium) — sem chave cobrada por token.
Desde maio de 2026 a xAI oferece login OAuth para agentes: você autentica sua
assinatura via _Device Flow_ (RFC 8628 + PKCE) e usa o token da assinatura.

```go
// 1) Configure o Device Flow (client_id fornecido pela xAI).
df := xai.NewDeviceFlow(os.Getenv("XAI_CLIENT_ID"))

// 2) Reutilize um token salvo (renova sozinho) ou faça o primeiro login.
ts, err := xai.LoadTokenSource("~/.crewai-xai-token.json", df)
if err != nil {
	tok, err := df.Authorize(context.Background()) // imprime URL + código
	if err != nil { log.Fatal(err) }
	xai.SaveToken("~/.crewai-xai-token.json", tok)
	ts = df.TokenSource(tok, func(t xai.Token) error {
		return xai.SaveToken("~/.crewai-xai-token.json", t)
	})
}

// 3) Use o LLM autenticado por OAuth.
llm := xai.NewWithOAuth("grok-4", ts)
```

O `TokenSource` renova o access token automaticamente com o refresh token e
regrava o arquivo. Veja o exemplo completo em `examples/xai_oauth`.

> ⚠️ A xAI ainda **não publica oficialmente** o `client_id` e os caminhos exatos
> dos endpoints de OAuth para clientes de terceiros. Este pacote implementa o
> padrão RFC 8628; informe o `client_id` e, se necessário, sobrescreva os
> endpoints com `WithDeviceCodeURL`/`WithTokenURL` (ou `WithAuthServer`) segundo
> a documentação oficial. O servidor de identidade base é `https://accounts.x.ai`.

## LLM customizado

Qualquer tipo que satisfaça a interface serve. Exemplo mínimo e offline:

```go
type MeuLLM struct{}

func (MeuLLM) Model() string { return "meu-modelo" }

func (MeuLLM) Call(ctx context.Context, messages []crewai.Message) (string, error) {
	// integre com o seu backend, um modelo local, uma fila, etc.
	return "resposta", nil
}
```

Isso é ideal para:

- Integrar provedores ainda não incluídos.
- Adicionar _caching_, _retries_ ou _rate limiting_ em volta de outro LLM.
- Testar (veja o pacote `llm/mock`).

## LLM mock (para testes)

```go
import "github.com/rhgs/crewai-go/llm/mock"

// Respostas em sequência:
llm := mock.New("primeira resposta", "segunda resposta")

// Ou controle total com um handler:
llm := &mock.LLM{Handler: func(ctx context.Context, msgs []crewai.Message) (string, error) {
	return "resposta determinística", nil
}}
```

## Concorrência

Um mesmo `LLM` pode ser compartilhado por vários agentes. As implementações
incluídas são seguras para uso concorrente.
