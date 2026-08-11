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

## Web search (interface WebSearcher)

Alguns provedores de LLM oferecem uma API de busca web nativa. O framework
expõe essa capacidade pela interface opcional `WebSearcher`:

```go
type WebSearcher interface {
	WebSearch(ctx context.Context, query string, max int) ([]SearchHit, error)
}
```

`SearchHit` é um único resultado de busca:

```go
type SearchHit struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Content string `json:"content"` // trecho (snippet), não a página completa
}
```

Este é o padrão **agent-driven** (dirigido pelo agente): o código Go decide
o que buscar, quando buscar e quantos resultados obter. O LLM não participa da
decisão — apenas a API do provedor é chamada.

### Provedores que implementam WebSearcher

| Provedor  | Pacote            | Como funciona a busca                                  |
|-----------|-------------------|--------------------------------------------------------|
| Ollama    | `llm/ollama`      | `POST /api/web_search` — busca pura, sem invocar modelo |
| OpenAI    | `llm/openai`     | `web_search_options` (não `tools`) com modelos de busca |
| Anthropic | `llm/anthropic`  | Ferramenta `web_search_20250305` (server tool)           |
| xAI       | `llm/xai`         | Delega para o cliente OpenAI compatível                 |

> O Mock (`llm/mock`) também implementa `WebSearcher` para testes.

### Ollama — busca pura via `POST /api/web_search`

O Ollama expõe um endpoint de busca que **não invoca o modelo** — é uma
API de busca pura, que usa a mesma autenticação do endpoint de chat. Isso
significa que não consome tokens de modelo, apenas faz a busca.

```go
llm := ollama.NewCloud("gpt-oss:120b") // precisa de OLLAMA_API_KEY

hits, err := llm.WebSearch(ctx, "Go programming language", 5)
// cada hit tem Title, URL e Content (snippet)
```

### OpenAI — `web_search_options` com modelos de busca

A OpenAI **não** tem uma API de busca autônoma. Em vez disso, a busca web é
ativada pelo campo `web_search_options` (e **não** por `tools`) na API de
Chat Completions, o que exige um modelo compatível com busca, como
`gpt-4o-search-preview` ou `gpt-5-search-api`.

> ⚠️ Diferente do Ollama, aqui **o modelo é invocado** — a busca consome
> tokens. Para volume alto de buscas, considere usar `WebSearchTool` com um
> provedor como Brave ou Google.

```go
// Configure o cliente com um modelo de busca.
llm := openai.New("gpt-4o-search-preview")

hits, err := llm.WebSearch(ctx, "Go programming language", 5)
```

O modelo recebe a query como mensagem de usuário com `web_search_options`
habilitado, busca na web e devolve os resultados como anotações
(`url_citation`), que o framework extrai em `SearchHit`.

### Anthropic — ferramenta `web_search_20250305`

A Anthropic também não tem uma API de busca autônoma. A busca é feita via
uma _server tool_ chamada `web_search_20250305`: o framework envia a query
como mensagem de usuário com essa ferramenta habilitada, e a API retorna os
resultados em blocos de conteúdo do tipo `web_search_tool_result`.

> Assim como na OpenAI, **o modelo é invocado** e a busca consome tokens.

```go
llm := anthropic.New("claude-sonnet-5")

hits, err := llm.WebSearch(ctx, "Go programming language", 5)
```

Cada bloco `web_search_tool_result` contém objetos `web_search_result` com
`title`, `url` e `encrypted_content`, que o framework extrai em `SearchHit`.

### xAI (Grok)

O xAI é compatível com a OpenAI e delega a busca para o cliente OpenAI
embutido. O modelo configurado deve ser compatível com busca.

```go
llm := xai.New("grok-4")

hits, err := llm.WebSearch(ctx, "Go programming language", 5)
```

### Helper `SearchWeb`

Para evitar _type assertion_ manual, use `crewai.SearchWeb`:

```go
import "github.com/rhgs/crewai-go"

hits, err := crewai.SearchWeb(ctx, llm, "Go programming language", 5)
if err != nil {
	// err pode ser crewai.ErrWebSearchUnsupported
	log.Fatal(err)
}
for _, h := range hits {
	fmt.Printf("%s — %s\n", h.Title, h.URL)
}
```

Se o LLM não implementar `WebSearcher`, `SearchWeb` retorna
`crewai.ErrWebSearchUnsupported`.

## Concorrência

Um mesmo `LLM` pode ser compartilhado por vários agentes. As implementações
incluídas são seguras para uso concorrente.

## Logging

O executor emite logs estruturados via `*log/slog`. Injete um logger em
`Crew` (e opcionalmente em `Agent`) antes de chamar `Kickoff`:

```go
import (
    "log/slog"
    "os"

    "github.com/rhgs/crewai-go"
)

log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
    Level: slog.LevelDebug,
}))

crew := crewai.NewCrew(agentes, tarefas).WithLogger(log)
out, err := crew.Kickoff(ctx, nil)
```

**Fallback padrão.** Se `WithLogger` não for chamado, `Kickoff` cria um
logger de texto no stderr em `LevelDebug` quando `Crew.Verbose` é `true`
e `LevelError` quando `Crew.Verbose` é `false` (correspondendo ao
comportamento legado de "silencioso a menos que verbose").

**Agent.Execute.** Quando usado fora de uma crew, `Agent.Execute` usa
`slog.Default()` a menos que `Agent.WithLogger` seja definido.

**Thread-safety.** `WithLogger` **não é concorrente-safe** — defina-o
antes de chamar `Kickoff`/`Execute` e não o mude concorrentemente. Múltiplas
chamadas sequenciais são idempotentes (a última vence).

### Eventos logados

| Componente       | Nível   | Mensagem                        | Chaves                            |
|------------------|---------|---------------------------------|-----------------------------------|
| `executor.go`    | Debug   | `agent thought`                 | `agent`, `output`                 |
| `executor.go`    | Info    | `tool invoked`                  | `agent`, `tool`, `input`          |
| `toolcall.go`    | Info    | `native tool call`              | `agent`, `tool`, `args`           |
| `toolcall.go`    | Debug   | `native tool loop done`         | `agent`, `iterations`             |
| `structured.go`  | Debug   | `structured output validated`   | `agent`, `attempt`                |
| `structured.go`  | Debug   | `structured output validation failed` | `agent`, `attempt`, `error` |
| `crew.go`        | Info    | `manager resolved`              | `manager`                         |
| `crew.go`        | Info    | `task delegated`                | `task_index`, `agent`             |
| `crew.go`        | Warn    | `delegation failed, using first agent` | `error` (redacted)         |

### Segurança

- **Logs em Debug incluem a saída completa do LLM** (`agent thought` →
  resposta inteira do modelo) e inputs de ferramentas. Direcione para
  destinos locais quando lidar com dados de usuário.
- **Erros de providers passam por `redactError`** antes de serem logados
  (notavelmente em `delegation failed`), que mascara segredos prováveis:
  tokens alfanuméricos longos (≥20 chars, preservando 4 primeiros/últimos
  quando ≥24 chars), `Bearer <token>`, e valores de query-string
  `api_key=`/`token=`/`key=`/`secret=`.

  Veja `redact.go` e `examples/logging/` para as regras completas e um
  handler de redação drop-in.
