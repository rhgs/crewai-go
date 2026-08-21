# crewai-go

> **Languages:** **English** (current) · [Português](README.pt-BR.md)

[![Go Reference](https://pkg.go.dev/badge/github.com/rhgs/crewai-go.svg)](https://pkg.go.dev/github.com/rhgs/crewai-go)
[![CI](https://github.com/rhgs/crewai-go/actions/workflows/ci.yml/badge.svg)](https://github.com/rhgs/crewai-go/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/rhgs/crewai-go/graph/badge.svg)](https://codecov.io/gh/rhgs/crewai-go)
[![Release](https://img.shields.io/github/v/release/rhgs/crewai-go?label=release)](https://github.com/rhgs/crewai-go/releases)
[![Go Version](https://img.shields.io/github/go-mod/go-version/rhgs/crewai-go)](https://github.com/rhgs/crewai-go/blob/main/go.mod)
[![Last Commit](https://img.shields.io/github/last-commit/rhgs/crewai-go)](https://github.com/rhgs/crewai-go/commits)
[![Zero Dependencies](https://img.shields.io/badge/dependencies-zero-success)](https://pkg.go.dev/github.com/rhgs/crewai-go)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](https://github.com/rhgs/crewai-go/blob/main/LICENSE)
[![Mentioned in Awesome Go](https://awesome.re/mentioned-badge.svg)](https://github.com/avelino/awesome-go)

<p align="center">
  <img src="gopher.jpg" alt="crewai-go cover" width="600">
</p>

**Orchestration of autonomous, collaborative AI agents in Go.**

`crewai-go` is an idiomatic Go port of the [CrewAI](https://github.com/crewAIInc/crewAI) framework. It lets you assemble teams (_crews_) of agents with distinct roles that collaborate — sequentially or hierarchically — to complete complex tasks using large language models (LLMs).

> Built with **zero external dependencies** — only the Go standard library. Easy to install, audit, and integrate.

---

## Table of Contents

- [Why crewai-go](#why-crewai-go)
- [Concepts](#concepts)
- [Installation](#installation)
- [Quick Start](#quick-start)
- [LLM Providers](#llm-providers)
- [Tools](#tools)
- [Native tool calling](#native-tool-calling)
- [Web search](#web-search)
- [Logging](#logging)
- [Processes: Sequential, Hierarchical, and Staged](#processes-sequential-hierarchical-and-staged)
- [Agentic loop](#agentic-loop)
- [Structured output](#structured-output)
- [Guardrails](#guardrails)
- [Facts & provenance](#facts--provenance)
- [MCP](#mcp-model-context-protocol)
- [Progress & warnings](#progress--warnings)
- [Memory](#memory)
- [Examples](#examples)
- [Documentation](#documentation)
- [Tests](#tests)
- [Comparison with CrewAI (Python)](#comparison-with-crewai-python)
- [Contributing](#contributing)
- [License](#license)

---

## Why crewai-go

- 🧩 **Simple, composable API** — `Agent`, `Task`, `Crew`, `Tool`, `LLM`.
- ⚡ **No dependencies** — stdlib only; small, fast builds.
- 🔌 **Any LLM** — OpenAI (and compatible: Ollama, Groq, Azure…), Anthropic (Claude), or your own implementation of the `LLM` interface.
- 🛠️ **Tools via ReAct** — agents reason and call tools in plain text.
- 📋 **Structured output** — tasks can require JSON validated against a JSON Schema, with a bounded repair loop.
- 🛡️ **Guardrails** — code-enforced post-output validation that blocks publication of outputs violating business invariants.
- 📌 **Facts & provenance** — first-class Fact type populated only by deterministic connector tools, never by the LLM, with full provenance metadata.
- 🔧 **Native tool calling** — use provider-native function calling (OpenAI, Anthropic, Ollama) instead of text-based ReAct, with automatic fallback and full trace observability.
- 🔎 **Web search** — agent-driven search via the `WebSearcher` interface (Ollama, OpenAI, Anthropic, xAI) or model-driven search via `WebSearchTool` with 7 providers (Wikipedia, LangSearch, Serpstack, DuckDuckGo, Google, Brave). SSRF-protected.
- 📝 **Structured logging via `log/slog`** — inject a custom `*slog.Logger` on `Crew` and `Agent`, with backward-compatible `Verbose` fallback.
- 🧠 **Memory** between tasks and chainable **context**.
- 👔 **Hierarchical process** with a manager that delegates dynamically.
- 🪜 **Staged process** — stages run in sequence, tasks within a stage run in parallel.
- 🔁 **Agentic loop** — optional Plan-Execute-Evaluate-Refine cycle with self-evaluation and iterative refinement.
- 🔌 **MCP** — connect to Model Context Protocol servers and expose their tools as `crewai.Tool` (schema-preserving).
- 📡 **Progress & warnings** — real-time `WithProgress` callbacks and per-task non-fatal warnings.
- ✅ **Testable** — mock LLM included; ~90% core coverage.

## Concepts

| Concept     | What it is                                                              |
|--------------|-------------------------------------------------------------------------|
| **Agent**    | A worker with a role, a goal, a backstory, an LLM, and tools.           |
| **Task**     | A unit of work with a description, expected output, and an assignee.   |
| **Crew**     | The team: groups agents and tasks and orchestrates them.                |
| **Process**  | Execution strategy: `Sequential`, `Hierarchical`, or `Staged`.        |
| **Tool**     | A capability an agent can invoke (calculation, search, API…).          |
| **LLM**      | Abstraction over the language model. Several providers ready to use.   |
| **Memory**   | Stores task outputs to give context to following tasks.                |
| **StructuredOutput** | Configures a task to require JSON validated against a JSON Schema. |
| **Guardrail** | Post-output validation hook that blocks publication of invalid outputs. |
| **Fact** | Data from a deterministic connector tool with provenance (source, hash). |
| **FactSource** | Optional interface for tools that produce Facts. |
| **ToolMode** | Tool execution strategy: `"react"` (default) or `"native"`. |
| **ToolCallingLLM** | Optional LLM interface for native function calling. |
| **ToolTrace** | Records each native tool invocation (name, args, output, duration). |

## Installation

Requires **Go 1.24+**.

```bash
go get github.com/rhgs/crewai-go@latest
```

In your code:

```go
import "github.com/rhgs/crewai-go"
```

## Quick Start

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/rhgs/crewai-go"
	"github.com/rhgs/crewai-go/llm/openai"
)

func main() {
	// 1. Pick an LLM (uses OPENAI_API_KEY from the environment).
	llm := openai.New("gpt-4o-mini")

	// 2. Create an agent.
	poet := crewai.NewAgent(
		"Poet",
		"Write short, memorable poems",
		"You are an award-winning poet, master of brevity.",
		llm,
	)

	// 3. Define a task.
	task := crewai.NewTask(
		"Write a haiku about the Go programming language.",
		"A haiku (3 lines) in English.",
		poet,
	)

	// 4. Assemble the crew and run it.
	crew := crewai.NewCrew([]*crewai.Agent{poet}, []*crewai.Task{task})
	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(out.Final)
}
```

```bash
export OPENAI_API_KEY=sk-...
go run .
```

## LLM Providers

Any type implementing the interface below works:

```go
type LLM interface {
	Call(ctx context.Context, messages []Message) (string, error)
	Model() string
}
```

Ready to use:

```go
import (
	"github.com/rhgs/crewai-go/llm/openai"
	"github.com/rhgs/crewai-go/llm/anthropic"
	"github.com/rhgs/crewai-go/llm/ollama"
	"github.com/rhgs/crewai-go/llm/xai"
	"github.com/rhgs/crewai-go/llm/mock" // for tests
)

// OpenAI (and compatible: Groq, Azure, Together…)
llm := openai.New("gpt-4o-mini")

// Anthropic (Claude)
llm := anthropic.New("claude-sonnet-5")

// Ollama local (no key)
llm := ollama.New("llama3.2")

// Ollama Cloud (uses OLLAMA_API_KEY)
llm := ollama.NewCloud("gpt-oss:120b")

// xAI (Grok) via API key
llm := xai.New("grok-4")

// xAI (Grok) via subscription OAuth (SuperGrok / X Premium) — no per-token key
df := xai.NewDeviceFlow(clientID)
ts, _ := xai.LoadTokenSource("~/.crewai-xai-token.json", df)
llm := xai.NewWithOAuth("grok-4", ts)
```

| Provider | Package | Authentication |
|----------|--------|----------------|
| OpenAI (and compatible) | `llm/openai` | `OPENAI_API_KEY` / `WithTokenSource` |
| Anthropic (Claude) | `llm/anthropic` | `ANTHROPIC_API_KEY` |
| Ollama local | `llm/ollama` (`New`) | none |
| Ollama Cloud | `llm/ollama` (`NewCloud`) | `OLLAMA_API_KEY` |
| xAI (Grok) — API key | `llm/xai` (`New`) | `XAI_API_KEY` |
| xAI (Grok) — subscription | `llm/xai` (`NewWithOAuth`) | OAuth Device Flow |
| Mock (tests) | `llm/mock` | none |

See [`docs/llms.md`](docs/llms.md) for all options.

## Tools

Create a tool from any Go function:

```go
search := crewai.NewTool(
	"web_search",
	"Searches the web for a term. Input: the search term.",
	func(ctx context.Context, term string) (string, error) {
		// ... your logic ...
		return result, nil
	},
)

agent.WithTools(search)
```

Built-in tools in the `tools` package:

```go
import "github.com/rhgs/crewai-go/tools"

agent.WithTools(
	tools.Calculator(),        // evaluates arithmetic expressions
	tools.CurrentTime(""),      // current date/time
	tools.WordCount(),         // counts words/characters
)
```

The agent uses tools via the **ReAct** protocol (`Thought → Action → Action Input → Observation → Final Answer`). Details in [`docs/tools.md`](docs/tools.md).

## Native tool calling

For providers that support native function calling (OpenAI, Anthropic, Ollama), set `Agent.ToolMode` to use the provider's built-in tool calling instead of text-based ReAct:

```go
llm := ollama.New("llama3.2")

agent := crewai.NewAgent("Researcher", "Find answers", "You are a researcher.", llm)
agent.ToolMode = crewai.ToolModeNative
agent.WithTools(
    tools.Calculator(),
    tools.CurrentTime(""),
)

task := crewai.NewTask("What is 15% of 200?", "A short answer.", agent)
crew := crewai.NewCrew([]*crewai.Agent{agent}, []*crewai.Task{task})
out, _ := crew.Kickoff(context.Background(), nil)

// ToolTraces record each native tool call for observability.
for _, trace := range out.TasksOutput[0].ToolTraces {
    fmt.Printf("%s(%s) -> %s\n", trace.Tool, string(trace.Args), trace.Output)
}
```

When `ToolMode` is `"native"` but the LLM does not implement `ToolCallingLLM`, the executor returns `ErrNativeToolsUnsupported`. The default (`""` or `"react"`) uses the existing ReAct loop — no changes to existing code. Details in [`docs/tools.md`](docs/tools.md).

## Web search

Web search is available in two patterns — agent-driven (Go code controls queries) and model-driven (the LLM decides when to search via the ReAct loop).

### Agent-driven: `WebSearcher` interface

For LLM providers that have a native web search API (Ollama Cloud, OpenAI, Anthropic, xAI), call `SearchWeb` directly from Go code:

```go
import "github.com/rhgs/crewai-go"

// OpenAI requires a search-capable model (e.g. gpt-4o-search-preview).
llm := openai.New("gpt-4o-search-preview")

hits, err := crewai.SearchWeb(ctx, llm, "Go programming language", 5)
if err != nil {
    // Returns ErrWebSearchUnsupported if the LLM doesn't implement WebSearcher.
    log.Fatal(err)
}
for _, hit := range hits {
    fmt.Printf("%s -- %s\n%s\n\n", hit.Title, hit.URL, hit.Content)
}
```

| Provider | How it works |
|----------|-------------|
| **Ollama Cloud** | `POST /api/web_search` — pure search endpoint, no model invocation, no tokens consumed |
| **OpenAI** | `web_search_options` in Chat Completions (NOT `tools`); requires search models (`gpt-4o-search-preview`, `gpt-5-search-api`); results as nested `url_citation` annotations |
| **Anthropic** | `web_search_20250305` server tool; results as `web_search_tool_result` blocks with `encrypted_content` |
| **xAI (Grok)** | Delegates to the OpenAI-compatible client |

### Model-driven: `WebSearchTool`

For the ReAct loop, use `WebSearchTool` so the LLM decides when to search. It implements both `Tool` and `FactSource`, so results are collected as `Fact`s with provenance.

```go
import "github.com/rhgs/crewai-go/tools"

// Wikipedia (default, free, no API key) — searches Wikipedia articles only.
search := tools.NewWebSearch(nil)

// LangSearch (100% free, semantic summaries) — general web search.
// Get a free key at https://langsearch.com
search := tools.NewWebSearch(tools.NewLangSearch("LANGSEARCH_API_KEY"))

// Serpstack (1000 free searches/month) — Google SERP data.
search := tools.NewWebSearch(tools.NewSerpstack("SERPSTACK_API_KEY"))

// Google Custom Search (requires API key + CSE ID).
search := tools.NewWebSearch(tools.NewGoogleSearch("GOOGLE_API_KEY", "GOOGLE_CSE_ID"))

// Brave Search (requires API key).
search := tools.NewWebSearch(tools.NewBraveSearch("BRAVE_API_KEY"))

// DuckDuckGo (no key, but may be blocked by captcha).
search := tools.NewWebSearch(tools.NewDuckDuckGoSearch())

agent.WithTools(search)
```

All search results are **SSRF-protected**: URLs pointing to localhost, private/CGNAT/multicast IPs, `0.0.0.0`, link-local addresses (`169.254.x`), metadata aliases, userinfo, and unspecified addresses are filtered out. Domain names are resolved via DNS to prevent DNS rebinding attacks (fail-closed).

Details in [`docs/llms.md`](docs/llms.md) (WebSearcher) and [`docs/tools.md`](docs/tools.md) (WebSearchTool).

## Logging

crewai-go uses [`log/slog`](https://pkg.go.dev/log/slog) (structured logging, Go 1.21+) from the standard library. Every log call is structured (key-value pairs), not format strings.

Inject a custom `*slog.Logger` via `WithLogger` on both `Crew` and `Agent`:

```go
log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
    Level: slog.LevelDebug,
}))

crew := crewai.NewCrew(agents, tasks).WithLogger(log)
agent := crewai.NewAgent("a", "g", "b", llm).WithLogger(log)
```

If no logger is injected, `Kickoff` creates a text-format logger on stderr. The default level depends on `Crew.Verbose`:

| `Verbose` | Default level | Effect                                                  |
|-----------|---------------|---------------------------------------------------------|
| `false`   | `LevelError`  | Only warnings and errors (matches legacy nop behavior). |
| `true`    | `LevelDebug`  | Everything: debug, info, warn, error.                   |

When `WithLogger` is used, the injected logger is used as-is — the caller controls the level and handler.

To mask likely secrets in log attributes and messages (best-effort, opt-in):

```go
log := slog.New(crewai.RedactHandler(
    slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}),
))
crew := crewai.NewCrew(agents, tasks).WithLogger(log)
```

See [`examples/logging/`](examples/logging/) for a full wiring sample. Prefer `LevelInfo` (or higher) in production; `LevelDebug` can include full LLM output and tool args.

Example log line (JSON handler):

```json
{"time":"...","level":"INFO","msg":"agent thought","agent":"Poet","output":"Final Answer: ..."}
```

Subpackages (`llm/*`, `tools/*`) do not log internally — they return errors that the executor logs at the appropriate level.

### Logging safety

> Debug-level logs include full LLM output (`agent thought` → the entire model response) and tool inputs/arguments. If your log destination is shared (e.g. remote log aggregator), the output may contain PII or proprietary model responses. Provider errors (logged at `WARN` on delegation failures, for example) may include API keys in their message.
>
> Mitigations:
>
> - Pick your destination accordingly (sink to local files, not a shared stream, when handling user data).
> - Use a level filter (e.g. `LevelError` only) to keep secrets out of logs by default.
> - Wrap your handler with a redactor. See [`examples/logging/`](examples/logging/) for a drop-in redaction handler that masks likely-secrets (API keys, bearer tokens, long alphanumeric tokens).

### Thread-safety

`WithLogger` is **not concurrent-safe**. Set the logger before calling `Kickoff`/`Execute` and do not mutate it concurrently. Multiple sequential calls to `WithLogger` are idempotent — the last call wins.

```go
log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
    Level: slog.LevelDebug,
}))

crew := crewai.NewCrew(agents, tasks).WithLogger(log)
agent := crewai.NewAgent("a", "g", "b", llm).WithLogger(log)
```

If no logger is injected, `Kickoff` creates a text-format logger on stderr. The default level depends on `Crew.Verbose`:

| `Verbose` | Default level | Effect                                                  |
|-----------|---------------|---------------------------------------------------------|
| `false`   | `LevelError`  | Only warnings and errors (matches legacy nop behavior). |
| `true`    | `LevelDebug`  | Everything: debug, info, warn, error.                   |

When `WithLogger` is used, the injected logger is used as-is — the caller controls the level and handler. `Agent.Execute` (standalone, without a crew) falls back to `slog.Default()` unless `Agent.WithLogger` is set.

Example log line (JSON handler):

```json
{"time":"...","level":"INFO","msg":"agent thought","agent":"Poet","output":"Final Answer: ..."}
```

Subpackages (`llm/*`, `tools/*`) do not log internally — they return errors that the executor logs at the appropriate level.

## Processes: Sequential, Hierarchical, and Staged

**Sequential** — tasks in order, each output becomes context for the next:

```go
crew := crewai.NewCrew(agents, tasks)
crew.Process = crewai.Sequential
```

**Hierarchical** — a manager delegates each task to the most suitable agent:

```go
crew.Process = crewai.Hierarchical
crew.ManagerLLM = llm // or crew.ManagerAgent = myManager
```

**Staged** — stages run in sequence, but the tasks within a single stage run
concurrently. The output of each stage is available as context to the tasks of
the following stages. A stage marked `Optional` does not abort the crew when
one of its tasks fails; otherwise the first failure aborts `Kickoff`.

```go
crew := crewai.NewCrew(agents, nil)
crew.Process = crewai.Staged
crew.Stages = []crewai.Stage{
    {Name: "collect", Tasks: []*crewai.Task{researchA, researchB}},
    {Name: "synthesize", Tasks: []*crewai.Task{write}},
}
```

Chain context explicitly with `WithContext`:

```go
analysis := crewai.NewTask("Analyze the data", "insights", analyst).
	WithContext(collection) // receives the output of the 'collection' task
```

## Agentic loop

By default an agent uses a single-pass ReAct executor. For tasks that benefit
from self-assessment and iterative refinement, set `Agent.Loop` (or
`Task.Loop`) to an `AgenticLoop`:

```go
agent.Loop = crewai.NewAgenticLoop(
    crewai.WithMaxRefinements(3),
    crewai.WithPassThreshold(80),
    crewai.WithEvaluator(evaluatorAgent), // optional independent evaluator
)
```

The loop follows a **Plan → Execute → Evaluate → Refine** cycle. If the output
never passes evaluation, `Kickoff` returns `ErrEvaluationFailed`. See
[docs/agents.md](docs/agents.md) and [examples/agentic_loop](examples/agentic_loop).

## Structured output

When a task needs typed, trustworthy data (e.g. for persisting into a
database), set the `Structured` field with a JSON Schema. The executor
instructs the model to reply with JSON only, validates the output in Go,
and retries up to `RepairMax` times if validation fails.

```go
schema := map[string]any{
    "type": "object",
    "properties": map[string]any{
        "name":  map[string]any{"type": "string"},
        "count": map[string]any{"type": "integer"},
    },
    "required": []string{"name", "count"},
}

structured, _ := crewai.NewStructuredOutput(schema, crewai.WithRepairMax(3))

task := crewai.NewTask("Extract the product name and count.", "JSON", agent)
task.Structured = structured
```

On success, `task.Output()` returns the **canonicalized** JSON string. If
the model never produces valid JSON within the repair budget, the task
fails with `crewai.ErrRepairBudgetExceeded`. The executor never returns
invalid JSON or invents data.

The built-in validator is a stdlib-only JSON Schema subset: `type`,
`properties`, `required`, `enum`, `items`, `additionalProperties`,
`minLength`/`maxLength` (**bytes**), numeric/array bounds, `pattern`,
and `oneOf`/`anyOf`/`allOf`. Use `WithStrictSchema()` to reject
unsupported keywords at construction. Optional `WithAllowTools()` runs a
tool gather phase before JSON capture. Details in
[`docs/tasks.md`](docs/tasks.md).

## Guardrails

Guardrails are code-enforced post-output validation hooks. They run after a
crew (or task) produces output and block publication if a business invariant
is violated. Unlike prompt-level instructions, guardrails are a hard code
guarantee.

```go
crew.Guardrails = []crewai.Guardrail{
    func(_ context.Context, out *crewai.CrewOutput) error {
        if !strings.Contains(out.Final, "http") {
            return fmt.Errorf("missing source URL")
        }
        return nil
    },
}
```

Task-level guardrails can also be set via `task.WithGuardrail(...)`. On
failure, `Kickoff` returns `crewai.ErrBlockedByGuardrail` — the output is
never partially returned. Details in [`docs/crews.md`](docs/crews.md).

## Facts & provenance

A **Fact** is a piece of data produced by a deterministic connector tool, not
by the LLM. It carries provenance (source, URL, timestamp, payload hash) so a
wrong value can never be presented as a "fact the model remembered".

```go
tool := crewai.NewFactSourceTool(
    "cnpj_lookup",
    "Looks up CNPJ status. Input: the CNPJ number.",
    func(_ context.Context, cnpj string) (string, error) { /* ... */ },
    func(_ context.Context, output string) []crewai.Fact {
        return []crewai.Fact{
            crewai.NewFact(output, "Receita Federal", "https://...", []byte(rawPayload)),
        }
    },
)
```

Facts flow only from tools, never from the model. They are deduplicated by
PayloadHash and appear in `CrewOutput.Facts` and `TaskOutput.Facts`. Use
`crewai.AllFactsProvenanced` in a guardrail to enforce provenance. Details in
[`docs/tools.md`](docs/tools.md).

## MCP (Model Context Protocol)

Connect to external MCP servers over Streamable HTTP and expose their tools
as `crewai.Tool` values. The original `inputSchema` is preserved
(`SchemaProvider`) so native tool calling receives the real schema.

```go
import "github.com/rhgs/crewai-go/mcp"

clients, err := mcp.LoadConfig(ctx, "/etc/mcp.json", "my-app", "1.0.0")
// or: client := mcp.New(endpoint, mcp.WithHTTPTimeout(30*time.Second),
//     mcp.WithHeader("Authorization", "Bearer "+tok))
var tools []crewai.Tool
for _, c := range clients {
    ts, _ := c.ListTools(ctx)
    // Optional least-privilege filter (deny-by-default for unlisted names):
    // ts = mcp.FilterTools(ts, map[string]struct{}{"search_docs": {}})
    for _, t := range ts {
        tools = append(tools, mcp.NewToolAdapter(c, t, mcp.WithDescriptionLimit(500)))
    }
}
agent.WithTools(tools...)
```

Default HTTP timeout is 30s (`DefaultHTTPTimeout`); JSON config accepts
per-server `"timeout"`. See [`docs/en/mcp.md`](docs/en/mcp.md) for
configuration, threat model, catalog guards, and the programmatic API.

## Progress & warnings

**Progress.** Surface real-time execution events to a frontend via
`Crew.WithProgress`. Events: `stage_started`, `stage_completed`,
`task_started`, `task_completed`, `tool_invoked`. Payloads carry metadata
only (never prompts, outputs, or tool inputs). The callback must be
goroutine-safe; panics are recovered.

```go
crew.WithProgress(func(p crewai.Progress) {
    fmt.Printf("%s %s/%s tool=%s\n", p.Event, p.Stage, p.Task, p.Tool)
})
```

**Warnings.** A task that succeeds can still record non-fatal diagnostics
(`Task.AddWarning` / `crewai.AddWarningFromCtx`). They aggregate into
`TaskOutput.Warnings` and `CrewOutput.Warnings` — distinct from
`Stage.Optional`, which swallows task *failures*.

```go
crewai.AddWarningFromCtx(ctx, "secondary source timeout")
```

Details in [`docs/crews.md`](docs/crews.md) and [`docs/tasks.md`](docs/tasks.md).

## Inter-agent delegation

```go
researcher.AllowDelegation = true // eligible target
crew.EnableDelegationTool = true  // auto-attach delegate_to_coworker
// or: writer.WithTools(crewai.NewDelegationTool(crew))
```

Depth/cycle/self guards apply (`DefaultMaxDelegationDepth` = 2). See
[`docs/agents.md`](docs/agents.md) and [`examples/delegation`](examples/delegation).

## Memory

```go
crew.Memory = true
// ...
crew.Kickoff(ctx, nil)

for _, r := range crew.MemorySnapshot().Records() {
	fmt.Printf("[%s] %s\n", r.Agent, r.Content)
}
```

`*Memory` also implements the pluggable `crewai.MemoryStore` contract
(Put/Query/Delete/Close with entry/query caps) used by long-term memory
backends in M2+. See [docs/memory.md](docs/memory.md).

## Examples

Run the included examples:

```bash
go run ./examples/custom_llm     # offline, no API key
go run ./examples/ollama         # local Ollama (or OLLAMA_CLOUD=1)

export OPENAI_API_KEY=sk-...
go run ./examples/basic
go run ./examples/sequential
go run ./examples/hierarchical
go run ./examples/staged
go run ./examples/agentic_loop   # offline, mock LLM
go run ./examples/tools

export XAI_API_KEY=xai-...        # or XAI_OAUTH=1 + XAI_CLIENT_ID
go run ./examples/xai_oauth
```

## Documentation

> 📖 All docs are available in **English** (default) and **Português** (`*.pt-BR.md` / `docs/pt-BR/`). Each file has a language switch link at the top.

### Core guides

| Guide | English | Português |
|------|---------|-----------|
| Getting Started | [EN](docs/getting-started.md) | [PT](docs/pt-BR/getting-started.md) |
| Agents | [EN](docs/agents.md) | [PT](docs/pt-BR/agents.md) |
| Tasks | [EN](docs/tasks.md) | [PT](docs/pt-BR/tasks.md) |
| Crews | [EN](docs/crews.md) | [PT](docs/pt-BR/crews.md) |
| Tools | [EN](docs/tools.md) | [PT](docs/pt-BR/tools.md) |
| LLMs | [EN](docs/llms.md) | [PT](docs/pt-BR/llms.md) |
| Memory | [EN](docs/memory.md) | [PT](docs/pt-BR/memory.md) |
| MCP | [EN](docs/en/mcp.md) | [PT](docs/pt-BR/mcp.md) |
| Plan / Roadmap | [EN](Plan/PLAN.md) | [PT](Plan/PLAN.pt-BR.md) |
| Security policy | [EN](SECURITY.md) | — |

### What's new in v0.5.0

All features are **backward compatible** — no breaking changes.

| Feature | Description | Docs (EN) | Docs (PT) |
|---------|-------------|-----------|-----------|
| **MCP hardening** | Default 30s HTTP timeout; `WithHTTPTimeout`; JSON `timeout`; `FilterTools` + `WithDescriptionLimit`; threat-model docs. | [docs/en/mcp.md](docs/en/mcp.md) | [docs/pt-BR/mcp.md](docs/pt-BR/mcp.md) |
| **OutputFile jail** | Path clean + optional `OutputDir` with symlink-aware checks (`ErrOutputPathRejected`). | [docs/tasks.md](docs/tasks.md) | [docs/pt-BR/tasks.md](docs/pt-BR/tasks.md) |
| **RedactHandler** | Opt-in `slog.Handler` secret masking in the root package. | [README §Logging](#logging) | [README.pt-BR](README.pt-BR.md#logging) |
| **Kickoff single-flight** | Concurrent `Kickoff` on the same `*Crew` returns `ErrCrewRunning`. | [docs/crews.md](docs/crews.md) | [docs/pt-BR/crews.md](docs/pt-BR/crews.md) |
| **Schema keywords** | Expanded JSON Schema subset + `WithStrictSchema` / `WithAllowTools`. | [docs/tasks.md](docs/tasks.md) | [docs/pt-BR/tasks.md](docs/pt-BR/tasks.md) |
| **Delegation tool** | `delegate_to_coworker` + `EnableDelegationTool` (opt-in). | [docs/agents.md](docs/agents.md) | [docs/pt-BR/agents.md](docs/pt-BR/agents.md) |

**Also in v0.4.x**: staged process, agentic loop, MCP client, progress callbacks, per-task warnings, structured `emit_result`.

**Also in v0.3.0**: native tool calling, web search (agent-driven + model-driven), structured logging via `log/slog`, secret redaction.

See the [CHANGELOG](CHANGELOG.md) for the full list of changes and the [v0.5.0 release](https://github.com/rhgs/crewai-go/releases/tag/v0.5.0) for details.


## Tests

```bash
go test ./...            # all tests
go test ./... -cover     # with coverage
go vet ./...             # static analysis
```

Tests are **hermetic**: they use the `mock` LLM and `httptest`, with no real network calls.

## Comparison with CrewAI (Python)

### API mapping

| CrewAI (Python)        | crewai-go                         |
|------------------------|-----------------------------------|
| `Agent(role=...)`      | `crewai.NewAgent(role, ...)`      |
| `Task(description=...)`| `crewai.NewTask(desc, ...)`       |
| `Crew(agents, tasks)`  | `crewai.NewCrew(agents, tasks)`   |
| `crew.kickoff(inputs)` | `crew.Kickoff(ctx, inputs)`       |
| `Process.sequential`   | `crewai.Sequential`               |
| `Process.hierarchical` | `crewai.Hierarchical`             |
| `Process.staged`       | `crewai.Staged`                   |
| `@tool` / `BaseTool`   | `crewai.NewTool` / `crewai.Tool`  |
| litellm                | `LLM` interface (openai/anthropic)|

### Features in crewai-go that the original CrewAI does NOT have

| Feature | crewai-go | CrewAI (Python) |
|---------|-----------|-----------------|
| **Zero dependencies** | ✅ stdlib only — no external packages | ❌ 50+ PyPI packages (litellm, langchain, pydantic, chromadb, etc.) |
| **Staged process** | ✅ stages in sequence, tasks within a stage concurrent; optional stages continue on failure | ❌ no staged/parallel-within-stage process |
| **Agentic loop** | ✅ opt-in Plan-Execute-Evaluate-Refine with independent evaluator and bounded refinements | ⚠️ agentic workflows exist, but not as a first-class Plan-Execute-Evaluate-Refine loop with score threshold |
| **Native tool calling with fallback** | ✅ `Agent.ToolMode` auto-falls back to ReAct if provider doesn't support `ToolCallingLLM` | ❌ no automatic fallback; requires compatible provider |
| **Facts & provenance** | ✅ first-class `Fact` type with `source_org`, `source_url`, `payload_hash`, `collection_time` — populated only by deterministic tools, never by the LLM | ❌ no provenance tracking; LLM can hallucinate "facts" |
| **Guardrails** | ✅ crew-level (`Crew.Guardrails`) + task-level (`Task.Guardrail`) post-output validation that blocks publication of invalid outputs | ❌ no built-in post-output validation hooks |
| **Structured output with repair loop** | ✅ JSON Schema validation with bounded repair loop (`RepairMax`, default 2) and `ErrRepairBudgetExceeded` | ⚠️ partial — uses Pydantic, no repair loop |
| **Web search (agent-driven)** | ✅ `WebSearcher` interface + `SearchWeb(ctx, llm, query, max)` — direct search from Go code via Ollama, OpenAI, Anthropic, xAI | ❌ no direct search API; requires tools |
| **Web search (model-driven)** | ✅ `WebSearchTool` with 7 pluggable providers (Wikipedia, LangSearch, Serpstack, DuckDuckGo, Google, Brave) | ⚠️ requires SerperDev or similar external tool integration |
| **SSRF protection** | ✅ blocks non-http(s), userinfo, loopback, private/CGNAT/multicast IPs, link-local, unspecified, metadata aliases; DNS rebinding prevention via `net.LookupIP` (fail-closed) | ❌ no URL filtering on search results |
| **Secret redaction in logs** | ✅ `redactError`/`redactString` + opt-in `RedactHandler` for `slog` messages/attrs | ❌ no log redaction |
| **Structured logging** | ✅ `log/slog` — inject any `*slog.Logger` with custom handler, level, and output | ❌ Python `logging` module, less flexible handler injection |
| **Tool call security limits** | ✅ max args size, output size, response size, JSON depth, arg validation — all configurable | ❌ no size/depth limits on tool calls |
| **Tool traces** | ✅ `ToolTrace` in `TaskOutput` — full observability of every tool call (name, args, result, duration) | ❌ no per-call trace type |
| **Context propagation** | ✅ `context.Context` throughout — cancellation, deadlines, tracing propagated to all LLM and tool calls | ❌ no native context/cancellation; async requires `asyncio` |
| **Thread-safe execution** | ✅ all providers safe for concurrent use; `-race` tested | ❌ Python GIL limits true concurrency |
| **Compile-time interface checks** | ✅ `var _ crewai.WebSearcher = (*Client)(nil)` — catches missing methods at build time | ❌ runtime duck-typing, no compile-time checks |
| **Single binary deployment** | ✅ compile to a single static binary — no runtime, no VM, no interpreter | ❌ requires Python runtime + virtualenv + dependencies |
| **Cold start** | ✅ milliseconds (native binary) | ❌ seconds (Python import + model loading) |
| **Memory footprint** | ✅ ~10-20 MB typical | ❌ ~100-300 MB typical (Python + deps) |
| **Cross-compilation** | ✅ `GOOS=linux GOARCH=arm64 go build` — any target from any host | ❌ requires target-platform Python or container |

This port covers the CrewAI core (agents, tasks, crews, processes, tools, memory) plus several original features not found in the Python version. Advanced features of the original project (event-driven Flows, training, telemetry) are not part of this version.

## Contributing

Contributions are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) (EN) /
[CONTRIBUTING.pt-BR.md](CONTRIBUTING.pt-BR.md) (PT) for setup, conventions, and
the PR checklist. Please also read our [Code of Conduct](CODE_OF_CONDUCT.md)
([PT](CODE_OF_CONDUCT.pt-BR.md)).

## License

[MIT](LICENSE).

## Changelog

See [CHANGELOG.md](CHANGELOG.md) (EN) / [CHANGELOG.pt-BR.md](CHANGELOG.pt-BR.md) (PT).
