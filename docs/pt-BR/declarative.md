# Crews declarativos (subconjunto JSON)

> **Languages:** [English](../declarative.md) · **Português** (atual)

A v1 carrega um **subconjunto compatível com JSON** do YAML via
`encoding/json` (D-Y1). A stdlib não tem parser YAML e o core permanece
zero-deps (`P-DEPS`). Âncoras, blocos (`|`, `>`) e chaves sem aspas **não**
são suportados. Documente como “subconjunto YAML compatível com JSON”; os
samples oficiais parseiam como JSON.

Sem interpolação de `${ENV}` ou segredos no arquivo (D-Y8). Env entra no Go.

## Load + Build

```go
cfg, err := crewai.LoadCrewFile("crew.json")
// ou crewai.LoadCrew(reader)

crew, err := cfg.Build(
    crewai.WithLLMMap(map[string]crewai.LLM{"echo": myLLM}),
    crewai.WithToolMap(map[string]crewai.Tool{"calc": tools.Calculator()}),
    crewai.WithGuardrailMap(map[string]crewai.Guardrail{"ok": myGuard}),
)
```

`llm` / `tools` / `guardrail` / `agent` no arquivo são **referências string**
nesses maps. Nome desconhecido falha fechado no `Build` (`ErrUnknownRef`,
D-Y5). O arquivo nunca executa código.

Input limitado a `MaxCrewConfigBytes` (1 MiB, D-Y9). Erros de schema usam
`ValidationError` com JSON pointer (D-Y4 / D-Y10).

## Subconjunto de campos (D-Y7)

| Objeto | Campos |
|---|---|
| agent | `name`, `role` (obrigatório), `goal`, `backstory`, `llm`, `tools[]`, `max_iterations`, `allow_delegation`, `tool_mode` |
| task | `name`, `description` (obrigatório), `expected_output`, `agent`, `tools[]`, `context[]`, `async`, `output_file`, `guardrail` |
| crew | `name`, `process` (`sequential`/`hierarchical`; **não** `staged` na v1), `verbose`, `memory`, `manager_llm`, `manager_agent`, `output_dir`, `enable_delegation_tool`, `async_max_workers`, `async_fail_fast`, `guardrails[]` |

Fora da v1: `Loop`, `StructuredOutput`, `Stages`, wiring de `Embed`/`MemoryStore`,
`WithProgress`/`WithStream`/`WithEvents` (setar no `*Crew` já construído).

## Context refs (D-Y6)

Entradas de `context` são **name-first**, depois índice 0-based (número JSON
ou string numérica). Depois do Build, `planWaves` rejeita ciclos
(`ErrTaskDependencyCycle`).

Demo offline: `examples/declarative`.
