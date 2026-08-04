# Getting Started

This guide takes you from zero to a working _crew_.

## Prerequisites

- **Go 1.24+** — check with `go version`.
- An API key for an LLM provider (e.g. `OPENAI_API_KEY`) — or use an offline/custom LLM.

## 1. Create a project

```bash
mkdir my-crew && cd my-crew
go mod init example.com/my-crew
go get github.com/rhgs/crewai-go@latest
```

## 2. Write the program

`main.go`:

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
	llm := openai.New("gpt-4o-mini")

	researcher := crewai.NewAgent(
		"Researcher",
		"Find relevant, reliable information",
		"You are an experienced, skeptical analyst.",
		llm,
	)

	task := crewai.NewTask(
		"Explain in 3 points why Go is good for back-end.",
		"A list of 3 short items.",
		researcher,
	)

	crew := crewai.NewCrew([]*crewai.Agent{researcher}, []*crewai.Task{task})
	crew.Verbose = true

	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(out.Final)
}
```

## 3. Run it

```bash
export OPENAI_API_KEY=sk-...
go run .
```

## 4. No API key? Run offline

You can implement the `crewai.LLM` interface yourself (see
[llms.md](llms.md)) or use the `mock` LLM from the test package. The
`examples/custom_llm` example runs fully offline:

```bash
go run github.com/rhgs/crewai-go/examples/custom_llm
```

## Next steps

- [Agents](agents.md) — configure roles, goals, and tools.
- [Tasks](tasks.md) — chain tasks with context and interpolate variables.
- [Crews](crews.md) — sequential and hierarchical processes.
- [Tools](tools.md) — give "hands" to your agents.