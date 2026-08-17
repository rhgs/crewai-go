# Changelog

All notable changes to **crewai-go** are documented here. This project adheres
to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
