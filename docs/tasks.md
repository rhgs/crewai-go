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
| `OutputFile`     | `string`        | If set, writes the output to this file. |
| `Structured`     | `*crewai.StructuredOutput` | If set, requires JSON output validated against a JSON Schema. |

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
```

After execution, the output is written to the file (in addition to being
available via `task.Output()`).

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
- When `Structured` is set, tools and the ReAct loop are bypassed; the
  executor goes straight to the structured-output path.

### Supported schema keywords

The built-in validator supports a subset of JSON Schema: `type`,
`properties`, `required`, `enum`, `items`. It is not a full JSON Schema
implementation and intentionally omits keywords such as
`additionalProperties`, `oneOf`/`anyOf`, `pattern`, `minimum`/`maximum`,
and `minItems`/`maxItems`.

### Errors

| Sentinel | Meaning |
|---|---|
| `ErrInvalidOutput` | The model returned JSON that does not validate against the schema. |
| `ErrRepairBudgetExceeded` | Repair attempts exhausted; the task fails. Wraps the last validation error. |

Both can be checked with `errors.Is`.
