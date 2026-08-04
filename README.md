# crewai-go

> **Languages:** **English** (current) · [Português](README.pt-BR.md)

[![Go Reference](https://pkg.go.dev/badge/github.com/rhgs/crewai-go.svg)](https://pkg.go.dev/github.com/rhgs/crewai-go)

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
- [Processes: Sequential and Hierarchical](#processes-sequential-and-hierarchical)
- [Memory](#memory)
- [Examples](#examples)
- [Documentation](#documentation)
- [Tests](#tests)
- [Comparison with CrewAI (Python)](#comparison-with-crewai-python)
- [License](#license)

---

## Why crewai-go

- 🧩 **Simple, composable API** — `Agent`, `Task`, `Crew`, `Tool`, `LLM`.
- ⚡ **No dependencies** — stdlib only; small, fast builds.
- 🔌 **Any LLM** — OpenAI (and compatible: Ollama, Groq, Azure…), Anthropic (Claude), or your own implementation of the `LLM` interface.
- 🛠️ **Tools via ReAct** — agents reason and call tools in plain text.
- 🧠 **Memory** between tasks and chainable **context**.
- 👔 **Hierarchical process** with a manager that delegates dynamically.
- ✅ **Testable** — mock LLM included; ~90% core coverage.

## Concepts

| Concept     | What it is                                                              |
|--------------|-------------------------------------------------------------------------|
| **Agent**    | A worker with a role, a goal, a backstory, an LLM, and tools.           |
| **Task**     | A unit of work with a description, expected output, and an assignee.   |
| **Crew**     | The team: groups agents and tasks and orchestrates them.                |
| **Process**  | Execution strategy: `Sequential` or `Hierarchical`.                    |
| **Tool**     | A capability an agent can invoke (calculation, search, API…).          |
| **LLM**      | Abstraction over the language model. Several providers ready to use.   |
| **Memory**   | Stores task outputs to give context to following tasks.                |

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

## Processes: Sequential and Hierarchical

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

Chain context explicitly with `WithContext`:

```go
analysis := crewai.NewTask("Analyze the data", "insights", analyst).
	WithContext(collection) // receives the output of the 'collection' task
```

## Memory

```go
crew.Memory = true
// ...
crew.Kickoff(ctx, nil)

for _, r := range crew.MemorySnapshot().Records() {
	fmt.Printf("[%s] %s\n", r.Agent, r.Content)
}
```

## Examples

Run the included examples:

```bash
go run ./examples/custom_llm     # offline, no API key
go run ./examples/ollama         # local Ollama (or OLLAMA_CLOUD=1)

export OPENAI_API_KEY=sk-...
go run ./examples/basic
go run ./examples/sequential
go run ./examples/hierarchical
go run ./examples/tools

export XAI_API_KEY=xai-...        # or XAI_OAUTH=1 + XAI_CLIENT_ID
go run ./examples/xai_oauth
```

## Documentation

> 📖 All docs are available in **English** (default) and **Português** (`*.pt-BR.md` / `docs/pt-BR/`). Each file has a language switch link at the top.

| Guide | English | Português |
|------|---------|-----------|
| Getting Started | [EN](docs/getting-started.md) | [PT](docs/pt-BR/getting-started.md) |
| Agents | [EN](docs/agents.md) | [PT](docs/pt-BR/agents.md) |
| Tasks | [EN](docs/tasks.md) | [PT](docs/pt-BR/tasks.md) |
| Crews | [EN](docs/crews.md) | [PT](docs/pt-BR/crews.md) |
| Tools | [EN](docs/tools.md) | [PT](docs/pt-BR/tools.md) |
| LLMs | [EN](docs/llms.md) | [PT](docs/pt-BR/llms.md) |
| Memory | [EN](docs/memory.md) | [PT](docs/pt-BR/memory.md) |
| Plan / Roadmap | [EN](PLAN.md) | [PT](PLAN.pt-BR.md) |

## Tests

```bash
go test ./...            # all tests
go test ./... -cover     # with coverage
go vet ./...             # static analysis
```

Tests are **hermetic**: they use the `mock` LLM and `httptest`, with no real network calls.

## Comparison with CrewAI (Python)

| CrewAI (Python)        | crewai-go                         |
|------------------------|-----------------------------------|
| `Agent(role=...)`      | `crewai.NewAgent(role, ...)`      |
| `Task(description=...)`| `crewai.NewTask(desc, ...)`       |
| `Crew(agents, tasks)`  | `crewai.NewCrew(agents, tasks)`   |
| `crew.kickoff(inputs)` | `crew.Kickoff(ctx, inputs)`       |
| `Process.sequential`   | `crewai.Sequential`               |
| `Process.hierarchical` | `crewai.Hierarchical`             |
| `@tool` / `BaseTool`   | `crewai.NewTool` / `crewai.Tool`  |
| litellm                | `LLM` interface (openai/anthropic)|

This port covers the CrewAI core (agents, tasks, crews, processes, tools, memory). Advanced features of the original project (event-driven Flows, training, telemetry) are not part of this version.

## License

[MIT](LICENSE).

## Changelog

See [CHANGELOG.md](CHANGELOG.md) (EN) / [CHANGELOG.pt-BR.md](CHANGELOG.pt-BR.md) (PT).
