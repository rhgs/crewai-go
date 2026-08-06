# Crews

> **Languages:** **English** (current) · [Português](pt-BR/crews.md)

A **Crew** groups agents and tasks and orchestrates them according to a
**Process**.

## Creating and running

```go
crew := crewai.NewCrew(
	[]*crewai.Agent{researcher, writer},
	[]*crewai.Task{research, article},
)
crew.Verbose = true

out, err := crew.Kickoff(context.Background(), nil)
```

## Fields

| Field          | Type           | Description |
|----------------|----------------|-------------|
| `Agents`       | `[]*Agent`     | Team members. |
| `Tasks`        | `[]*Task`      | Tasks to execute. |
| `Process`      | `Process`      | `Sequential` (default) or `Hierarchical`. |
| `Verbose`      | `bool`         | Enables detailed logs. |
| `Memory`       | `bool`         | Enables shared memory. |
| `ManagerLLM`   | `LLM`          | The manager's LLM (hierarchical process). |
| `ManagerAgent` | `*Agent`       | Explicit manager (takes precedence over `ManagerLLM`). |

## The result: `CrewOutput`

```go
type CrewOutput struct {
	Final       string        // output of the last task
	TasksOutput []TaskOutput  // output of each task
	Duration    time.Duration // total time
}
```

## Sequential process

Tasks run in the defined order. If a task has no `Agent`, the crew uses the
agent at the same position in the agents list.

```go
crew.Process = crewai.Sequential
```

## Hierarchical process

A **manager** decides which agent runs each task that has no fixed `Agent`.
Define the manager in one of these ways:

```go
// A) The crew creates an automatic manager from an LLM:
crew.Process = crewai.Hierarchical
crew.ManagerLLM = llm

// B) You provide a custom manager agent:
crew.ManagerAgent = crewai.NewAgent(
	"Project Director", "Coordinate the team", "...", llm,
)
```

For each task without an agent, the manager receives the list of members
(role + goal) and the task description, and responds with the chosen member's
role. If the task already has an `Agent`, it is respected.

> If there is only one agent, it is chosen automatically. If delegation fails
> (LLM error), the crew falls back to the first agent.

## Input interpolation

The second argument to `Kickoff` feeds the `{key}` interpolation of all tasks:

```go
crew.Kickoff(ctx, map[string]string{"client": "Acme"})
```

## Cancellation and timeouts

`Kickoff` respects the `context.Context`:

```go
ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
defer cancel()
out, err := crew.Kickoff(ctx, nil)
```

## Guardrails

Guardrails are code-enforced post-output validation hooks. They run after a
crew (or task) produces output and BLOCK publication if a business invariant
is violated. Unlike prompt-level instructions, guardrails are a hard code
guarantee: the output is never returned if a guardrail fails.

### Crew-level guardrails

Set `Crew.Guardrails` to run validation against the full `CrewOutput` after
all tasks complete:

```go
crew.Guardrails = []crewai.Guardrail{
    func(_ context.Context, out *crewai.CrewOutput) error {
        if len(out.Final) < 50 {
            return fmt.Errorf("output too short: %d chars", len(out.Final))
        }
        return nil
    },
}
```

### Task-level guardrails

Set `Task.Guardrail` to validate a single task's output as soon as it
completes (before the next task starts):

```go
task := crewai.NewTask("...", "...", agent).
    WithGuardrail(func(_ context.Context, out *crewai.CrewOutput) error {
        if !strings.Contains(out.Final, "http") {
            return fmt.Errorf("missing source URL")
        }
        return nil
    })
```

### Blocking semantics

- If any guardrail returns a non-nil error, `Kickoff` returns
  `crewai.ErrBlockedByGuardrail` wrapping the guardrail's error.
- The partially validated output is NOT returned.
- Task-level guardrails run at task completion; crew-level guardrails run
  after all tasks. First failure short-circuits.
- Guardrails MUST NOT mutate the output.

### Guardrails vs. structured output

| Feature | What it checks | When | Failure |
|---|---|---|---|
| Structured output | JSON shape (schema) | During LLM call | Repair loop, then `ErrRepairBudgetExceeded` |
| Guardrails | Business meaning (invariants) | After output | `ErrBlockedByGuardrail` (no retry) |

Schema validation checks the **shape**; guardrails check the **meaning**.
Use both for maximum safety.
