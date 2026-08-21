# Crews

> **Languages:** **English** (current) · [Português](pt-BR/crews.md)

A **Crew** groups agents and tasks and orchestrates them according to a
**Process**.

## Kickoff concurrency

Only **one** `Kickoff` may run at a time on a given `*Crew`. A concurrent call returns `ErrCrewRunning` immediately (fail fast — it does not queue). Create separate `Crew` values for parallel runs. Sequential reuse of the same Crew is supported.

For **semantic races** (completion order vs fold order), Memory D-M7, and
how Staged/Async groups stay deterministic, see
**[Concurrency model](concurrency.md)** — including the discussion from the
[dev.to thread](https://dev.to/rhgs/from-python-to-go-rewriting-a-crewai-workflow-in-pure-stdlib-47nm#comments).

## Creating and running

```go
crew := crewai.NewCrew(
	[]*crewai.Agent{researcher, writer},
	[]*crewai.Task{research, article},
)
crew.Verbose = true

out, err := crew.Kickoff(context.Background(), nil)
```

For structured logging, inject a `*slog.Logger` instead of using `Verbose`:

```go
import "log/slog"

log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
    Level: slog.LevelDebug,
}))
crew := crewai.NewCrew(agents, tasks).WithLogger(log)
```

See [LLMs > Logging](llms.md#logging) for the full reference.

## Fields

| Field          | Type           | Description |
|----------------|----------------|-------------|
| `Agents`       | `[]*Agent`     | Team members. |
| `Tasks`        | `[]*Task`      | Tasks to execute. |
| `Process`      | `Process`      | `Sequential` (default), `Hierarchical`, or `Staged`. |
| `Stages`       | `[]Stage`      | Stages for the `Staged` process (takes precedence over `Tasks`). |
| `Verbose`      | `bool`         | Enables detailed logs (maps to `LevelDebug` when no logger is injected via `WithLogger`). |
| `logger`       | `*slog.Logger` | Internal — set via `WithLogger`. When nil, `Kickoff` creates a default text logger on stderr. |
| `Memory`       | `bool`         | Ensures InMemory store when `MemoryStore` is nil (permanent v0.x alias). |
| `MemoryStore`  | `MemoryStore`  | Optional long-term backend; app owns `Close`. |
| `MemoryPolicy` | `*MemoryPolicy`| AutoSave/inject policy; nil ⇒ `NewMemoryPolicy()` defaults. |
| `Embed`        | `EmbeddingFunc`| Optional app embedder; used when `AutoEmbed` is true (serial at barrier). |
| `Name`         | `string`       | Optional crew id; default `MemoryPolicy.Scope` when set. |
| `AsyncMaxWorkers` | `int`       | Cap on concurrent Async tasks per wave (default 8 via `NewCrew`; **0 = unlimited**). Prefer `NewCrew` — a bare `Crew{}` literal leaves 0 (unlimited) by design. |
| `AsyncFailFast` | `bool`        | Cancel wave on first failure (default true). |
| `ManagerLLM`   | `LLM`          | The manager's LLM (hierarchical process). |
| `ManagerAgent` | `*Agent`       | Explicit manager (takes precedence over `ManagerLLM`). |
| `Guardrails`   | `[]Guardrail`  | Crew-level post-output validation hooks. |
| `OutputDir` | `string` | Optional jail for `Task.OutputFile` when the task has none. Symlink-aware; see tasks guide. |
| `EnableDelegationTool` | `bool` | When true, attaches `delegate_to_coworker` to each agent at Kickoff (default false). Targets still need `AllowDelegation`. |
| `stream`       | `StreamFunc`   | Internal — set via `WithStream` (LLM text deltas). |
| `events`       | `EventFunc`    | Internal — set via `WithEvents` (metadata lifecycle). |
| `progress`     | `ProgressFunc` | Internal — set via `WithProgress`. |

## The result: `CrewOutput`

```go
type CrewOutput struct {
	Final       string        // output of the last task
	TasksOutput []TaskOutput  // output of each task (incl. Facts, ToolTraces, Warnings)
	Duration    time.Duration // total time
	Facts       []Fact        // facts collected from FactSource tools (deduped)
	Warnings    []string      // non-fatal diagnostics from successful tasks
}
```

`TaskOutput` also carries per-task `Facts`, `ToolTraces` (native tool
calling only), and `Warnings`.

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

## Async tasks under Sequential / Hierarchical (A3)

Mark a task `Async` to let independent tasks overlap without switching to
`Staged`. Dependencies still flow through `Task.Context`, and aggregation is
always by **declaration index after a wave barrier** — never by completion
order (the same contract the staged process follows).

```go
research := crewai.NewTask("research", "notes", agent).WithAsync()
outline  := crewai.NewTask("outline", "points", agent).WithAsync()
write    := crewai.NewTask("write article", "markdown", agent).
    WithContext(research, outline) // runs after both

crew := crewai.NewCrew([]*crewai.Agent{agent}, []*crewai.Task{research, outline, write})
out, _ := crew.Kickoff(ctx, nil)
```

- `NewCrew` sets `AsyncMaxWorkers = DefaultAsyncMaxWorkers` (**8**) and
  `AsyncFailFast = true`. Override with `crew.WithAsyncMaxWorkers(n)`; **0 =
  unlimited** (explicit opt-out — LLM fan-out is your responsibility).
- `AsyncFailFast: false` keeps running independent branches after a failure
  and skips the failed task's dependents with an error (D-A3).
- `Task.Context` edges define the DAG; a cycle, self-dependency, or duplicate
  task pointer fails fast at Kickoff with `ErrTaskDependencyCycle` (G12).
- Under `Staged` the flag is ignored with a one-shot Warn log (D-A5/G5) —
  stages own the batch parallelism either way.

## Staged process

The `Staged` process groups tasks into **stages**: stages run in sequence, but
the tasks within a single stage run concurrently. Peers in a stage are
**independent mid-flight** (no same-stage merge via each other's live
outputs). After the stage barrier, results are folded in **declaration
order** (not completion order) into `TasksOutput` / Facts / Warnings. The
next stage starts only after that fold. Wire cross-stage dependencies with
`Task.Context` (earlier stages only). See [concurrency.md](concurrency.md).

```go
crew := crewai.NewCrew(agents, nil)
crew.Process = crewai.Staged
crew.Stages = []crewai.Stage{
    {Name: "collect", Tasks: []*crewai.Task{researchA, researchB}},
    {Name: "synthesize", Tasks: []*crewai.Task{write}},
}
```

Each `Stage` has:

| Field      | Type      | Description |
|------------|-----------|-------------|
| `Name`     | `string`  | Short identifier used in logs. |
| `Tasks`    | `[]*Task` | Tasks that run concurrently within this stage. |
| `Optional` | `bool`    | If `true`, a failure does not abort `Kickoff` (logged as a warning). |

- A stage's output feeds the following ones: a task in stage N may list, in
  `Context`, tasks from earlier stages (not from the same stage, which run in
  parallel).
- `CrewOutput.Final` is the output of the last task of the last stage.
- If `Process == Staged` and `Stages` is empty, `Kickoff` returns
  `ErrNoStages`.

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

## Streaming (LLM deltas)

Register a `StreamFunc` with `WithStream` to receive final-answer text deltas
on no-tools paths. Concurrent tasks may invoke the callback in parallel —
it MUST be concurrency-safe. Chunks carry `Task` and `Agent` for demux.
Panics are recovered. See [llms.md](llms.md#streaming).

## Lifecycle events (`WithEvents`)

Register an `EventFunc` with `WithEvents` for exportable metadata-only
lifecycle records (`CrewEvent`): kickoff, task/stage/wave, `llm_call_*`,
`react_iteration`, `structured_repair`, `loop_phase`, `guardrail_blocked`.

```go
crew.WithEvents(func(ev crewai.CrewEvent) {
    // JSON-serializable; concurrent-safe; panics recovered
    fmt.Println(ev.Type, ev.Task, ev.KickoffID)
})
```

- Works alongside `WithProgress` (dual-emit for Progress-shaped events).
- **No prompt bodies** by default (same posture as Progress). Stream text
  stays on `WithStream`.
- See `examples/events`.

## Progress observability

`Kickoff` is a black box until it returns. To surface real-time events
to a frontend (e.g. via SSE), set a progress callback with
`WithProgress`:

```go
crew := crewai.NewCrew(agents, tasks).
    WithProgress(func(p crewai.Progress) {
        // p carries: Stage, Task, Agent, Event, Tool, Duration, Err
        // Event is one of: "stage_started", "stage_completed",
        //                  "task_started", "task_completed",
        //                  "tool_invoked"
        w.Header().Set("Content-Type", "text/event-stream")
        fmt.Fprintf(w, "data: %s %s %s\n\n", p.Event, p.Task, p.Tool)
    })
```

The callback is invoked from multiple goroutines when stages run in
parallel — it MUST be safe for concurrent use, like an `slog.Handler`.
A panic inside the callback is recovered and logged via
`slog.Default()`; the `Kickoff` runs to completion either way.

Progress events never contain prompt bodies, LLM outputs, or tool
inputs — only metadata. Task-failure errors on `task_completed` are
passed through `redactError` before the callback sees them (long
tokens and Bearer credentials are masked). If you need to surface
tool inputs or outputs yourself, log them explicitly with the
sensitive parts masked.
