# Changelog

All notable changes to **crewai-go** are documented here. This project adheres
to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **Lifecycle events (P2)**: `CrewEvent` + `Crew.WithEvents` / `ContextWithEvents`,
  `KickoffID`, dual-emit with Progress, `llm_call_*`, `react_iteration`,
  `structured_repair`, `loop_phase`, `guardrail_blocked`, `wave_*`. Helpers
  `ProgressAsEvents`, `EventLogger`. Example `examples/events`. Design:
  `Plan/PLAN.p2-callbacks-schema.md` Part C (D-C1–D-C10).
- **JSON Schema remainder (P2)**: local `$ref` (`#/$defs`, `#/definitions`) with
  depth/expansion caps; `format` allowlist; `const`, `not`, `if`/`then`/`else`,
  `minProperties`/`maxProperties`, `uniqueItems`; boolean schemas. StrictSchema
  still rejects `unevaluated*`. Design plan Part J (D-J1–D-J12).

### Documentation

- **P2 design plan** was linked earlier; implementation docs: crews `WithEvents`,
  tasks schema keyword table, SECURITY events + local `$ref`, examples READMEs.


## [v0.7.0] — 2026-08-21

### Added

- **Streaming LLM tokens (P1)**: optional `StreamingLLM` (`CallStream` →
  `<-chan StreamChunk`), `Crew.WithStream` / `ContextWithStream`, public
  `CollectStream` / `CallOrStream`. Executor streams ReAct/native **no-tools**
  paths; Task/Agent demux on chunks (D-S13); body-byte cap via
  `MaxProviderResponseBytes`; mock + openai/ollama/anthropic/xai providers.
  Example `examples/streaming`. Design: `Plan/PLAN.streaming.md`
  (D-S1–D-S14). PRs #35, #36.

### Documentation

- **README What's new**: bumped to **v0.7.0** (EN + PT); Why/concepts/
  comparison include Streaming; example `streaming` listed.
- **Streaming guides**: `docs/llms.md` / crews (+ PT), SECURITY stream-sink
  note, PLAN maturity at v0.7.0; post-merge review hygiene (`drainToSink`
  cancel emit, HTTP timeout notes).
- **Docs polish carried from post-v0.6**: memory/async surface in README,
  getting-started Memory link, examples lists, SECURITY `v0.6.x` support
  (superseded by v0.7.x support row where applicable).

## [v0.6.0] — 2026-08-21

### Changed

- **Go toolchain pin**: `go.mod` now declares `toolchain go1.24.9` (language
  version remains `go 1.24`) so builds prefer a stdlib with known CVE fixes
  while CI stays on the `1.24.x` line.

### Added

- **`MemoryStore` long-term memory contract (M1)**: new `MemoryEntry`,
  `MemoryStore`, and `MemoryQuery` types plus entry/query caps
  (`MaxMemoryEntryBytes`, `MaxMemoryQueryLimit`, `DefaultMemoryQueryLimit`,
  `DefaultMemoryMaxChars`). The built-in `*Memory` now implements
  `MemoryStore`: `Put` maps to `Save` with an assigned ID and `CreatedAt`,
  `Query` searches `Content`/`Task` (case-insensitive; empty `Text` returns
  the latest entries first, bounded by `Limit`/`MaxChars`), `Delete` is
  idempotent, and `Close` is a no-op. Entries are partitioned by
  `MemoryScope`. Crew wiring arrived in M2; FileStore in M3. No change to
  the pre-existing `Memory bool` short-term path beyond the store bridge.

- **`examples/mcp`**: offline wiring demo for MCP `FilterTools` /
  `WithDescriptionLimit` / `NewToolAdapter` (optional live `MCP_ENDPOINT`).

### Added (async beyond Staged: A2/A3)

- **`Task.Async` + wave scheduler**: marking a task `WithAsync()` (or setting
  `Async = true`) lets independent tasks run concurrently under the
  `Sequential` and `Hierarchical` processes. Scheduling is wave-based:
  `Task.Context` defines the DAG, `planWaves` assigns each task to the
  earliest wave strictly after its dependencies, and aggregation folds each
  wave by declaration index after the barrier (same contract as Staged). A
  cycle, self-dependency, or duplicate task pointer fails fast at Kickoff
  with the new sentinel `ErrTaskDependencyCycle` (G12). Under Hierarchical,
  agents are pre-resolved serially before the waves run (D-A2). Under
  `Staged` the flag is ignored with a one-shot Warn log (D-A5/G5).

- **`Crew.AsyncMaxWorkers` / `Crew.AsyncFailFast`**: wave concurrency is
  capped by `AsyncMaxWorkers`; `NewCrew` sets `DefaultAsyncMaxWorkers` (**8**)
  and `AsyncFailFast = true`. Use `WithAsyncMaxWorkers(n)` to override, or
  **0 for unlimited** (explicit opt-out — bounding LLM fan-out is then your
  responsibility). With `AsyncFailFast = false`, independent branches
  continue after a failure and only the failed task's dependents are skipped
  with an error (D-A3); the default `true` cancels siblings and aborts
  Kickoff. Both are ignored under `Staged`.


- **`MemoryPolicy` + D-M7 commit barrier (M2)**: `Crew.MemoryStore` and
  `Crew.MemoryPolicy` wire long-term memory into Kickoff. `Memory=true`
  remains the permanent v0.x alias that ensures an InMemory store when no
  external store is set (D-M1/G4). AutoSave is success-only (G2); inject uses
  `queryCommitted` over the committed snapshot only (latest N, budgeted by
  `DefaultLimit`/`DefaultMaxChars`). During parallel waves/stages, AutoSave
  is buffered per task and committed at the barrier in **declaration order**
  (D-M7/G9) — next-wave inject cannot see in-flight sibling writes. Failed/
  cancelled buffers are discarded. AutoSave errors warn+capture and do not
  abort Kickoff (G11). Default `Scope` = `Crew.Name` when set (G3). Use
  `NewMemoryPolicy()` for library defaults (a zero `MemoryPolicy{}` is not
  those defaults).

- **A3 fixes**: `AsyncMaxWorkers` now actually caps in-flight workers in async
  waves (semaphore). Mixed ready waves run non-Async tasks after the Async
  subset (previously dropped). `AsyncFailFast=false` skips only dependents of
  failed tasks (D-A3). `0` remains unlimited by design; `NewCrew` still sets 8.

### Added (embeddings: M4)

- **`EmbeddingFunc` + cosine Query (M4)**: `Crew.Embed` accepts an
  app-provided embedder. With `MemoryPolicy.AutoEmbed = true`, each AutoSave
  entry is embedded **serially at the commit barrier** (G8) before Put —
  never inside parallel workers. Built-in stores (*Memory, FileStore) rank
  `MemoryQuery.Embedding` by stdlib cosine similarity; substring `Text` is
  ignored on the semantic path. Dim mismatch / zero-norm score 0; positive
  matches trim zero-score rows; if nothing is embedded, Query falls back to
  latest-N. AutoEmbed errors are soft (warn+capture, entry still saved).
  Example: `examples/memory_embed` (offline bag-of-words mock).

### Added (FileStore: M3)

- **`FileStore` JSONL backend**: `OpenFileStore(dir)` opens a durable
  stdlib-only `MemoryStore` under a caller-trusted root (`filestore.go`,
  D-M2=A). Layout `{root}/scopes/{urlsafeScope}/{entries.jsonl,meta.json}`;
  files `0600`, dirs `0700`. Put appends + RAM index; Delete writes a
  tombstone; Query matches the in-memory semantics (latest-N, Limit/MaxChars,
  Scope). Corrupt JSONL lines are skipped on Open and counted in meta /
  `CorruptSkipped()` (D-M5). v1 is single-writer per root (G7); the app owns
  `Close` (D-M6). Empty/blank root returns `ErrFileStoreRoot`; use after
  Close returns `ErrFileStoreClosed`. Example: `examples/memory_file`.

### Changed

- **Internal**: the staged parallel runtime was extracted into a shared
  `runTaskGroup` primitive (A1) that `runStaged` now calls per stage. The
  barrier (join) is the only point where results are folded, and results are
  always indexed by task position (declaration order), never completion
  order. No public or observable behavior change in the staged process —
  this is the foundation the async wave scheduler (A2/A3) and the
  memory commit barrier (M2, D-M7) build on.

### Documentation

- **User-facing docs for memory + async**: `docs/memory.md`, `docs/crews.md`,
  `docs/tasks.md` (+ PT mirrors) cover `MemoryStore` / `MemoryPolicy` / D-M7,
  `FileStore`, embeddings, and `Task.Async` waves. Package `doc.go` godoc
  updated. Offline examples `async_tasks`, `memory_file`, `memory_embed`.
- **Plan sync**: maturity snapshot and roadmap items marked shipped for
  v0.6.0 (PR #31).

## [v0.5.0] — 2026-08-20

### Security

- **Kickoff single-flight**: concurrent `Kickoff` on the same `*Crew`
  returns `ErrCrewRunning` (fail fast; does not queue). Sequential reuse
  remains supported.

- **OutputFile path jail**: `Task.OutputFile` paths are cleaned; empty
  paths are rejected. Optional `Task.OutputDir` / `Crew.OutputDir` jails
  writes with symlink evaluation (`EvalSymlinks`, fail closed) and
  sentinel `ErrOutputPathRejected`. Mode remains `0600`.

- **MCP default HTTP timeout**: `mcp.New` no longer uses
  `http.DefaultClient`. It builds a client with
  `DefaultHTTPTimeout` (30s). Configure via `WithHTTPTimeout`,
  `WithHTTPClient` (as-is, including `Timeout: 0`), or JSON
  `servers[].timeout` (Go duration string; omitted → 30s; `"0s"` →
  disabled). Invalid JSON durations fail `LoadConfig` before any
  network call.

- **Provider response bodies**: `Call` on OpenAI, Anthropic, and Ollama
  now caps bodies at `MaxProviderResponseBytes` (previously only
  `CallWithTools` / `WebSearch` did). Non-2xx errors no longer echo the
  response body (providers may restate credentials).
- **Native tool round-trip**: `Message.ToolCallID` is populated by the
  executor and mapped to OpenAI `tool_call_id` and Anthropic
  `tool_use_id`. Anthropic tool definitions are sent in the flat
  `{name, description, input_schema}` wire format (not the nested
  OpenAI shape), and assistant/tool turns are replayed as typed content
  blocks so multi-turn native tool loops work end-to-end.
- **ReAct observation cap**: tool outputs in the text ReAct loop are
  truncated with the same `MaxToolOutputBytes` limit as native tool
  calling.
- **SSRF hardening** (`tools.WebSearchTool`): also blocks userinfo,
  multicast, CGNAT (`100.64.0.0/10`), empty hosts, and metadata
  aliases (`metadata`, `metadata.google.internal`).
- **Search provider errors**: Google, Brave, LangSearch, and Serpstack
  no longer echo upstream error bodies / request URLs that could leak
  API keys into logs.
- **xAI OAuth paths**: `SaveToken` / `LoadToken` expand a leading
  `~/...` to the user home directory (previously the tilde was treated
  literally).

### Added

- **Inter-agent delegation tool**: `NewDelegationTool(roster)` exposes
  `delegate_to_coworker` (JSON `coworker`/`request`/`context`). Targets must
  set `AllowDelegation`. Nested calls honor `DefaultMaxDelegationDepth` (2)
  with cycle/self guards. `Crew.EnableDelegationTool` (default false)
  auto-attaches the tool at Kickoff; `Crew` implements `DelegationRoster`
  via `PeerAgents()`.

- **JSON Schema keywords (expanded)**: validator now supports
  `additionalProperties`, `minLength`/`maxLength` (bytes),
  `minimum`/`maximum`/`exclusiveMinimum`/`exclusiveMaximum`,
  `minItems`/`maxItems`, `pattern`, and `oneOf`/`anyOf`/`allOf`.
  `WithStrictSchema()` fails fast on unsupported keywords (`$ref`, etc.).
- **`WithAllowTools`**: optional gather phase before structured capture;
  preserves `FactSource` facts; gather budget exhaustion records a
  warning and still captures JSON (D6).

- **MCP catalog guards**: `mcp.FilterTools` (name allowlist, deny-by-default
  when filtering) and `mcp.WithDescriptionLimit` on `NewToolAdapter`
  (strip ASCII controls + truncate to N runes). Defaults unchanged without
  options.

- **`RedactHandler`**: opt-in `slog.Handler` wrapper in the root package
  that applies the existing secret redaction rules to log messages and
  string attributes. Default logger behavior is unchanged.
  `examples/logging` now uses `crewai.RedactHandler`.

- **MCP support** (`mcp/`): a stdlib-only client for the Model Context
  Protocol over Streamable HTTP (JSON-RPC 2.0, protocol version 2025-06-18).
  Two configuration modes, both supplied programmatically by the caller:
  - **Programmatic**: `mcp.New(endpoint, opts...)` + `client.Initialize(ctx, name, version)`.
  - **JSON file**: `mcp.LoadConfig(ctx, path, name, version)` reads a config
    with one or more server entries (Name, Endpoint, Headers, optional Timeout), creates a
    client for each, calls `Initialize`, and returns the slice.
  Each MCP tool is exposed as a `crewai.Tool` via `mcp.NewToolAdapter`.
  The adapter implements `crewai.SchemaProvider`, so the original
  `inputSchema` is forwarded to native function calling instead of being
  replaced with a placeholder. A tool-level `isError: true` is returned
  to the model as observation text (prefixed `[tool error]`), not as a
  Go error — only HTTP / JSON-RPC failures surface as `error`.
  New helpers: `WithHTTPClient`, `WithHTTPTimeout`, `WithHeader`, `Client.Close`
  (HTTP DELETE session teardown; idempotent). `LoadConfig` closes
  already-initialized clients if a later server fails `Initialize`.
  New constant `MaxMCPResponseBytes = 16 MiB`. No new dependencies;
  fully backward compatible.

- **`SchemaProvider` interface**: an optional, type-asserted capability a
  `Tool` may implement to expose its JSON Schema. The executor's
  `toToolSpecs` consults `SchemaProvider` before falling back to the
  default empty object schema — used by the MCP adapter and available to
  any tool that wants to carry its real schema forward to the model.
  Purely additive.

- **Per-task warnings (graceful degradation)**: new `Task.AddWarning(msg)`
  and `Task.Warnings()` thread-safe methods, the optional `WarningSink`
  interface retrievable from `context.Context` via `warningSinkFromCtx`,
  and the convenience helper `AddWarningFromCtx(ctx, msg)`. The executor
  (`Crew.execute`) injects the task as a `WarningSink` into `ctx` before
  invoking tools, so a tool can record a non-fatal diagnostic and the
  task still succeeds. Warnings aggregate into `TaskOutput.Warnings` and
  `CrewOutput.Warnings` in execution order. `Agent.Execute` standalone
  does not inject a sink — `AddWarningFromCtx` is a silent no-op there.
  Distinct from `Stage.Optional`: warnings mean *partial success within
  a task*; optional stages mean *task failure does not abort the crew*.
  No new dependencies; backward compatible.

- **Progress observability**: new `crewai.ProgressFunc` and
  `crewai.Progress` types plus the fluent setter `Crew.WithProgress(fn)`.
  During `Kickoff` the executor emits events for `stage_started`,
  `stage_completed`, `task_started`, `task_completed`, and `tool_invoked`
  (both ReAct and native paths). The callback is invoked from multiple
  goroutines when stages run in parallel — it MUST be safe for
  concurrent use, like an `slog.Handler`. Panics inside the callback
  are recovered and logged via `slog.Default()`; `Kickoff` is never
  aborted by a callback failure. `Progress` payloads never contain
  prompt bodies, LLM outputs, or tool inputs — only metadata. No new
  dependencies; backward compatible.

- **Structured output via tool-call (`WithToolCall`)**: a new mode on
  `StructuredOutput` that extracts JSON via a synthetic tool call
  instead of plain-text JSON prompts. Useful for providers that do not
  support structured outputs through the `format` parameter (notably
  Ollama Cloud): the executor declares a `ToolSpec` named
  `emit_result` with `Parameters = schema`, asks the model to call it
  exactly once, and parses the call's `arguments` (already
  `json.RawMessage` in every provider's `ToolCallingLLM`) as the
  validated output. If the model returns free text or the arguments
  fail validation, the existing `RepairMax` repair loop kicks in. If
  the LLM does not implement `ToolCallingLLM`, returns
  `ErrToolCallStructuredUnsupported`. The legacy JSON-only mode
  remains the default and is backward compatible. Opt in with
  `crewai.WithToolCall()`.

### Changed

- Local pre-commit hook (`scripts/pre-commit`) now runs `govulncheck
  ./...` against Go 1.25.x in addition to `gofmt`. The module itself
  has no known vulnerabilities. Install with
  `ln -sf ../../scripts/pre-commit .git/hooks/pre-commit`.

## [v0.4.0] — 2026-08-17

### Added

- **Staged process** (`process.go`, `crew.go`): a third orchestration mode
  (`Staged`) that groups tasks into stages. Stages run in sequence, while the
  tasks within a single stage run concurrently. The output of each stage feeds
  the following ones via `Task.Context`. A stage marked `Optional` does not
  abort the crew when one of its tasks fails. New `Stage` type and
  `ErrNoStages` sentinel.

- **Agentic loop** (`loop.go`): an optional Plan-Execute-Evaluate-Refine
  execution strategy (`AgenticLoop`) that replaces the single-pass ReAct
  executor. Set `Agent.Loop` or `Task.Loop` to enable it. Features:
  - Planning phase (skipped when the agent has no tools, or via `WithSkipPlan`).
  - Evaluation phase with a configurable pass threshold (`WithPassThreshold`)
    and an optional independent evaluator agent (`WithEvaluator`).
  - Refinement up to `MaxRefinements` rounds, with `WithRefineRewriteOnly` to
    rewrite without re-running tools.
  - New sentinel errors `ErrEvaluationFailed` and `ErrInvalidEvaluation`.

## [v0.3.0] — 2026-08-17

### Security

- **Secret redaction in logs** (`redact.go`): provider errors logged via
  `logger.WarnContext` (notably the hierarchical delegation path) are now
  passed through `redactError`, which masks likely-secrets in the message:
  - Long alphanumeric tokens (≥20 chars), preserving 4 leading and 4 trailing
    characters for identifiability when the token is ≥24 chars.
  - `Bearer <token>` in HTTP-style messages.
  - `api_key=`, `token=`, `key=`, `secret=` query-string values.

  See `redact.go` and `redact_test.go` for the exact rules. Example program
  showing the same redaction pattern at the handler level: `examples/logging/`.

- **Logging safety docs**: README + doc.go now warn about Debug-level logs
  containing full LLM output and tool inputs, and provider errors potentially
  including API keys. Recommends wrapping handlers with a redactor.

- **`WithLogger` is not concurrent-safe**: documented on `Crew.logger`,
  `Agent.logger`, `Crew.WithLogger`, and `Agent.WithLogger`. Multiple
  sequential calls are idempotent (last wins), tested by
  `TestWithLogger_Idempotent_Crew` and `TestWithLogger_Idempotent_Agent`.

### Changed

- **Logger replaced with `log/slog`**: The custom `Logger` interface
  (`Infof`/`Debugf` format-string style) backed by `log.Logger` has
  been removed in favor of `*slog.Logger` from the standard library.
  All executor functions (`executeTask`, `executeStructured`,
  `executeTaskWithTools`) now take `*slog.Logger` and emit structured
  key-value logs via `InfoContext`/`DebugContext`/`WarnContext`. New
  `Crew.WithLogger(*slog.Logger) *Crew` and `Agent.WithLogger(*slog.Logger) *Agent`
  fluent setters allow injecting any `*slog.Logger`. Backward compatible:
  `Crew.Verbose` continues to work — it now controls the level of the
  fallback logger (`Verbose=true` → `LevelDebug`, `Verbose=false` →
  `LevelError`, matching the legacy "silent when off" behavior).
  `Agent.Execute` standalone falls back to `slog.Default()`. Subpackages
  (`llm/*`, `tools/*`) remain logging-free. `logger.go` deleted. No new
  dependencies; only `log/slog` from stdlib.

### Added

- **Web search (agent-driven)**: `WebSearcher` interface and `SearchWeb`
  helper function for direct web search from Go code. `SearchHit` struct
  (Title, URL, Content). Implemented by Ollama (`/api/web_search`, pure
  search), OpenAI (`web_search_options`, requires search-capable model),
  Anthropic (`web_search_20250305` server tool), and xAI (delegates to
  OpenAI-compatible client). `ErrWebSearchUnsupported` sentinel. The `mock`
  LLM also implements `WebSearcher` for testing. No new dependencies;
  backward compatible.
- **Web search (model-driven)**: `WebSearchTool` in the `tools` package — a
  `Tool` + `FactSource` for the ReAct loop with pluggable `SearchProvider`.
  7 providers: Wikipedia (default, free), LangSearch (100% free), Serpstack
  (1000/month free), DuckDuckGo (optional, no key), Google Custom Search
  (API key + CX ID), Brave Search (API key). Results collected as `Fact`s
  with provenance. Options: `WithMaxResults`, `WithSearchTimeout`.
- **Web search support**: Ollama, OpenAI, Anthropic, xAI.
- **Native tool calling**: `Agent.ToolMode` (`"react"` | `"native"`) selects
  between the existing text-based ReAct loop and the provider's native
  function calling API. New `ToolCallingLLM` interface (implements `LLM`),
  `ToolSpec`, `ToolCall`, `ToolCallResponse`, `ToolTrace` types.
  `CallWithTools` implemented for Ollama, OpenAI, Anthropic, and Mock.
  `ToolTraces` in `TaskOutput` for observability. `ErrNativeToolsUnsupported`
  sentinel. Security: argument validation (size/depth limits), tool output
  truncation, provider response size limits (`io.LimitReader`). Backward
  compatible: default is ReAct, no changes to existing code paths.

### Security

- **SSRF protection** for web search: non-http(s) schemes, loopback, private,
  link-local, and unspecified IP addresses are blocked. Domain names are
  resolved via DNS and checked to prevent DNS rebinding attacks. Unresolvable
  hosts are blocked (fail-closed).

## [v0.2.0] — 2026-08-07

### Added

- **Facts & provenance**: first-class `Fact` type populated only by
  `FactSource` tools, never by the LLM. Facts carry source org, source URL,
  collection time, and payload hash (SHA-256). `NewFactSourceTool` constructor,
  `AllFactsProvenanced` helper for guardrails, `dedupFacts` by PayloadHash.
  `CrewOutput.Facts` and `TaskOutput.Facts`. `executeTask` returns
  `(string, []Fact, error)` internally. No new dependencies; backward
  compatible.
- **Guardrails**: crew-level (`Crew.Guardrails`) and task-level
  (`Task.Guardrail`) post-output validation hooks that block publication of
  outputs violating business invariants. New sentinel
  `ErrBlockedByGuardrail`. Functional options `WithGuardrails` (crew) and
  `WithGuardrail` (task). Complements structured-output schema validation
  (shape vs. meaning). No new dependencies; backward compatible.
- **Structured output**: `Task.Structured` (`*StructuredOutput`) requires the
  model to produce JSON validated against a JSON Schema, with a bounded repair
  loop (`RepairMax`, default 2). New sentinels `ErrInvalidOutput` and
  `ErrRepairBudgetExceeded`. Minimal in-house schema validator (type,
  properties, required, enum, items) — stdlib only, no new dependencies.
  The `LLM.Call` interface is unchanged; structured output works via prompt
  engineering and Go-side validation. Constructor `NewStructuredOutput` and
  functional option `WithRepairMax`.

## [v0.1.0] — 2026-08-04

First public release: an idiomatic Go port of the CrewAI framework core.

### Added

- **Core orchestration**: `Agent`, `Task`, `Crew`, `Process`, `Tool`, `Memory`,
  and a text-based **ReAct** executor.
- **Processes**: `Sequential` (default) and `Hierarchical` (manager-driven
  delegation via `ManagerLLM` / `ManagerAgent`).
- **Context & interpolation**: chain task outputs with `WithContext`; inject
  `{key}` variables through `Crew.Kickoff`.
- **LLM providers** (stdlib only, no external deps):
  - OpenAI and compatible endpoints (Groq, Azure, Ollama `/v1`, …) — `llm/openai`.
  - Anthropic (Claude) — `llm/anthropic`.
  - Ollama local + Ollama Cloud — `llm/ollama`.
  - xAI (Grok) via API key **or** subscription OAuth (Device Flow RFC 8628 +
    PKCE + refresh + persistence) — `llm/xai`.
  - Deterministic mock for tests — `llm/mock`.
- **Built-in tools**: `Calculator` (safe recursive-descent parser), `CurrentTime`,
  `WordCount` — `tools` package.
- **Memory**: concurrency-safe in-process memory with substring search.
- **Docs & examples**: English README + 7 guides; Portuguese mirrors
  (`docs/pt-BR/`); 7 runnable examples; hermetic tests (~90% core coverage).

### Security

- Secrets never committed; `.gitignore` protects `.claude/`, `.env`, `*token.json`.
- xAI OAuth token persisted with `0600` permissions.

### Notes

- License: MIT.
- Known limitations of this version: simplified hierarchical delegation (no
  runtime inter-agent calls), no streaming, in-process memory only, no native
  function calling. See `Plan/PLAN.md` for the full roadmap.

[Unreleased]: https://github.com/rhgs/crewai-go/compare/v0.7.0...HEAD
[v0.7.0]: https://github.com/rhgs/crewai-go/compare/v0.6.0...v0.7.0
[v0.6.0]: https://github.com/rhgs/crewai-go/compare/v0.5.0...v0.6.0
[v0.5.0]: https://github.com/rhgs/crewai-go/compare/v0.4.0...v0.5.0
[v0.4.0]: https://github.com/rhgs/crewai-go/compare/v0.3.0...v0.4.0
[v0.3.0]: https://github.com/rhgs/crewai-go/compare/v0.2.0...v0.3.0
[v0.2.0]: https://github.com/rhgs/crewai-go/compare/v0.1.0...v0.2.0
[v0.1.0]: https://github.com/rhgs/crewai-go/releases/tag/v0.1.0
