# Agents

> **Languages:** **English** (current) · [Português](pt-BR/agents.md)

An **Agent** is an autonomous worker. It combines a _persona_ (role, goal,
backstory), an **LLM** for reasoning, and optionally **tools** to act.

## Creating an agent

```go
agent := crewai.NewAgent(
	"Senior Researcher",              // Role
	"Uncover actionable insights",  // Goal
	"You have 20 years of experience...", // Backstory
	llm,                                // LLM
)
```

Or fill the struct directly for more control:

```go
agent := &crewai.Agent{
	Role:          "Senior Researcher",
	Goal:          "Uncover actionable insights",
	Backstory:     "You have 20 years of experience in data analysis.",
	LLM:           llm,
	MaxIterations: 10,   // limit of reasoning cycles per task
	Tools:         []crewai.Tool{ /* ... */ },
}
```

## Fields

| Field             | Type           | Description |
|-------------------|----------------|-------------|
| `Role`            | `string`       | The agent's role. **Required.** |
| `Goal`            | `string`       | Personal goal that guides decisions. |
| `Backstory`       | `string`       | Context and personality. |
| `LLM`             | `crewai.LLM`   | The language model. **Required.** |
| `Tools`           | `[]crewai.Tool`| Tools available for any task. |
| `MaxIterations`   | `int`          | Max reasoning/tool cycles per task (default 15). |
| `AllowDelegation` | `bool`         | Marks the agent as eligible to manage/delegate. |
| `ToolMode`         | `ToolMode`    | `"react"` (default) or `"native"` — selects text-based ReAct or native function calling. |
| `Loop`             | `crewai.Loop` | Optional execution strategy (e.g. `AgenticLoop`) replacing the default ReAct executor. |

## Logging

For standalone `Agent.Execute` (without a crew), inject a `*slog.Logger`
via `WithLogger`. If not set, `slog.Default()` is used.

```go
agent := crewai.NewAgent("a", "g", "b", llm).
    WithLogger(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
        Level: slog.LevelDebug,
    })))
```

When the agent runs inside a `Crew`, the crew's logger is used instead.
See [LLMs > Logging](llms.md#logging) for details.

## Adding tools

```go
agent.WithTools(tools.Calculator(), myTool)
```

`WithTools` is chainable and returns the agent itself:

```go
agent := crewai.NewAgent("Analyst", "...", "...", llm).
	WithTools(tools.Calculator())
```

## Running a standalone agent

Outside a crew (useful in tests or simple flows):

```go
output, err := agent.Execute(context.Background(), task)
```

## How the agent thinks

- **Without tools:** the agent makes a single LLM call and returns the answer.
- **With tools:** the agent enters a ReAct loop — thinks, picks a tool, observes
  the result, and repeats until it reaches a `Final Answer` or hits
  `MaxIterations`. See [tools.md](tools.md).

## Agentic loop

By default, an agent uses the single-pass ReAct executor. For tasks that
benefit from self-assessment and iterative refinement, set `Agent.Loop` (or
`Task.Loop`) to an `AgenticLoop`, which follows a
**Plan-Execute-Evaluate-Refine** cycle:

1. **Plan** — the agent produces a numbered plan (skipped when the agent has
   no tools, or when `WithSkipPlan` is set).
2. **Execute** — the agent runs the ReAct/structured executor with the plan as
   context.
3. **Evaluate** — an evaluator (the same agent, or a separate one via
   `WithEvaluator`) scores the output (0-100) against the expected output.
4. **Refine** — if the score is below the pass threshold, the feedback is
   injected and the agent re-executes, up to `MaxRefinements` times.

```go
agent.Loop = crewai.NewAgenticLoop(
    crewai.WithMaxRefinements(3),
    crewai.WithPassThreshold(80),
    crewai.WithEvaluator(evaluatorAgent), // optional independent evaluator
)
```

If the output never passes evaluation, `Kickoff` returns
`crewai.ErrEvaluationFailed`. If the evaluator returns an unparseable response,
it returns `crewai.ErrInvalidEvaluation`.

See [examples/agentic_loop](../examples/agentic_loop) for a runnable example.

## Best practices

- Give a **specific role** ("API Technical Writer" rather than "Writer").
- The **goal** should be measurable and outcome-oriented.
- Use the **backstory** to calibrate tone and level of detail.
- Tune `MaxIterations` for tasks that use many tools.
