# Declarative crews (JSON-subset)

> **Languages:** **English** (current) · [Português](pt-BR/declarative.md)

v1 loads a **JSON-compatible subset** of YAML via `encoding/json` (D-Y1).
There is no YAML parser in the stdlib and the core stays zero-deps (`P-DEPS`).
Anchors, block scalars (`|`, `>`), and unquoted keys are **not** supported.
Call it “JSON-compatible YAML subset” in docs; ship samples that parse as JSON.

No interpolation of `${ENV}` or secrets from the file (D-Y8). Wire env in Go.

## Load + Build

```go
cfg, err := crewai.LoadCrewFile("crew.json")
// or crewai.LoadCrew(reader)

crew, err := cfg.Build(
    crewai.WithLLMMap(map[string]crewai.LLM{"echo": myLLM}),
    crewai.WithToolMap(map[string]crewai.Tool{"calc": tools.Calculator()}),
    crewai.WithGuardrailMap(map[string]crewai.Guardrail{"ok": myGuard}),
)
```

`llm` / `tools` / `guardrail` / `agent` in the file are **string references**
into those maps. Unknown names fail closed at `Build` (`ErrUnknownRef`, D-Y5).
The file never executes code.

Input is capped at `MaxCrewConfigBytes` (1 MiB, D-Y9). Schema errors use
`ValidationError` JSON-pointer paths (D-Y4 / D-Y10).

## Field subset (D-Y7)

| Object | Fields |
|---|---|
| agent | `name`, `role` (required), `goal`, `backstory`, `llm`, `tools[]`, `max_iterations`, `allow_delegation`, `tool_mode` |
| task | `name`, `description` (required), `expected_output`, `agent`, `tools[]`, `context[]`, `async`, `output_file`, `guardrail` |
| crew | `name`, `process` (`sequential`/`hierarchical`/`dag` alias of sequential; **not** `staged` in v1), `verbose`, `memory`, `manager_llm`, `manager_agent`, `output_dir`, `enable_delegation_tool`, `enable_memory_tools`, `async_max_workers`, `async_fail_fast`, `guardrails[]` |

Out of v1: `Loop`, `StructuredOutput`, `Stages`, `Embed`/`MemoryStore` wiring,
`WithProgress`/`WithStream`/`WithEvents` (set those on the built `*Crew`).

## Context refs (D-Y6)

`context` entries are **name-first**, then a 0-based index (JSON number or
numeric string). After Build, `planWaves` rejects cycles (`ErrTaskDependencyCycle`).

```json
{
  "agents": [{"name": "w", "role": "Writer", "llm": "echo"}],
  "tasks": [
    {"name": "draft", "description": "Draft", "agent": "w"},
    {"name": "edit", "description": "Edit", "agent": "w", "context": ["draft"]}
  ],
  "crew": {"process": "sequential"}
}
```

Offline demo: `examples/declarative`.
