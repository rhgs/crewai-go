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

## 3.1 Features exclusive to crewai-go (not in CrewAI Python)

The following features are original to crewai-go and have no direct
equivalent in the Python CrewAI framework:

| Feature | What it does | Why it matters |
|---|---|---|
| **Structured output with JSON Schema validation** | `Task.Structured` (`*StructuredOutput`) requires the model to emit JSON validated against a JSON Schema, with a bounded repair loop (`RepairMax`). Validation is done in Go, not via prompt. | Guarantees typed, trustworthy data for pipelines that persist facts into databases. No external validator dependency. |
| **Post-output guardrails** | `Crew.Guardrails` and `Task.Guardrail` are code-enforced post-output validation hooks that block publication of outputs violating business invariants. Returns `ErrBlockedByGuardrail`. | The "anti-hallucination barrier as a code guarantee" layer. Decisions to block are made in Go, not via prompt. Complements structured-output schema validation (shape vs. meaning). |
| **Facts & provenance model** | First-class `Fact` type populated ONLY by deterministic connector tools (`FactSource`), never by the LLM. Facts carry source org, source URL, collection time, and SHA-256 payload hash. `AllFactsProvenanced` helper for guardrails. | A wrong value can never be presented as a "fact the model remembered". Facts are deduplicated by payload hash and carry full provenance for auditing. |
| **Zero external dependencies** | The entire framework uses only the Go standard library. No pip install, no version conflicts. | Easy to audit, install, and test. `go.mod` has zero require directives. |

## 4. Architecture (packages)

```
crewai (root)          Agent, Task, Crew, Process, Tool, Memory, LLM, ReAct/native executor,
                       StructuredOutput, Guardrails, Facts, Progress, RedactHandler,
                       DelegationTool
├── llm/openai         OpenAI and compatible (Groq, Azure…) + ToolCallingLLM + WebSearcher
├── llm/anthropic      Claude + ToolCallingLLM + WebSearcher
├── llm/ollama         Ollama local/cloud — native /api/chat + tools + web search
├── llm/xai            Grok: API key + subscription OAuth (Device Flow RFC 8628 + PKCE)
├── llm/mock           Deterministic LLM for tests
├── mcp                MCP Streamable HTTP client, LoadConfig, ToolAdapter, FilterTools
├── tools              Calculator, time, word count, WebSearchTool + search providers
├── examples           basic, sequential, hierarchical, staged, tools, native_tools,
                       agentic_loop, facts, guardrails, logging, delegation, mcp, …
└── docs               getting-started, agents, tasks, crews, tools, llms, memory, en/mcp
```

### Execution flow

1. `Crew.Kickoff` enforces single-flight (`ErrCrewRunning` if re-entered),
   optionally attaches `delegate_to_coworker` when `EnableDelegationTool`,
   interpolates `{inputs}`, and injects progress + agent role into `ctx`.
2. Process: Sequential | Hierarchical (manager assigns agent) | Staged
   (stages in order; tasks within a stage concurrent).
3. Per task, the executor picks: AgenticLoop (if set) → StructuredOutput
   (optional AllowTools gather → capture / emit_result) → native
   ToolCallingLLM (`ToolModeNative`) → ReAct text loop. Facts, tool traces,
   and non-fatal warnings aggregate into `CrewOutput`.

## 5. Current status — ✅ COMPLETE (phase 1)

- [x] Core: Agent, Task, Crew, Process, Tool, Memory, ReAct executor.
- [x] Sequential and hierarchical processes (with LLM delegation).
- [x] Context between tasks (`WithContext`) and `{inputs}` interpolation.
- [x] Providers: OpenAI(+compatible), Anthropic, Ollama local, Ollama Cloud, xAI (API key + OAuth), mock.
- [x] xAI subscription OAuth: Device Flow (RFC 8628) + PKCE + refresh + persistence.
- [x] Built-in tools (calculator with its own parser, time, word counter).
- [x] Hermetic tests (~90% core; providers via httptest) and `Example`s.
- [x] **Structured output** — `Task.Structured` with JSON Schema validation,
  repair loop, `WithToolCall` (emit_result), `WithAllowTools` (gather→capture),
  expanded keywords (v0.5.0). Sentinels `ErrInvalidOutput`,
  `ErrRepairBudgetExceeded`, `ErrToolCallStructuredUnsupported`.
- [x] Documentation: bilingual README + guides + MCP + SECURITY; 14 examples.
- [x] Clean `go build`, `go vet`, and `go test ./...`.

### Maturity snapshot (2026-08-20 — v0.5.0)

| Metric | Value |
|---------|-------|
| Latest release | **v0.5.0** (2026-08-20) |
| Go LOC (approx.) | ~23k |
| `.go` files | ~111 |
| External dependencies | 0 (stdlib) |
| Coverage — core (`crewai`) | ~96.9% |
| Coverage — `mcp` / `tools` | ~92% / ~91% |
| Coverage — `llm/*` providers | all ≥ 90% |
| Runnable examples | 14 (+ pt-BR mirrors) |
| Documentation | bilingual README + guides + MCP + SECURITY |
| CI | GitHub Actions (`gofmt`, `vet`, `test -race`) + CodeQL |

### Known limitations (still open after v0.5.0)

- **No streaming** — `LLM.Call` returns the full response; there is no
  `CallStream` / `StreamingLLM` yet.
- **In-process memory only** — no persistence or semantic/embedding search.
- **JSON Schema is still a subset** — supports core keywords plus
  `additionalProperties`, bounds, `pattern`, `oneOf`/`anyOf`/`allOf`
  (v0.5.0). Still no `$ref`, `if`/`then`/`else`, `format`, unevaluated*, etc.
- **MCP servers are trusted** — tool descriptions/results enter the model
  context; use `FilterTools`, network allowlists, and least privilege (see
  MCP threat model docs).
- **No event-driven Flows / YAML / training** — P3 roadmap.
- **Async beyond staged** — parallel-within-stage exists; free-form
  `async_execution` across arbitrary tasks does not.

## 6. Roadmap — next steps (out of current scope)

Advanced features of the original CrewAI **not** yet ported, by suggested
priority (highest impact / lowest effort first):

- [x] **Agentic loop** (`AgenticLoop`) — Plan-Execute-Evaluate-Refine
  execution strategy, opt-in via `Agent.Loop`/`Task.Loop`. See
  `PLAN.agentic-loop.md` for the full design.

- [x] **Security & code-review residuals (post PR #27)** — **done in v0.5.0**
  (PR #28): MCP timeout + catalog guards + threat model, OutputFile jail,
  `RedactHandler`, Kickoff single-flight, schema keyword expansion,
  `WithAllowTools`, `delegate_to_coworker`. Design archive:
  [`PLAN.security-residuals.md`](PLAN.security-residuals.md)
  ([PT](PLAN.security-residuals.pt-BR.md)).

### Completed through v0.5.0 (do not re-open)

- [x] **Staged process** + optional stages.
- [x] **Native function calling** (`ToolModeNative` + `ToolCallingLLM`) with ReAct fallback — OpenAI, Anthropic, Ollama, xAI.
- [x] **Web search** — agent-driven (`WebSearcher`) + model-driven (`tools.WebSearchTool`) + SSRF hardening.
- [x] **MCP client** (`mcp/`) + SchemaProvider + session teardown.
- [x] **Progress** (`WithProgress`) + per-task warnings.
- [x] **Structured `emit_result`** (`WithToolCall`) + **`WithAllowTools`** gather→capture.
- [x] **Expanded JSON Schema** + `WithStrictSchema`.
- [x] **`RedactHandler`**, OutputFile jail, Kickoff single-flight.
- [x] **Real delegation tool** `delegate_to_coworker` + `EnableDelegationTool`.
- [x] Tags **v0.1.0 … v0.5.0**; bilingual docs; CI + CodeQL.

### P0 — Publishing and fundamentals

- [x] **`git init` + public repository** — version, CI, tags through **v0.5.0**.
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
  - [x] Set up CI (GitHub Actions: lint, `go vet`, `go test`, examples build)
    — **done (2026-08-04):** `.github/workflows/ci.yml` (gofmt, vet, build, `go test -race`).
    Test badges added to README EN/PT.
  - [x] Tag `v0.1.0` — **published (2026-08-04)** with a bilingual CHANGELOG.
  - [x] **Toolchain pin for stdlib CVEs** — `go.mod` declares
    `toolchain go1.24.9` (language `go 1.24`). CI matrix stays on `1.24.x`
    (latest patch). Local go1.24.4 still builds via `GOTOOLCHAIN=auto`
    downloading the pinned toolchain when needed. Re-run `govulncheck`
    after upgrading the local SDK.
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

### govulncheck — stdlib CVEs (historical Go 1.24.4 note)

Scans on Go 1.24.4 reported multiple stdlib issues (crypto/tls, crypto/x509,
etc.) on provider/MCP TLS paths. **Mitigation (v0.5.0 hygiene):**
`toolchain go1.24.9` in `go.mod`; CI uses `1.24.x`. No vulnerability in
project application code. Re-scan after local SDK upgrades.

### Manual code review — points of attention

- **`Crew.Kickoff` single-flight (v0.5.0)** — concurrent Kickoff on the same
  `*Crew` returns `ErrCrewRunning`. Sequential reuse is supported. Task field
  mutation during interpolate remains single-owner under that lock.
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
- [x] **Native function calling** per provider (OpenAI/Anthropic/Ollama/xAI)
  via `ToolModeNative` + `ToolCallingLLM` — ReAct remains the default/fallback.
- [ ] **Async/parallelism** of tasks (`async_execution` / `Task.Async` + DAG
  via `Task.Context`) with `sync.WaitGroup` / waves — beyond Staged
  parallel-within-stage. Design: [`PLAN.memory-async.md`](PLAN.memory-async.md)
  ([PT](PLAN.memory-async.pt-BR.md)).
- [x] **Real delegation** between agents — `NewDelegationTool` /
  `delegate_to_coworker` + `Crew.EnableDelegationTool` (v0.5.0). Hierarchical
  manager assignment remains separate.

### P2 — Persistence and observability

- [ ] **Long-term memory** with `MemoryStore`, JSONL FileStore (stdlib),
  budgeted injection policy, and optional embedding hook (no core deps;
  SQLite stays out of module). Design:
  [`PLAN.memory-async.md`](PLAN.memory-async.md)
  ([PT](PLAN.memory-async.pt-BR.md)).
- [ ] **Callbacks and telemetry** — task and ReAct-iteration start/end hooks,
  exportable (structured logs, metrics).
- [x] **Guardrails** — output validation with retry via the structured-output
  repair loop (`Task.Structured` + `StructuredOutput.RepairMax`). The repair
  loop re-prompts the model with validation errors and retries up to a bounded
  number of attempts. Does not transform output (only validates).
- [x] **Post-output guardrails** — code-enforced post-output validation hooks
  (`Crew.Guardrails` and `Task.Guardrail`) that block publication of outputs
  violating business invariants. New sentinel `ErrBlockedByGuardrail`.
  Complements structured-output schema validation (shape vs. meaning).
  Coverage 93.7% in the root package.
- [x] **Facts & provenance** — first-class `Fact` type populated only by
  `FactSource` tools, never by the LLM. Facts carry source org, source URL,
  collection time, and payload hash (SHA-256). `NewFactSourceTool` constructor,
  `AllFactsProvenanced` helper, `dedupFacts` by PayloadHash. Coverage 94.5%
  in the root package.
- [x] **Structured output extensions (partial — v0.5.0)** — validator gained
  `additionalProperties`, bounds, `pattern`, `oneOf`/`anyOf`/`allOf`,
  `WithStrictSchema`; `WithAllowTools` gather→capture; `WithToolCall`
  emit_result. Still open: `$ref`, `format`, full draft 2020-12.

### P3 — Advanced

- [ ] **Flows** (event-driven orchestration with state and routing).
- [ ] More tools (HTTP, files, RAG) — **web search already shipped** (v0.3+).
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
