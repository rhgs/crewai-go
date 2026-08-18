# Show HN draft — Hacker News

**Title (≤ 80 chars):**
crewai-go: idiomatic Go port of CrewAI, zero dependencies

**Body:**

Hi HN,

I just shipped **v0.4.0** of **crewai-go**, an idiomatic Go port of
[CrewAI](https://github.com/crewAIInc/crewAI) (Python). It lets you build
teams of agents that collaborate via LLMs to complete tasks.

The whole library is **zero external dependencies** — pure Go standard
library. No Pydantic, no langchain, no litellm, no chromadb. That means
the binary is small, the build is fast, the audit surface is small, and
it cross-compiles to any GOOS/GOARCH with a single command.

What it does today:

- **Sequential**, **Hierarchical**, and **Staged** process orchestration
  (stages run sequentially; tasks within a stage run concurrently).
- **ReAct** loop + provider-native tool calling (OpenAI, Anthropic,
  Ollama) with automatic fallback to ReAct when unsupported.
- **Agentic loop** (opt-in Plan–Execute–Evaluate–Refine) with independent
  evaluator and bounded refinements.
- **Web search**: 7 pluggable providers (Wikipedia, LangSearch,
  Serpstack, DuckDuckGo, Google, Brave) with SSRF protection and DNS
  rebinding prevention.
- **Structured output** with JSON Schema validation and a bounded
  repair loop.
- **Guardrails**, **facts & provenance**, **memory**, `context.Context`
  propagation end-to-end.
- **Bilingual docs** (EN + pt-BR).

It ships with a **mock LLM** so every example runs offline; CI runs with
`-race`, ~94% coverage, and a hard 90% gate.

Repo: https://github.com/rhgs/crewai-go
v0.4.0 release notes: https://github.com/rhgs/crewai-go/releases/tag/v0.4.0

Happy to answer technical questions about the port, the trade-offs vs.
the Python original, or the multi-agent design.

---

## Posting tips

1. Submit to **Show HN** at https://news.ycombinator.com/show
2. Title must match the **HN rules** for Show HN: must start with “Show HN:”
   — change first line to: `Show HN: crewai-go – idiomatic Go port of CrewAI, zero dependencies`
3. Best posting hours: weekday 8–10am US Eastern, or Tuesday–Wednesday.
4. Reply to **every comment** within the first 2 hours; that’s when HN
   ranks a thread.
5. No promotional wording; HN users punish it.

---

## Field-tested alt titles (pick one)

- `Show HN: crewai-go – multi-agent LLM orchestration in pure Go, zero deps`
- `Show HN: crewai-go – an idiomatic Go port of CrewAI (zero deps, stdlib)`
- `Show HN: crewai-go – stdlib-only multi-agent orchestration for Go`
