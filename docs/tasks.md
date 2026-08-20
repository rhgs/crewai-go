# Tasks

> **Languages:** **English** (current) · [Português](pt-BR/tasks.md)

A **Task** is a unit of work: what to do, what output to expect, and who is
responsible.

## Creating a task

```go
task := crewai.NewTask(
	"Write an executive summary of the quarterly report.", // Description
	"A summary of up to 200 words in bullet points.",       // ExpectedOutput
	writer,                                                  // Agent
)
```

## Fields

| Field            | Type            | Description |
|------------------|-----------------|-------------|
| `Name`           | `string`        | Short identifier (shows in logs/memory). |
| `Description`    | `string`        | Detailed instruction. Supports `{variables}`. |
| `ExpectedOutput` | `string`        | Expected format/quality. |
| `Agent`          | `*crewai.Agent` | The assignee. Can be `nil` (the crew resolves it). |
| `Tools`          | `[]crewai.Tool` | Overrides the agent's tools for this task. |
| `Context`        | `[]*crewai.Task`| Tasks whose outputs become this task's context. |
| `OutputFile`     | `string`        | If set, writes the output to this file (mode `0600`). Path is cleaned; empty paths are rejected. |
| `OutputDir`      | `string`        | Optional jail directory for `OutputFile`. Symlink-aware (`EvalSymlinks`, fail closed). Prefer when paths come from external config. |
| `Structured`     | `*crewai.StructuredOutput` | If set, requires JSON output validated against a JSON Schema. |
| `Guardrail`      | `crewai.Guardrail` | Optional task-level post-output validation. |
| `Loop`           | `crewai.Loop`   | Optional per-task execution strategy (overrides `Agent.Loop`). |

## Context between tasks

Use `WithContext` to pass previous tasks' outputs:

```go
collect  := crewai.NewTask("Collect the sales data.", "table", analyst)
analyze  := crewai.NewTask("Analyze the trends.", "insights", analyst).
	WithContext(collect) // receives 'collect' output in the prompt
```

In the **sequential process**, the previous task's output is also chained
automatically when you use `WithContext`. Without `WithContext`, each task
receives only accumulated memory (if `crew.Memory = true`).

## Variable interpolation

Use `{key}` in the description and expected output; pass the values in `Kickoff`:

```go
task := crewai.NewTask("Research {topic} in the {sector} sector.", "...", agent)

crew.Kickoff(ctx, map[string]string{
	"topic":  "generative AI",
	"sector": "healthcare",
})
```

## Saving the output to a file

```go
task.OutputFile = "report.md"
// Optional jail (recommended when the path comes from config/env):
task.OutputDir = "/var/lib/myapp/outputs"
// or: task.WithOutputDir("/var/lib/myapp/outputs")
// Crew-level default for tasks without their own OutputDir:
// crew.OutputDir = "/var/lib/myapp/outputs"
```

After execution, the output is written to the file (mode `0600`) in addition to
being available via `task.Output()`. Paths are cleaned. When `OutputDir` (task
or `Crew.OutputDir`) is set, the file must resolve inside that directory;
symlinks are evaluated and escapes fail with `ErrOutputPathRejected`. Never
pass unvalidated model output as `OutputFile`.

## Retrieving the output

```go
crew.Kickoff(ctx, nil)
fmt.Println(task.Output()) // output of this specific task
```

Or from the consolidated result:

```go
out, _ := crew.Kickoff(ctx, nil)
for _, to := range out.TasksOutput {
	fmt.Printf("%s (%s): %s\n", to.Task, to.Agent, to.Output)
}
```

## Structured output

When a task needs typed, trustworthy data (e.g. for persisting into a
database), set the `Structured` field with a JSON Schema. The executor
instructs the model to reply with JSON only, validates the output in Go,
and retries up to `RepairMax` times if validation fails.

```go
schema := map[string]any{
    "type": "object",
    "properties": map[string]any{
        "name":  map[string]any{"type": "string"},
        "count": map[string]any{"type": "integer"},
    },
    "required": []string{"name", "count"},
}

structured, _ := crewai.NewStructuredOutput(schema, crewai.WithRepairMax(3))

task := crewai.NewTask("Extract the product name and count.", "JSON", agent)
task.Structured = structured
```

### Behavior

- The model is told to output **only** JSON — no markdown, no prose.
- The output is validated against the schema in Go (stdlib `encoding/json`).
- If invalid, the executor re-prompts the model with the validation
  errors and asks it to fix the JSON. This repeats up to `RepairMax`
  times (default 2).
- If the budget is exhausted, the task fails with
  `crewai.ErrRepairBudgetExceeded`. **The executor never returns invalid
  JSON or invents data.**
- On success, `Task.Output()` returns the **canonicalized** (compacted,
  stable) JSON string.
- When `Structured` is set **without** `AllowTools`, tools and the ReAct
  loop are bypassed; the executor goes straight to the structured-output
  path.
- With `AllowTools` / `WithAllowTools()`, the executor first runs a bounded
  **gather** phase (ReAct or native tools per `Agent.ToolMode`), then a
  **capture** phase that validates JSON against the schema. Facts from
  `FactSource` tools are preserved. If gather hits `MaxIterations` without
  a clean stop, the task records a warning (`gather budget exhausted`) and
  still proceeds to capture.

### Supported schema keywords

The built-in validator is a **stdlib-only subset** of JSON Schema:

| Keyword | Notes |
|---|---|
| `type`, `properties`, `required`, `enum`, `items` | Core (unchanged) |
| `additionalProperties` | `false` rejects unknown keys; object schema validates extras |
| `minLength`, `maxLength` | String length in **bytes** (`len(s)`), not runes |
| `minimum`, `maximum` | Numbers as `float64` |
| `exclusiveMinimum`, `exclusiveMaximum` | Bool (draft-04) or numeric (draft-06+) |
| `minItems`, `maxItems` | Arrays |
| `pattern` | Go `regexp`; pattern length capped at `MaxSchemaPatternLen` (512) |
| `oneOf`, `anyOf`, `allOf` | Composition |

**Not supported:** `$ref`, `if`/`then`/`else`, `format`, `unevaluated*`, and most draft 2020-12 keywords. Use `WithStrictSchema()` / `StrictSchema` on `NewStructuredOutput` to fail fast when an author-supplied schema contains unsupported keywords.

### Errors

| Sentinel | Meaning |
|---|---|
| `ErrInvalidOutput` | The model returned JSON that does not validate against the schema. |
| `ErrRepairBudgetExceeded` | Repair attempts exhausted; the task fails. Wraps the last validation error. |
| `ErrToolCallStructuredUnsupported` | `ToolCall` is true but the agent's LLM does not implement `ToolCallingLLM`. |

All three can be checked with `errors.Is`.

### Tool-call mode (`WithToolCall`)

For providers that do not honour the JSON-Schema `format` parameter
(notably **Ollama Cloud**), the more reliable path is to declare a
synthetic tool whose parameters are the schema itself and force the
model to call it. The arguments the model passes come back pre-parsed
by every `ToolCallingLLM` implementation in `llm/openai.go`, `llm/ollama.go`,
etc. — they arrive as `json.RawMessage`, not as a string.

```go
structured, _ := crewai.NewStructuredOutput(
    schema,
    crewai.WithToolCall(),
    crewai.WithRepairMax(3),
)
task.Structured = structured
```

The executor builds `ToolSpec{Name: "emit_result", Parameters: schema}`,
asks the model to call it exactly once, and uses the call's
`arguments` field as the validated output. If the model returns free
text or the arguments fail validation, the existing repair loop kicks
in (up to `RepairMax` attempts). On exhaustion, the task fails with
`ErrRepairBudgetExceeded`.

Tool-call mode requires the agent's LLM to implement
`ToolCallingLLM`; otherwise the task fails with
`ErrToolCallStructuredUnsupported`. The default JSON-only mode is
preserved for backward compatibility — opt in with `WithToolCall()`.

### Tools before structured output (`WithAllowTools`)

When the agent must call tools (search, connectors) before emitting JSON:

```go
structured, _ := crewai.NewStructuredOutput(
    schema,
    crewai.WithAllowTools(),
    crewai.WithRepairMax(3),
)
task.Structured = structured
```

Pipeline:

1. **Gather** — tool loop (ReAct or native) until Final Answer / no tool
   calls, or `MaxIterations` exhausted.
2. **Capture** — existing JSON or `emit_result` path with the gather
   transcript in context.

Default remains `AllowTools == false` (backward compatible).

## Graceful degradation: per-task warnings

A task that **succeeds** can still record non-fatal diagnostics when a
secondary source was unavailable or a non-critical step failed. The task
itself succeeds; the warning is preserved in `TaskOutput.Warnings` and
`CrewOutput.Warnings` so the caller can render a degraded section (e.g.
"section unavailable") rather than aborting the whole investigation.

This is distinct from `Stage.Optional` — optional stages swallow
**failures**; warnings represent **partial successes**.

Recording a warning from Go:

```go
task.AddWarning("CNPJ lookup service returned 503")
crewai.AddWarningFromCtx(ctx, "secondary source timeout")
```

`Task.AddWarning` is thread-safe (protected by `sync.RWMutex`). A tool
can record warnings during execution via the context:

```go
func (t *myTool) Call(ctx context.Context, _ string) (string, error) {
    primary, err := t.primary(ctx)
    if err != nil { return "", err }
    secondary, err := t.secondary(ctx)
    if err != nil {
        // Secondary source failed — task can still succeed.
        crewai.AddWarningFromCtx(ctx, "secondary unavailable: " + err.Error())
    }
    return primary + "\n" + secondary, nil
}
```

The executor (`Crew.execute`) injects the task as a `WarningSink` into
`ctx` before invoking tools. `Agent.Execute` (standalone, without a
crew) does not inject a sink — calls to `AddWarningFromCtx` in that
mode are silent no-ops.

### Surfacing warnings

After `Kickoff`:

```go
out, _ := crew.Kickoff(ctx, nil)
for _, to := range out.TasksOutput {
    for _, w := range to.Warnings {
        fmt.Printf("WARNING from %s: %s\n", to.Task, w)
    }
}
// Aggregated view (in execution order):
for _, w := range out.Warnings {
    fmt.Println("AGG:", w)
}
```
