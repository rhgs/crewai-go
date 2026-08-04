# Plan — CrewAI port to Go (crewai-go)

> **Languages:** **English** (current) · [Português](PLAN.pt-BR.md)

A living document for the planning, architecture decisions, status, and roadmap
of porting [CrewAI](https://github.com/crewAIInc/crewAI) (Python) to Go.

---

## 1. Goal

Port the **core** of the CrewAI framework to Go in an **idiomatic** way, with
**no external dependencies** (stdlib only), **complete docs and tests**, and
**examples** for installation/usage.

## 2. Design principles

| Principle | Decision |
|-----------|---------|
| Zero dependencies | Go standard library only — easy to audit, install, and test. |
| LLM-agnostic | Minimal `LLM` interface; providers in subpackages. |
| Text-based tools | **ReAct** protocol (Thought/Action/Observation/Final Answer) — no dependency on each provider's native _function calling_. |
| Testable | `mock` LLM + `httptest`; hermetic tests (no real network). |
| Idiomatic | `context.Context` on all I/O, sentinel errors, _functional options_. |

## 2.1 Documentation conventions

| Convention | Rule |
|------------|------|
| Bilingual docs | `README.md`, `PLAN.md`, and all `*.md` docs are kept in **both** English (default) and Portuguese (`*.pt-BR.md` / `docs/pt-BR/`). |
| Language switch | Each doc has a `> **Languages:** …` link at the top to toggle between EN/PT. |
| Code comments | Always in **English, no accents**. Applies to godoc, error messages, prompts, and identifier strings. |
| Default language | English is the primary/authoritative version; PT is a faithful mirror kept in sync. |

## 3. CrewAI → crewai-go mapping

| CrewAI (Python)          | crewai-go                          |
|--------------------------|------------------------------------|
| `Agent`                  | `crewai.Agent` / `NewAgent`        |
| `Task`                   | `crewai.Task` / `NewTask`          |
| `Crew`                   | `crewai.Crew` / `NewCrew`          |
| `crew.kickoff(inputs)`   | `crew.Kickoff(ctx, inputs)`        |
| `Process.sequential`     | `crewai.Sequential`                |
| `Process.hierarchical`   | `crewai.Hierarchical`              |
| `BaseTool` / `@tool`     | `crewai.Tool` / `NewTool`          |
| short-term memory        | `crewai.Memory`                    |
| litellm                  | `LLM` interface + `llm/*` subpackages |

## 4. Architecture (packages)

```
crewai (root)          Agent, Task, Crew, Process, Tool, Memory, LLM, ReAct executor
├── llm/openai         OpenAI and compatible (Groq, Azure…) + WithTokenSource (OAuth)
├── llm/anthropic      Claude (separate system prompt)
├── llm/ollama         Ollama local (no auth) and cloud (Bearer) — native /api/chat
├── llm/xai            Grok: API key + subscription OAuth (Device Flow RFC 8628 + PKCE)
├── llm/mock           Deterministic LLM for tests
├── tools              Calculator, current time, word counter
├── examples           basic, sequential, hierarchical, tools, custom_llm, ollama, xai_oauth
└── docs               getting-started, agents, tasks, crews, tools, llms, memory
```

### Execution flow

1. `Crew.Kickoff` interpolates `{inputs}` into tasks.
2. Sequential: runs in order, chaining context/memory.
   Hierarchical: a manager (ManagerLLM/ManagerAgent) delegates each task.
3. Per task, the `executor` runs the ReAct loop: builds the prompt (persona +
   tools), calls the LLM, parses `Action`/`Final Answer`, runs tools, and
   repeats until the final answer or `MaxIterations`.

## 5. Current status — ✅ COMPLETE (phase 1)

- [x] Core: Agent, Task, Crew, Process, Tool, Memory, ReAct executor.
- [x] Sequential and hierarchical processes (with LLM delegation).
- [x] Context between tasks (`WithContext`) and `{inputs}` interpolation.
- [x] Providers: OpenAI(+compatible), Anthropic, Ollama local, Ollama Cloud, xAI (API key + OAuth), mock.
- [x] xAI subscription OAuth: Device Flow (RFC 8628) + PKCE + refresh + persistence.
- [x] Built-in tools (calculator with its own parser, time, word counter).
- [x] Hermetic tests (~90% core; providers via httptest) and `Example`s.
- [x] Documentation: README + 7 guides + godoc; 7 runnable examples.
- [x] Clean `go build`, `go vet`, and `go test ./...`.

### Maturity snapshot (2026-08-04)

| Metric | Value |
|---------|-------|
| Go LOC (total) | 3.826 |
| `.go` files | 41 |
| External dependencies | 0 (stdlib) |
| Coverage — core (`crewai`) | 90.0% |
| Coverage — `tools` | 92.1% |
| Coverage — `llm/*` providers | 71.7%–87.5% |
| Runnable examples | 7 |
| Documentation guides | 7 + README |

### Known limitations of phase 1

- ~~**No git**~~ — **resolved (2026-08-04):** repository initialized and published
  at https://github.com/rhgs/crewai-go.git (branch `main`), `.gitignore` covering
  `.claude/`, xAI OAuth tokens, and `.env`; MIT license kept (the remote came
  with GPL v3 from the GitHub template, resolved in favor of the MIT declared
  in the README). Only CI setup remains.
- **Simplified hierarchical** — the manager picks one agent per task via LLM,
  but there is no runtime inter-agent delegation (one agent calling another
  during reasoning). The `Agent.AllowDelegation` field is "informational in this
  version" (no practical effect).
- **No streaming** — `LLM.Call` returns the full response; there is no
  `CallStream(ctx, messages) (<-chan StreamChunk, error)`.
- **In-process memory only** — no persistence or semantic search.
- **No native function calling** — all tool usage goes through the text-based
  ReAct loop, which works with any provider but doesn't leverage native tools.

## 6. Roadmap — next steps (out of current scope)

Advanced features of the original CrewAI **not** yet ported, by suggested
priority (highest impact / lowest effort first):

### P0 — Publishing and fundamentals

- [ ] **`git init` + public repository** — version, set up CI (lint, test,
  examples build), tag `v0.1.0`.
  - [x] **`git init` + push** — done at `https://github.com/rhgs/crewai-go.git`.
  - [x] **Module path fixed** — `github.com/rodolphosa/crewai-go` →
    `github.com/rhgs/crewai-go` in `go.mod` + 27 files (imports, docs,
    examples). Publication consistent with the remote.
  - [x] **G306 (gosec) fixed** — `Task.setOutput` now writes with `0600`.
  - [x] **Full English translation** — README, PLAN, all docs/*.md, examples/README,
    godoc comments, error messages, prompts, tool names/descriptions, and test
    strings translated to English. `gofmt` clean; `go test -race` clean; offline
    example verified.
  - [x] **Bilingual docs rule** — English (default) + Portuguese versions
    restored (`README.pt-BR.md`, `PLAN.pt-BR.md`, `docs/pt-BR/`, `examples/pt-BR/`)
    with language-switch links. Code comments remain English-only. Documented in
    `PLAN.md §2.1` and the `_rules/documentation-language.md` memory page.
  - [ ] Set up CI (GitHub Actions: lint, `go vet`, `go test`, examples build).
  - [x] Tag `v0.1.0` — **published (2026-08-04)** with a bilingual CHANGELOG.
  - [ ] **Upgrade toolchain to Go 1.24.9+** — govulncheck reports 21 CVEs in
    the Go 1.24.4 stdlib (crypto/x509 etc.), fixed in 1.24.9. No code change,
    just bump `go.mod`/CI.
- [x] **Confirm module path** (`github.com/rodolphosa/crewai-go` → final
  destination) and update import paths in examples/docs. **Resolved (2026-08-04).**
- [x] **Document `go vet` and `-race` in CI** (tests already use concurrency).
  `go test -race ./...` is clean.

## 6.1 Code review and security validation (2026-08-04)

**Tools:** `go vet` ✅ · `gofmt -l` ✅ · `go test -race` ✅ · `gosec` ·
`govulncheck`.

### gosec — 2 remaining findings (false positives, no `//nolint`)

| ID | File | Verdict |
|----|---------|----------|
| G304 | `llm/xai/oauth.go:314` `os.ReadFile(path)` | **False positive.** `path`
  is provided by the library caller (e.g. `~/.crewai-xai-token.json`), not by an
  attacker. A library cannot restrict the path the user chooses. |
| G117 | `llm/xai/oauth.go:305` marshal of `AccessToken` | **False positive.** The
  file's purpose is precisely to persist the token; there is no exposure. |

There are no `//nolint` directives in the code (`Nosec: 0`). The false positives
are intentionally left unsuppressed so as not to mask real findings in the
future.

### govulncheck — 21 CVEs in the stdlib (Go 1.24.4)

All in `crypto/x509` and related, via the providers' TLS paths
(`ollama.NewCloud` → `x509.ParseCertificate`, etc.). **Fixed in Go 1.24.9.**
There is no vulnerability in the project's code; just bump the toolchain in
`go.mod` and CI.

### Manual code review — points of attention

- **`Crew.Kickoff` mutates `Task.Description`/`ExpectedOutput` in `interpolate`
  without holding `Task.mu`.** Safe in normal use (Kickoff is called once, the
  flow is single-threaded), but **not safe for reusing the same `Crew` across
  concurrent goroutines**. Document or protect with a lock if P1 (async) is
  implemented.
- **OAuth Device Flow** (`llm/xai/oauth.go`): correct implementation — PKCE with
  `crypto/rand` (32 bytes), S256, polling honors `authorization_pending`/`slow_down`
  (RFC 8628 §3.5), `refreshingSource` with `sync.Mutex`, `0600` persistence.
  No hardcoded `client_id`/endpoints (configurable via options).
- **Provider auth** (`llm/openai`): dynamic `TokenSource` (OAuth) takes
  precedence over the static key; token resolved per call → auto-renewal.
  Correct.
- **`delegate` (hierarchical)** gracefully falls back to the first agent on LLM
  error — does not stall execution.
- No `TODO`/`FIXME`/`HACK` in production code.

### Secret validation

Full scan (tracked files + commit history): **no key, token, or credential
published.** Mentions of secrets are only placeholders (`sk-...`, `xai-...`)
and environment variable names in docs/README. The `.gitignore` protects
`.claude/`, `.env`, `*token.json`.

### P1 — Core extensibility

- [ ] **Streaming** — add `CallStream(ctx, messages) (<-chan StreamChunk, error)`
  to the `LLM` interface (or an optional `StreamingLLM` interface) and propagate
  in the executor.
- [ ] **Native function calling** per provider (OpenAI/Anthropic) as an
  alternative to the text-based ReAct — keep ReAct as the universal fallback.
- [ ] **Async/parallelism** of tasks (`async_execution`) with `sync.WaitGroup`
  / an errgroup-like pattern using goroutines and channels.
- [ ] **Real delegation** between agents — a "Delegate work to coworker" tool
  that lets an agent invoke another during reasoning (removes the inertia of
  `AllowDelegation`).

### P2 — Persistence and observability

- [ ] **Long-term memory** with embeddings/semantic search and persistence
  (`MemoryStore` interface + file/sqlite implementation as default).
- [ ] **Callbacks and telemetry** — task and ReAct-iteration start/end hooks,
  exportable (structured logs, metrics).
- [ ] **Guardrails** — output validation/transformation with retry.

### P3 — Advanced

- [ ] **Flows** (event-driven orchestration with state and routing).
- [ ] More tools (web search, HTTP, files, RAG).
- [ ] Declarative YAML definition (agents.yaml/tasks.yaml).
- [ ] _Training_ (prompt fine-tuning from executions).

## 7. Open decisions

- **Module path**: currently `github.com/rhgs/crewai-go`. Adjust on publishing.
- **xAI OAuth**: `client_id` and exact endpoints are not public; implemented
  per RFC 8628 with configurable endpoints. Fix the defaults when xAI publishes
  the official documentation.
- **Streaming**: decide whether the `LLM` interface gets a `CallStream` method
  (breaks existing implementations) or whether we introduce an optional
  `StreamingLLM` interface that the executor checks via type assertion.
- **Function calling vs ReAct**: keep ReAct as the universal tool layer and add
  native function calling as an optional per-provider optimization, or migrate
  completely to function calling? Leaning toward the former.
