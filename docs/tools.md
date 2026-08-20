# Tools

> **Languages:** **English** (current) · [Português](pt-BR/tools.md)

A **Tool** gives "hands" to an agent: it lets the agent perform actions —
calculate, query an API, read a file — during reasoning.

## The interface

```go
type Tool interface {
	Name() string
	Description() string
	Call(ctx context.Context, input string) (string, error)
}
```

Input and output are `string` to match the agent's text-based reasoning. If
your tool needs structured arguments, document the expected format (e.g. a
JSON object) in its `Description`.

## Creating a tool from a function

```go
weather := crewai.NewTool(
	"weather",
	"Looks up the weather for a city. Input: the city name.",
	func(ctx context.Context, city string) (string, error) {
		// call your API here...
		return fmt.Sprintf("Sunny in %s, 27°C", city), nil
	},
)
```

## Built-in tools (the `tools` package)

```go
import "github.com/rhgs/crewai-go/tools"

tools.Calculator()      // evaluates "2 + 2 * (3 - 1)" — offline and safe
tools.CurrentTime("")   // current date/time (time package layout; "" = RFC3339)
tools.WordCount()       // counts words and characters in the text
```

## Attaching tools

To an agent (available for all its tasks):

```go
agent.WithTools(tools.Calculator(), weather)
```

To a specific task (overrides the agent's tools for that task only):

```go
task.Tools = []crewai.Tool{weather}
```

## The ReAct protocol

When an agent has tools, it follows a **Reasoning + Action** cycle. The model
is instructed to respond in this format:

```
Thought: I need to calculate the total
Action: calculator
Action Input: 1500 * 1.12
```

The framework runs the tool and returns:

```
Observation: 1680
```

The cycle repeats until the model concludes:

```
Thought: now I know the answer
Final Answer: The final value is $1,680.00.
```

### Robustness

- If the model does **not** follow the protocol, the output is treated as the
  final answer (instead of stalling).
- If the model asks for a **non-existent** tool, the framework returns an error
  `Observation` listing the valid tools, and the agent tries again.
- Errors returned by a tool become an error `Observation` — the agent can react
  to them.
- The loop stops at `MaxIterations` (default 15), returning `ErrMaxIterations`.

## Best practices

- **Short names** without spaces (`web_search`, not `Web Search`).
- **Clear descriptions** stating *what it does* and *what input is expected*.
- Tools should be **idempotent** when possible — the agent may call them more
  than once.
- Respect `context.Context` (timeouts/cancellation) in network calls.

## Facts & provenance

A **Fact** is a piece of data produced by a deterministic connector tool, not
by the LLM. It always carries provenance (source organization, source URL,
collection time, payload hash) so a wrong value can never be presented as a
"fact the model remembered".

### The Fact type

```go
type Fact struct {
    Claim       string    `json:"claim"`
    SourceOrg   string    `json:"source_org"`
    SourceURL   string    `json:"source_url"`
    CollectedAt time.Time `json:"collected_at"`
    PayloadHash string    `json:"payload_hash"`
}
```

### Making a tool a FactSource

A tool declares itself as a fact source by using `NewFactSourceTool`:

```go
tool := crewai.NewFactSourceTool(
    "cnpj_lookup",
    "Looks up CNPJ status. Input: the CNPJ number.",
    func(_ context.Context, cnpj string) (string, error) {
        // call your API...
        return "Company X is ATIVA", nil
    },
    func(_ context.Context, output string) []crewai.Fact {
        return []crewai.Fact{
            crewai.NewFact(output, "Receita Federal",
                "https://api.receita.gov.br/v1/cnpj/...", []byte(rawPayload)),
        }
    },
)
```

After each successful `Call`, the executor collects the tool's `Facts()` and
attaches them to `TaskOutput.Facts` and `CrewOutput.Facts`.

### Rules

- The LLM NEVER produces a Fact. Facts come only from FactSource tools.
- Facts are deduplicated by PayloadHash (first occurrence kept).
- Tools not implementing FactSource contribute zero facts.

### Provenance guardrails

Use `AllFactsProvenanced` in a guardrail to enforce that every fact has
SourceURL and PayloadHash:

```go
crew.Guardrails = []crewai.Guardrail{
    func(_ context.Context, out *crewai.CrewOutput) error {
        return crewai.AllFactsProvenanced(out.Facts)
    },
}
```

If any fact lacks provenance, `Kickoff` returns `ErrBlockedByGuardrail`.

## Web search (WebSearchTool)

The `WebSearchTool` is a `Tool` that searches the web via a pluggable
`SearchProvider` and also implements `FactSource`, so results are collected
as `Fact`s with provenance. Use this in the ReAct loop when you want the
**LLM to decide** when to search (model-driven pattern).

```go
import "github.com/rhgs/crewai-go/tools"

// Default: Wikipedia (free, no API key).
tool := tools.NewWebSearch(tools.NewWikipediaSearch())
agent.WithTools(tool)
```

### SearchProvider implementations

| Provider | Constructor | API key | Notes |
|----------|------------|--------|-------|
| **Wikipedia** (default) | `NewWikipediaSearch()` | Free, none | Searches Wikipedia articles only. `NewWikipediaSearchWithLanguage("pt")` to set language. |
| **LangSearch** | `NewLangSearch(apiKey)` | Free tier | 100% free. General web search. |
| **Serpstack** | `NewSerpstack(apiKey)` | Free 1000/month | Google SERP results. |
| **DuckDuckGo** | `NewDuckDuckGoSearch()` | Free, none | Optional provider. May be rate-limited or blocked by DuckDuckGo. |
| **Google** | `NewGoogleSearch(apiKey, cxID)` | Required | Google Custom Search. Needs API key + CX ID. |
| **Brave** | `NewBraveSearch(apiKey)` | Required | Brave Search API. |

### Provider examples

**Wikipedia** (default, free):

```go
tool := tools.NewWebSearch(tools.NewWikipediaSearch())
// or with a specific language:
tool := tools.NewWebSearch(tools.NewWikipediaSearchWithLanguage("en"))
```

**LangSearch** (100% free):

```go
tool := tools.NewWebSearch(tools.NewLangSearch(os.Getenv("LANGSEARCH_API_KEY")))
```

**Serpstack** (1000 requests/month free):

```go
tool := tools.NewWebSearch(tools.NewSerpstack(os.Getenv("SERPSTACK_API_KEY")))
```

**DuckDuckGo** (optional, no API key):

```go
tool := tools.NewWebSearch(tools.NewDuckDuckGoSearch())
```

**Google Custom Search** (requires API key + CX ID):

```go
tool := tools.NewWebSearch(
	tools.NewGoogleSearch(os.Getenv("GOOGLE_API_KEY"), os.Getenv("GOOGLE_CSE_ID")),
)
```

**Brave Search** (requires API key):

```go
tool := tools.NewWebSearch(tools.NewBraveSearch(os.Getenv("BRAVE_API_KEY")))
```

### Options

```go
tool := tools.NewWebSearch(
	tools.NewWikipediaSearch(),
	tools.WithMaxResults(10),             // default 5
	tools.WithSearchTimeout(60*time.Second), // default 30s
)
```

### FactSource integration

`WebSearchTool` implements `crewai.FactSource`. After each successful
`Call`, results are collected as `Fact`s with source URL and payload hash,
then attached to `TaskOutput.Facts` and `CrewOutput.Facts`:

```go
crew.Guardrails = []crewai.Guardrail{
	func(_ context.Context, out *crewai.CrewOutput) error {
		return crewai.AllFactsProvenanced(out.Facts)
	},
}
```

### SSRF protection

All URLs returned by any provider are filtered through SSRF (Server-Side
Request Forgery) protection before being presented to the agent:

- **Non-http(s) schemes** are blocked (`file://`, `ftp://`, etc.).
- **Userinfo** (`user:pass@host`) is blocked — never useful for public
  search results and a common SSRF smuggling vector.
- **Loopback** addresses (`localhost`, `localhost.localdomain`,
  `127.0.0.1`, `::1`) and bare metadata aliases
  (`metadata`, `metadata.google.internal`) are blocked by name.
- **Private, link-local, unspecified, multicast, and CGNAT**
  (`100.64.0.0/10`, RFC 6598) IP addresses are blocked
  (e.g. `10.x`, `192.168.x`, `169.254.x` — this covers cloud metadata
  endpoints like `169.254.169.254`).
- **DNS rebinding prevention**: domain names are resolved via DNS and all
  resolved IPs are checked. If any resolves to a blocked address, the URL
  is blocked.
- **Fail-closed**: if DNS resolution fails or the host is empty, the URL
  is blocked by default.

## Logging

Tools log via `*log/slog`. Two logs can leak sensitive content:

- `agent thought` (Debug) - the entire LLM response before tool call.
- `tool invoked` (Info) / `native tool call` (Info) - the tool input/args.

**Recommendation:** in production, keep the log level at `LevelError` or wrap the logger handler with a redactor. The `crewai` package provides `redactError` (used internally for delegation-failed warnings) and `examples/logging/` shows a drop-in redaction handler that masks likely-secrets in all attributes before they reach the destination.
