# dev.to draft

**Title:**
From Python to Go: rewriting a CrewAI workflow in pure stdlib

**Cover image suggestion:** use `gopher.jpg` cropped to 1000×420.

**Tags:** `go`, `ai`, `tutorial`, `openai`

---

## Body

In the [CrewAI](https://github.com/crewAIInc/crewAI) Python framework you
assemble teams of agents that collaborate via LLMs. It’s a great model
when tasks require multiple specialists. But pulling in `litellm`,
`langchain`, `pydantic` and friends makes cold starts heavy and
deployment a container story.

A few months ago I started **crewai-go**, an idiomatic Go port with
**zero external dependencies**. The whole core package is pure
`net/http`, `encoding/json`, `log/slog` and friends.

This post walks through a minimal port of the canonical CrewAI “research
→ write” example, side-by-side with the Python version.

### Python (CrewAI)

```python
from crewai import Agent, Crew, Process, Task

researcher = Agent(
    role="Senior Researcher",
    goal="Uncover the best practices in concurrency",
    backstory="You are a veteran engineer with deep distributed-systems chops.",
)

writer = Agent(
    role="Tech Writer",
    goal="Write a concise summary",
    backstory="You turn research into tight prose.",
)

research = Task(description="Research Go concurrency best practices",
                expected_output="Bullet list of 5 best practices", agent=researcher)
write    = Task(description="Write a 1-paragraph summary from the research",
                expected_output="A single paragraph", agent=writer)

crew = Crew(agents=[researcher, writer], tasks=[research, write],
            process=Process.sequential)
print(crew.kickoff().final)
```

### Go (crewai-go)

```go
package main

import (
    "context"
    "fmt"

    "github.com/rhgs/crewai-go"
    "github.com/rhgs/crewai-go/llm/openai"
)

func main() {
    llm := openai.New("gpt-4o-mini") // uses OPENAI_API_KEY
    researcher := crewai.NewAgent(
        "Senior Researcher",
        "Uncover the best practices in concurrency",
        "You are a veteran engineer with deep distributed-systems chops.",
        llm,
    )
    writer := crewai.NewAgent(
        "Tech Writer",
        "Write a concise summary",
        "You turn research into tight prose.",
        llm,
    )

    research := crewai.NewTask(
        "Research Go concurrency best practices",
        "Bullet list of 5 best practices",
        researcher,
    )
    write := crewai.NewTask(
        "Write a 1-paragraph summary from the research",
        "A single paragraph",
        writer,
    ).WithContext(research) // explicit dependency, like CrewAI’s context

    crew := crewai.NewCrew([]*crewai.Agent{researcher, writer},
                           []*crewai.Task{research, write})
    out, _ := crew.Kickoff(context.Background(), nil)
    fmt.Println(out.Final)
}
```

### Same surface, different runtime

- Same concepts: `Agent`, `Task`, `Crew`, `Process`.
- Sequential, Hierarchical and **Staged** processes (stages run in
  sequence; tasks within a stage run concurrently).
- **Agentic loop** (opt-in Plan–Execute–Evaluate–Refine) with an
  independent evaluator and bounded refinements.
- Web search, structured output with JSON Schema repair loop, guardrails,
  facts & provenance — all of it built on stdlib only.

### Why port it?

- **Single binary** (`GOOS=linux GOARCH=arm64 go build` from your laptop).
- **Millisecond** cold start, **~10–20 MB** memory footprint.
- **Thread-safe** end-to-end; CI runs `go test -race`.
- **~94% test coverage**, hard 90% gate.

### Try it

```bash
git clone https://github.com/rhgs/crewai-go
cd crewai-go
export OPENAI_API_KEY=sk-...
go run ./examples/sequential
```

There’s also `examples/agentic_loop` which runs **fully offline** with a
mock LLM — no API key needed.

If you’re a Go developer working on LLM orchestration, give it a star,
file an issue, or open a PR. The CONTRIBUTING guide is bilingual
(English + Brazilian Portuguese) and the PR checklist is short.

Repo: https://github.com/rhgs/crewai-go
v0.4.0 release: https://github.com/rhgs/crewai-go/releases/tag/v0.4.0

Happy hacking.
