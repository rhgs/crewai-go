# Changelog

All notable changes to **crewai-go** are documented here. This project adheres
to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
  function calling. See `PLAN.md` for the full roadmap.
