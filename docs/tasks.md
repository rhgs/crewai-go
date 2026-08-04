# Tasks

> **Languages:** **English** (current) · [Português](pt-BR/tasks.md)

> **Languages:** [**English**](pt-BR/tasks.md) · [Português](pt-BR/tasks.md)


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