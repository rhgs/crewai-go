# Concurrency model

> **Languages:** **English** (current) · [Português](pt-BR/concurrency.md)

This page is the library’s contract for **data races** vs **semantic races**
when tasks run in parallel (Staged stages and Sequential/Hierarchical
`Task.Async` waves). It matches the implementation as of **v0.6+** (async
waves + D-M7 memory barrier) and **v0.7+** (streaming demux).

## Two different problems

| Concern | What `-race` catches | What orchestration must guarantee |
|---|---|---|
| **Memory-level data race** | Concurrent read/write of the same Go variable without sync | Mutexes, channels, single-flight Kickoff, race-clean tests |
| **Semantic race** | *Nothing* — the program can be race-detector clean and still nondeterministic | Whether **completion order** can change prompts, `CrewOutput`, or memory that later tasks read |

`go test -race` is **necessary but not sufficient**. crewai-go treats merge
order as part of the **orchestration contract**, not as an accident of
scheduling.

## Independence inside a parallel group

**Intra-stage / intra-wave tasks are independent while the group runs.**

- They do **not** read each other’s outputs, facts, or warnings mid-flight.
- Same-group `Task.Context` edges are **not** a supported merge channel for
  siblings. Under Async waves, a dependency must finish in a **strictly
  earlier** wave or Kickoff fails with `ErrTaskDependencyCycle` (G12).
- Under Staged, the intended graph is: **stage N may depend on stages &lt; N**,
  not on peers in the same stage (peers run concurrently).

If two specialists must feed a synthesizer, put the synthesizer in a **later
stage** or a **later wave** and wire it with `WithContext(...)`.

## Barrier, then deterministic fold

Every parallel group is a **barrier** (`WaitGroup` / wave join), then a
**declaration-order fold**:

1. Each task writes into a **slot keyed by declaration index** (not by who
   finished first).
2. After the barrier, the library aggregates in **slice order**:
   - `CrewOutput.TasksOutput`
   - per-task `Facts` / `Warnings` / `ToolTraces`
   - `CrewOutput.Final` (last successful task in fold order for that path)
3. Tests such as `TestStagedDeterministicOrder` cover slow-first / fast-second
   scheduling so completion order cannot reshuffle the fold.

**Implication:** two agents in the same stage can finish in any wall-clock
order; the next stage’s view of “what the previous stage produced” is stable
as long as it consumes outputs through the fold (`WithContext` / stage
pipeline), not through live shared mutation.

## How the next stage / wave sees prior work

| Channel | Ordering | Notes |
|---|---|---|
| **`Task.WithContext`** | Explicit list, walked in **declaration order** of that list; reads write-once, mutex-protected outputs | Preferred merge channel for parallel siblings |
| **Stage pipeline** | Later stages start only after the previous stage’s barrier returns | Peers never inject into each other mid-stage |
| **Facts on `CrewOutput`** | Folded in declaration order; `dedupFacts` by `PayloadHash` (first occurrence wins) | Facts are **not** auto-injected into the next LLM prompt |
| **Memory inject** | Sees only the **committed snapshot** after the group barrier (D-M7) | See below — do not use Memory as a sibling merge bus |

## Memory: the historical caveat and D-M7

### The question

If short-term `Memory.Save` followed **completion order**, a later task with
**empty** `Context` and `InjectWhenEmptyContext` could observe sibling finishes
in wall-clock order — a semantic race that `-race` would not flag.

### The contract today (D-M7)

For **parallel waves and Staged stages**, AutoSave does **not** publish into
the live store mid-group:

1. Successful tasks **stash** entries in a per-group buffer.
2. At the barrier, `commitMemoryBuffer` writes them in **declaration order**.
3. The next wave/stage inject (`queryCommitted`) reads only that committed
   snapshot — **never** in-flight sibling writes.

Failed / cancelled tasks leave **no** buffer entry (G2). Fail-fast abort
**discards** the buffer.

So Memory is no longer an implicit “event log of completion order” for
parallel groups. It is still **not** a substitute for `WithContext` when you
need a precise, directed dependency: use Context for the merge edge; use
Memory for budgeted, policy-driven recall of the **committed** past.

Standalone `Memory.Save` outside Kickoff remains insertion-order (caller’s
problem). FileStore is single-writer per root in v1.

### Line between “nondeterminism by design” and “API invariant”

| Allowed nondeterminism | Forbidden / prevented |
|---|---|
| Wall-clock finish order inside a group | Fold order of `TasksOutput` / Context text / D-M7 commit |
| LLM token content | Same-wave Context cycles (fail fast) |
| Which parallel branch hits a rate limit first | Kickoff single-flight on one `*Crew` (`ErrCrewRunning`) |
| Stream delta interleaving on a shared sink | Demux via `StreamChunk.Task` / `Agent` (app still needs a mutex on the sink) |

We make illegal states **hard at Kickoff** (cycles) or **impossible by
construction** (slot-by-index fold, D-M7 buffer). We do **not** pretend that
LLM I/O itself is deterministic.

## Streaming and callbacks under concurrency

- **`WithStream`**: providers may run concurrently on Async waves; chunks carry
  `Task` / `Agent` for demux. The sink **must** be concurrency-safe.
- **`WithProgress` / `WithEvents`**: may fire from multiple goroutines; same
  rule. Events are metadata-only; Progress never carries bodies.
- **`Progress` + `Events`**: dual-emit for Progress-shaped lifecycle; use one
  sink or compose carefully (avoid double-handling if you wrap
  `ProgressAsEvents` **and** `WithEvents`).

## Kickoff single-flight

Only **one** `Kickoff` may run at a time on a given `*Crew`. Concurrent calls
return `ErrCrewRunning` (fail fast, no queue). Parallel crews ⇒ separate
`Crew` values.

## Practical recipes

```go
// Good: parallel research, deterministic merge
a := NewTask("research A", "...", agentA).WithAsync()
b := NewTask("research B", "...", agentB).WithAsync()
merge := NewTask("merge", "...", writer).WithContext(a, b)

// Good: Staged collect → synthesize
crew.Process = Staged
crew.Stages = []Stage{
    {Name: "collect", Tasks: []*Task{a, b}},
    {Name: "synthesize", Tasks: []*Task{merge}},
}

// Avoid: relying on Memory alone to order sibling outputs into a peer
// Prefer WithContext or a later stage/wave.
```

## Related

- [Crews](crews.md) — Async waves, Staged, Kickoff single-flight  
- [Memory](memory.md) — D-M7 commit barrier, Context vs Memory  
- [Tasks](tasks.md) — `WithContext`, `WithAsync`  
- Design archives: `Plan/PLAN.memory-async.md`, `Plan/PLAN.streaming.md`

## Test anchors

| Test / area | Guards |
|---|---|
| `TestStagedDeterministicOrder` | Fold ≠ completion order |
| Async wave + Context tests | Earlier-wave deps only |
| Memory policy D-M7 tests | Buffer commit order / inject snapshot |
| Stream Async demux tests | Task labels under concurrency |
| `go test -race` | Data races on shared structures |
