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
