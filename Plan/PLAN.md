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
| short-term memory        | `crewai.Memory` / `Crew.Memory`    |
| long-term memory         | `MemoryStore`, `FileStore`, `MemoryPolicy`, `EmbeddingFunc` |

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
| **Async waves under Sequential/Hierarchical** | `Task.Async` + DAG via `Task.Context`; wave scheduler; declaration-order fold; `AsyncMaxWorkers` default 8. | First-class barrier semantics without leaving Sequential/Hierarchical or switching to Staged. |
| **Pluggable long-term memory (stdlib)** | `MemoryStore` + JSONL `FileStore` + optional cosine embeddings; D-M7 commit barrier. | Cross-run persistence and semantic recall without Chroma/cgo or extra module deps. |

## 4. Architecture (packages)

```
crewai (root)          Agent, Task, Crew, Process, Tool, Memory/MemoryStore/MemoryPolicy,
                       FileStore, EmbeddingFunc, Task.Async / wave scheduler, LLM,
                       ReAct/native executor, StructuredOutput, Guardrails, Facts,
                       Progress, RedactHandler, DelegationTool
├── llm/openai         OpenAI and compatible (Groq, Azure…) + ToolCallingLLM + WebSearcher
├── llm/anthropic      Claude + ToolCallingLLM + WebSearcher
├── llm/ollama         Ollama local/cloud — native /api/chat + tools + web search
├── llm/xai            Grok: API key + subscription OAuth (Device Flow RFC 8628 + PKCE)
├── llm/mock           Deterministic LLM for tests
├── mcp                MCP Streamable HTTP client, LoadConfig, ToolAdapter, FilterTools
├── tools              Calculator, time, word count, WebSearchTool + search providers
├── examples           basic, sequential, hierarchical, staged, async_tasks, tools,
                       native_tools, agentic_loop, facts, guardrails, logging,
                       delegation, mcp, memory_file, memory_embed, …
└── docs               getting-started, agents, tasks, crews, tools, llms, memory, en/mcp
```

### Execution flow

1. `Crew.Kickoff` enforces single-flight (`ErrCrewRunning` if re-entered),
   optionally attaches `delegate_to_coworker` when `EnableDelegationTool`,
   interpolates `{inputs}`, and injects progress + agent role into `ctx`.
2. Process: Sequential | Hierarchical (manager assigns agent) | Staged
   (stages in order; tasks within a stage concurrent). Under Sequential/
   Hierarchical, `Task.Async` tasks schedule as DAG waves (`Task.Context`
   edges); results fold by declaration order after each barrier. Memory
   AutoSave buffers per wave/stage and commits at the barrier (D-M7).
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
- [x] Documentation: bilingual README + guides + MCP + SECURITY + memory/async; 17 examples.
- [x] Clean `go build`, `go vet`, and `go test ./...`.

### Maturity snapshot (2026-08-21 — v0.6.0)

| Metric | Value |
|---------|-------|
| Latest release | **v0.6.0** (2026-08-21) — PR #31 |
| Go LOC (approx.) | ~25k+ |
| External dependencies | 0 (stdlib) |
| Coverage — core (`crewai`) | ~94%+ (memory/async epic) |
| Coverage — `mcp` / `tools` / `llm/*` | all ≥ 90% |
| Runnable examples | 17 (+ pt-BR mirrors): +async_tasks, memory_file, memory_embed |
| Documentation | bilingual README + guides + MCP + SECURITY + memory/async |
| CI | GitHub Actions (`gofmt`, `vet`, `test -race`) + CodeQL |

### Known limitations (post v0.6.0)

- **No streaming** — `LLM.Call` returns the full response; there is no
  `CallStream` / `StreamingLLM` yet (roadmap §6 P1).
- **JSON Schema is still a subset** — supports core keywords plus
  `additionalProperties`, bounds, `pattern`, `oneOf`/`anyOf`/`allOf`
  (v0.5.0). Still no `$ref`, `if`/`then`/`else`, `format`, unevaluated*, etc.
  (roadmap §6 P2).
- **MCP servers are trusted** — tool descriptions/results enter the model
  context; use `FilterTools`, network allowlists, and least privilege (see
  MCP threat model docs).
- **No event-driven Flows / YAML / training** — roadmap §6 P3.
- **No first-class callbacks/telemetry beyond `WithProgress`** — task and
  ReAct-iteration lifecycle hooks are still open (roadmap §6 P2).
- **Tool surface beyond web search** — no built-in HTTP/files/RAG tools yet
  (roadmap §6 P3). Web search shipped in v0.3+.

> **Closed in **v0.6.0** (PR #31):** long-term `MemoryStore` + FileStore
> JSONL + embeddings hook, and `Task.Async` DAG waves beyond Staged. See
> [`PLAN.memory-async.md`](PLAN.memory-async.md).

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
- [x] Tags **v0.1.0 … v0.6.0**; bilingual docs; CI + CodeQL.

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

- [x] **Native function calling** per provider (OpenAI/Anthropic/Ollama/xAI)
  via `ToolModeNative` + `ToolCallingLLM` — ReAct remains the default/fallback.
- [x] **Real delegation** between agents — `NewDelegationTool` /
  `delegate_to_coworker` + `Crew.EnableDelegationTool` (v0.5.0). Hierarchical
  manager assignment remains separate.
- [x] **Async/parallelism beyond Staged** — `Task.Async` + DAG via
  `Task.Context`, wave scheduler, `AsyncMaxWorkers` (default 8; 0=unlimited),
  `AsyncFailFast`. Staged golden unchanged. **Shipped in v0.6.0 (PR #31).**
  Design archive: [`PLAN.memory-async.md`](PLAN.memory-async.md)
  ([PT](PLAN.memory-async.pt-BR.md)). Optional follow-ups deferred there:
  **M5** agent memory tools, **A5** `Process=DAG` alias.
- [x] **Streaming** — optional `StreamingLLM` (`CallStream` → `<-chan StreamChunk`)
  via type assertion (does not break existing `LLM` implementers); `Crew.WithStream`
  / context sink; v1 streams final text / no-tools paths only with Call fallback.
  **Design plan:** [`PLAN.streaming.md`](PLAN.streaming.md)
  ([PT](PLAN.streaming.pt-BR.md)). Decisions D-S1–D-S14 closed in design
  review (not coded).

### P2 — Persistence and observability

- [x] **Long-term memory** — `MemoryStore`, JSONL `FileStore` (stdlib),
  `MemoryPolicy` + D-M7 commit barrier, optional `EmbeddingFunc` + cosine
  recall; `Memory bool` remains the v0.x alias. **Shipped in v0.6.0 (PR #31).**
  Design archive: [`PLAN.memory-async.md`](PLAN.memory-async.md)
  ([PT](PLAN.memory-async.pt-BR.md)). SQLite stays out of core.
- [x] **Guardrails** — structured-output repair loop (`Task.Structured` +
  `StructuredOutput.RepairMax`).
- [x] **Post-output guardrails** — `Crew.Guardrails` / `Task.Guardrail`,
  `ErrBlockedByGuardrail`.
- [x] **Facts & provenance** — `Fact` / `FactSource` only (never LLM-authored).
- [x] **Structured output extensions (partial — v0.5.0)** — validator:
  `additionalProperties`, bounds, `pattern`, `oneOf`/`anyOf`/`allOf`,
  `WithStrictSchema`; `WithAllowTools` gather→capture; `WithToolCall`
  emit_result.
- [ ] **Callbacks and telemetry** — beyond `WithProgress` / slog:
  task and ReAct-iteration start/end hooks, exportable structured events
  (logs, metrics, optional OpenTelemetry later). Must stay metadata-safe
  (no prompt bodies by default; same redaction posture as Progress).
  **Separate plan when scheduled.**
- [ ] **JSON Schema remainder** — `$ref` (and remote/`$id` policy),
  `if`/`then`/`else`, `format`, `unevaluatedProperties` / `unevaluatedItems`,
  fuller draft 2020-12. Keep zero deps; fail closed on unsupported keywords
  under `WithStrictSchema`. Completes the partial v0.5.0 work above.

### P3 — Advanced orchestration, tools, and packaging

- [ ] **Flows** — event-driven orchestration with explicit state and routing
  (CrewAI-style Flows), without replacing Sequential/Hierarchical/Staged/
  Async waves. **Separate plan when scheduled.**
- [ ] **More built-in tools** — HTTP client (SSRF-safe, allowlists), sandboxed
  file read/write (jail like `OutputDir`), lightweight RAG helpers **as app
  patterns or optional examples** (vector DBs stay out of core; embeddings
  already hook via `EmbeddingFunc`). Web search already shipped (v0.3+).
- [ ] **Declarative YAML** — `agents.yaml` / `tasks.yaml` (and optional crew
  composition) compiled into the existing Go types; validation errors at load
  time. No runtime YAML eval of untrusted paths without a jail.
- [ ] **Training** — prompt fine-tuning / few-shot distillation from successful
  executions (export traces → curated examples). Out of core model training;
  library-side capture + export only unless a future optional module says
  otherwise.

### Out-of-epic backlog (single view)

Items **not** part of `PLAN.memory-async` and **not** shipped as of v0.6.0.
Schedule each with its own design note before coding (same gates as residuals:
decisions first, ≥90% coverage, race-clean, EN+PT docs, no new core deps
unless explicitly accepted).

| Priority | Item | Notes |
|---|---|---|
| P1 | **Streaming** | Implemented (tag pending): [`PLAN.streaming.md`](PLAN.streaming.md) — `StreamingLLM` + `WithStream`; not started |

| P2 | **Callbacks / telemetry** | Lifecycle hooks beyond `WithProgress` |
| P2 | **JSON Schema `$ref` / `format` / …** | Finish subset → practical 2020-12 slice |
| P3 | **Flows** | Event-driven state + routing |
| P3 | **Tools: HTTP, files, RAG patterns** | SSRF/jail; RAG not a core vector DB |
| P3 | **YAML crew definitions** | agents.yaml / tasks.yaml → Go types |
| P3 | **Training / trace export** | Few-shot distillation from runs |
| Deferred (memory-async P3) | **M5** `recall_memory` / `remember` | Agent-driven memory tools |
| Deferred (memory-async P3) | **A5** `Process=DAG` alias | Naming sugar only |

## 7. Open decisions

Product/design choices still open for **future** epics (not memory-async):

- **Streaming API shape** — **locked in design plan:** optional
  `StreamingLLM` (embeds `LLM`) + `Crew.WithStream`; do **not** add
  `CallStream` to `LLM`. Full matrix D-S1–D-S14 (Task/Agent demux, dual byte
  cap, non-nil chan contract) in [`PLAN.streaming.md`](PLAN.streaming.md).
  Ack before Phase 1 code.
- **Function calling vs ReAct** — **lean keep both**: ReAct remains the
  universal tool layer; native `ToolCallingLLM` stays opt-in per provider
  (already shipped). Revisit only if ReAct maintenance cost dominates.
- **xAI OAuth defaults** — `client_id` and exact endpoints are not public;
  implemented per RFC 8628 with configurable endpoints. Fix defaults when xAI
  publishes official documentation.
- **Module path** — published as `github.com/rhgs/crewai-go` (resolved).

Deferred from memory-async (only if product asks): **M5** memory tools,
**A5** `Process=DAG` alias — see [`PLAN.memory-async.md`](PLAN.memory-async.md) §10.
