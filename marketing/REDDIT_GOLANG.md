# r/golang draft — “Show & Tell”

**Title:**
crewai-go v0.4.0: idiomatic Go port of CrewAI — zero deps, stdlib only

**Body:**

Hi r/golang,

I just released **v0.4.0** of `crewai-go`, an idiomatic Go port of
[CrewAI](https://github.com/crewAIInc/crewAI). It's a multi-agent
orchestration framework where you assemble teams of agents that
collaborate via LLMs to complete complex tasks.

What makes it interesting for the Go community:

- **Zero external dependencies** — pure stdlib. No `go.sum` is required
  for the core package (you can audit the entire codebase with grep).
- **Single binary** deployment, millisecond cold start.
- **Concurrent by design** — the new `Staged` process runs tasks within
  a stage in parallel goroutines, with proper cancellation and first-error
  tracking.
- **`context.Context`** is propagated through every LLM and tool call.
- **`-race`** tested, **~94% coverage**, hard **90% gate** in CI.
- **Cross-compile** to any GOOS/GOARCH with one command.

What it ships with:

- Processes: Sequential, Hierarchical, Staged (new in v0.4.0).
- ReAct loop + provider-native tool calling (OpenAI, Anthropic, Ollama)
  with automatic fallback.
- Agentic loop (opt-in Plan–Execute–Evaluate–Refine).
- Web search: 7 providers with SSRF + DNS rebinding protection.
- Structured output with JSON Schema + repair loop.
- Guardrails, facts & provenance, memory.
- 13 working examples, including one that runs **fully offline** with a
  mock LLM.
- Bilingual docs (EN + pt-BR).

Repo: https://github.com/rhgs/crewai-go
Release notes: https://github.com/rhgs/crewai-go/releases/tag/v0.4.0

Quick start:

```go
crew := crewai.NewCrew(
    []*crewai.Agent{researcher, writer},
    []*crewai.Task{research, write},
)
out, _ := crew.Kickoff(ctx, nil)
fmt.Println(out.Final)
```

If you have any feedback, questions, or want to compare it with the
Python original, I'd love to discuss. PRs welcome — CONTRIBUTING is
bilingual too.

---

## Posting tips

1. Submit at https://www.reddit.com/r/golang/submit
2. Tag with `flair: show` (Show & Tell) if you have the option.
3. Avoid emojis and link-bait in the title.
4. Engage every top-level comment in the first hour; Reddit punishes
   low-engagement threads.
5. **No** cross-posting to r/programming until the r/golang thread has
   settled.
