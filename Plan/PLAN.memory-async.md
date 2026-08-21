# Plan — Long-term Memory & Async beyond Staged

> **Status:** Plan only — do not implement until scheduled. **Open decisions D-M1–D-M7, D-A1–D-A6 and gaps G1–G12 closed 2026-08-21** (see §9).  
> **Related:** current in-RAM `Memory` (`memory.go`), `Crew.Memory` / `MemorySnapshot`, `Process=Staged` (`runStaged`), roadmap items in `PLAN.md` §6 P1/P2.  
> **Constraints:** zero external module dependencies in the core library (`go.mod` stays stdlib-only). Quality gates from §6.1 of `PLAN.security-residuals.md` apply to every implementation PR (coverage ≥ 90% on touched packages, race-clean, docs EN+PT-BR, CHANGELOG).  
> **External feedback:** DEV.to thread on staged semantic races vs `-race` ([article](https://dev.to/rhgs/from-python-to-go-rewriting-a-crewai-workflow-in-pure-stdlib-47nm) — freerave): merge must be orchestration contract (declaration-order fold after barrier). Memory completion-order caveat → **D-M7** visibility + stronger **D-M3**/**D-M4**. Reinforced **D-A1**/**D-A5**/**D-A6**.

---

## 0. Why these two together

| Capability | Today | Gap |
|---|---|---|
| **Memory** | Per-Kickoff in-RAM bag of `MemoryRecord`; substring `Search`; injected only when `Task.Context` is empty | No cross-run persistence, no semantic recall, no scoping (crew/agent/session), no budgeted injection into prompts |
| **Async** | Parallelism **only** inside a Staged stage (`WaitGroup` + cancel-on-fail) | Sequential/Hierarchical always serial; no DAG of independent tasks; no `Task.Async` / wave scheduler |

They interact: async waves write memory concurrently (store must stay race-safe **and** commit-visible only at barriers — D-M7); long-term stores must not assume single-threaded Kickoff. Designing them in one plan avoids two incompatible APIs (mutex-only memory would reintroduce the staged Memory caveat under Sequential+Async).

**Non-goals (this plan):**
- Streaming LLM tokens (separate P1).
- Full vector DB / cloud RAG products inside the core module.
- Replacing Staged process (Staged stays; async extends Sequential/Hierarchical).
- Cgo or mandatory third-party DB drivers in `github.com/rhgs/crewai-go`.

---

## 1. Baseline (code truth as of v0.5.0)

### 1.1 Memory today

```go
type Memory struct { /* RWMutex + []MemoryRecord */ }
type MemoryRecord struct { Agent, Task, Content string }

// Crew
Memory bool          // if true, Kickoff does c.mem = NewMemory()
mem    *Memory       // private
MemorySnapshot() *Memory

// Crew.execute
contextText := task.contextText()
if c.mem != nil && contextText == "" {
    contextText = c.mem.String()   // dump ALL records into the prompt
}
// after success:
c.mem.Save(MemoryRecord{Agent, Task: task.Name, Content: result})
```

Implications:
- Memory is **Kickoff-scoped** and discarded when the `Crew` value is GC'd.
- Injection is **all-or-nothing** and only when there is no `WithContext` chain — large crews blow the context window.
- No timestamps, IDs, namespaces, importance, or embeddings.
- Docs already say custom embeddings/persistence are “application-side”.

### 1.2 Concurrency today

| Process | Parallelism |
|---|---|
| `Sequential` | None — one task after another |
| `Hierarchical` | None — manager picks agent, then serial execute |
| `Staged` | All tasks in a stage concurrent; stages serial; optional stage continues on failure; panic recovery per task |

`Kickoff` is single-flight (`ErrCrewRunning`). Staged already proves the pattern: derived `stageCtx`, `WaitGroup`, ordered aggregation, first-error tracking.

### 1.3 Dependency signal already present

`Task.Context []*Task` is an explicit dependency list used to build `contextText()`. Async scheduling should **reuse** this as the DAG edge set (no second dependency DSL in v1).

---

## 2. Part A — Long-term memory

### 2.1 Goals

1. **Pluggable store** so apps can persist and reload memory across process restarts.
2. **Queryable recall** beyond substring: at least keyword/`Search`, optionally embedding similarity **without** forcing a vector dependency into core.
3. **Budgeted prompt injection** (top-K, max chars) so memory cannot DoS the context window.
4. **Backward compatible** `Crew.Memory bool` and existing `Memory` type remain valid for short-term runs.
5. **Facts stay separate** — `Fact` / `FactSource` remain provenance-grade connector data; long-term memory is soft working/episodic context, not a substitute for Facts.

### 2.2 Layered design

```
┌─────────────────────────────────────────────────────────┐
│ Crew / executor                                         │
│  - MemoryPolicy (when/how to inject & save)             │
│  - wave buffer (internal; D-M7 — not a public type)     │
└──────────────────────────┬──────────────────────────────┘
                           │ uses
┌──────────────────────────▼──────────────────────────────┐
│ MemoryStore (interface)                                 │
│  Put / Query / Delete / Close                           │
└────────────┬─────────────────────────────┬──────────────┘
             │                             │
   ┌─────────▼─────────┐         ┌─────────▼──────────────┐
   │ InMemoryStore     │         │ FileStore (stdlib)     │
   │ (wraps/extends    │         │ JSONL append + index   │
   │  today's Memory)  │         │ under a directory      │
   └───────────────────┘         └────────────────────────┘
             │                             │
             └────────── optional ─────────┘
                    EmbeddingFunc (app-provided)
                    for QueryModeSemantic
```

### 2.3 Public API sketch (stdlib-only core)

```go
// MemoryScope identifies a logical partition (tenant/crew/project).
// Empty scope is valid (default partition).
type MemoryScope string

// MemoryEntry is the persisted unit (superset of MemoryRecord).
type MemoryEntry struct {
    ID        string      // stable id (uuid-ish hex); assigned by store if empty
    Scope     MemoryScope
    Agent     string
    Task      string
    Content   string
    CreatedAt time.Time
    // Metadata is optional small key/value (string values only in v1).
    Metadata  map[string]string
    // Embedding is optional; nil means "not embedded yet".
    // Stored as float32 for compactness when present.
    Embedding []float32
}

// MemoryQuery controls recall.
type MemoryQuery struct {
    Scope     MemoryScope
    Text      string            // keyword / substring; optional if embedding set
    Embedding []float32         // if len>0 and store supports it → similarity
    Limit     int               // default e.g. 8; hard cap MaxMemoryQueryLimit
    MaxChars  int               // total Content chars across hits; 0 = no cap
    // Since, until optional time bounds (v1.1 if needed)
}

// MemoryStore is implemented by built-in and application stores.
// All methods must be safe for concurrent use.
type MemoryStore interface {
    Put(ctx context.Context, e MemoryEntry) (MemoryEntry, error)
    Query(ctx context.Context, q MemoryQuery) ([]MemoryEntry, error)
    // Delete removes by id within scope. Unknown id → nil error (idempotent).
    Delete(ctx context.Context, scope MemoryScope, id string) error
    Close() error
}

// EmbeddingFunc is provided by the application (HTTP call to OpenAI/Ollama/etc.).
// Core never bundles an embedding model.
type EmbeddingFunc func(ctx context.Context, texts []string) ([][]float32, error)

// MemoryPolicy configures automatic save/inject during Kickoff.
type MemoryPolicy struct {
    // AutoSave stores each successful task output (default true when store set).
    AutoSave bool
    // AutoEmbed runs EmbeddingFunc on save when non-nil (default false if func nil).
    AutoEmbed bool
    // InjectWhenEmptyContext keeps today's behavior: inject only if task.Context empty.
    // If false, always attempt inject (merged with explicit context — see D-M3).
    InjectWhenEmptyContext bool
    // Query defaults used for injection.
    DefaultLimit    int
    DefaultMaxChars int
    // Scope defaults to Crew.Name or a configured Scope.
    Scope MemoryScope
}

// On Crew:
//   MemoryStore MemoryStore  // optional; if nil && Memory bool → InMemoryStore
//   MemoryPolicy *MemoryPolicy // nil ⇒ NewMemoryPolicy() defaults
//   Embed EmbeddingFunc      // optional
//   Memory bool              // permanent v0.x alias (G4): true ⇒ ensure InMemoryStore for this Kickoff
```

**Zero-value / constructors (Go):**
- `NewMemoryPolicy()` (and `MemoryPolicy == nil` at Kickoff) sets `AutoSave=true`, `InjectWhenEmptyContext=true`, `DefaultLimit`/`DefaultMaxChars` to library defaults. A literal `MemoryPolicy{}` is **not** those defaults (`bool` zero is false).
- `DefaultAsyncMaxWorkers = 8`. `NewCrew` sets `AsyncMaxWorkers=8`. A composite literal `Crew{AsyncMaxWorkers: 0}` is **unlimited** by design (D-A4 escape hatch). There is no sentinel that makes `0` mean “use 8”.


**Compatibility bridge:**

```go
// *Memory continues to work for simple apps.
// Prefer: var _ MemoryStore = (*Memory)(nil) via adapter methods,
// OR keep Memory as today and wrap it:

func (m *Memory) AsStore() MemoryStore // returns in-memory store sharing m's data
```

Recommended: evolve `*Memory` to implement `MemoryStore` with `Put`/`Query` mapping to `Save`/`Search`, `Close` no-op, so existing tests stay green.

### 2.4 Built-in stores

| Store | Package location | Persistence | Search |
|---|---|---|---|
| **InMemoryStore** | root (`memory.go` / `memory_store.go`) | Process lifetime | substring (and cosine if embeddings present in entries) |
| **FileStore** | root (`filestore.go`) — **D-M2=A** | Directory of JSONL + small JSON index | substring on load/index; cosine if embeddings on entries |

**FileStore format (v1):**

```
{root}/
  scopes/{urlsafeScope}/
    entries.jsonl      # append-only MemoryEntry JSON lines
    meta.json          # schema version, counters
```

- Load index into RAM on `Open` (acceptable for tens of thousands of short entries).
- `Put` appends line + updates RAM index under mutex.
- Optional compaction API later (`Compact(ctx)` rewrite).
- File mode `0600` for files, `0700` for dirs (match OutputFile / token hygiene).
- Path must be caller-trusted; document jail expectations (no model-controlled roots).

**Explicitly deferred:** SQLite **inside core**. Reasons: zero-deps policy; `database/sql` + modernc is an external module. Document an **example** `examples/memory_sqlite` pattern *outside* the library or a future optional module `github.com/rhgs/crewai-go-memory-sqlite` if needed — not in this plan’s core PRs.

### 2.5 Embedding / semantic search (optional path)

1. App sets `Crew.Embed = myEmbedder`.
2. On `Put` with `AutoEmbed`, store calls embedder once, saves `Embedding`.
3. `Query` with `Embedding` or with `Text` (embed query text then score) ranks by cosine similarity, then applies `Limit` / `MaxChars`.
4. If no embeddings available, fall back to substring `Search` behavior.

```go
func cosine(a, b []float32) float64 // stdlib only; reject dim mismatch
```

**Security:** embedder is outbound HTTP under app control — same trust as LLM providers. Core must pass `ctx` and not log full text at Info by default.

### 2.6 Prompt injection policy

Replace blind `c.mem.String()` with a **Crew-owned committed snapshot** (D-M7), not a live `store.Query` mid-wave:

```go
// queryCommitted is the orchestrator's view: entries folded at the last
// barrier (plus earlier Kickoffs). It must NOT observe in-flight sibling Puts.
hits, err := queryCommitted(ctx, MemoryQuery{
    Scope: policy.Scope,
    Text: injectionQueryFromTask(task), // see D-M4 — empty ⇒ latest N committed
    Limit: policy.DefaultLimit,
    MaxChars: policy.DefaultMaxChars,
})
contextText = formatMemoryBlock(hits) // bounded, labeled
```

`MemoryStore.Put`/`Query` may still be immediately visible (persistence). The **prompt path** only reads the snapshot. Raw `store.Query` during Kickoff is not the merge channel (same invariant as “don’t use Memory as sibling bus”).

Format sketch:

```text
## Memory (recalled)
- [Researcher | task=market] …truncated content…
- [Writer | task=brief] …
```

Hard caps (constants):
- `MaxMemoryQueryLimit = 32`
- `MaxMemoryEntryBytes = 32 << 10` (**reject** oversized Content on Put — G11; no silent truncate)
- `DefaultMemoryMaxChars = 4000`

**Visibility (see D-M7):** automatic inject/`Query` used by the orchestrator MUST only see entries **committed** at a prior wave/stage barrier (or from earlier Kickoffs). In-flight sibling writes are invisible to prompt assembly. “Latest N” (**D-M4-A**) means latest N **committed** entries with **stable order** `(CommittedSeq or declaration index, then ID)` — never raw completion/append order of a still-open wave.

### 2.7 Interaction with Facts

| | Memory | Facts |
|---|---|---|
| Source | Usually LLM task outputs (soft) | `FactSource` tools only |
| Trust | Untrusted for compliance | Provenanced |
| Use | Prompt context / recall | Guardrails, audit, final reports |

Do **not** auto-copy Facts into MemoryStore in v1 (apps may do so explicitly).

### 2.8 Memory — PR breakdown

| PR | Deliverable | Notes |
|---|---|---|
| **M1** | `MemoryEntry`, `MemoryStore`, `MemoryQuery`; `*Memory` implements store; adapter tests | No Crew behavior change yet |
| **M2** | `MemoryPolicy` + Crew wiring (`Store`, inject/save paths); **wave-buffered Save + barrier commit (D-M7)**; deprecate dump-all | Backward compatible `Memory bool`; no mid-wave inject visibility |
| **M3** | `FileStore` JSONL + docs EN/PT + example `examples/memory_file` | Path security, 0600 |
| **M4** | `EmbeddingFunc` + cosine query path + docs; example with mock embedder | No network in tests |
| **M5** (opt) | Memory tool for agents (`recall_memory` / `remember`) | Only if product wants agent-driven recall |

### 2.9 Memory — tests & gates

- Unit: Put/Query/Delete concurrency (`-race`), limit/maxchars, scope isolation, oversized content.
- FileStore: restart durability (Put → Close → Open → Query), corrupt line skip/fail policy (**D-M5**).
- Crew integration: `Memory bool` still works; policy inject doesn’t exceed MaxChars; async Kickoff (once async ships) doesn’t race the store.
- **Semantic order:** `TestMemoryCommitOrderMatchesDeclaration` — slow-first / fast-second parallel tasks → committed Memory sequence matches **declaration index**, not finish time (mirror `TestStagedDeterministicOrder`). Inject in the next wave must not observe uncommitted sibling entries.
- Coverage ≥ 90% on new files/packages.
- Docs: `docs/memory.md` + pt-BR rewrite; elevate DEV caveat to **API invariant** (Memory is not a merge channel for parallel siblings — use `WithContext`); README blurb; CHANGELOG.

### 2.10 Memory — decisions (closed 2026-08-21)

| ID | Question | Options | Recommendation |
|---|---|---|---|
| **D-M1** | Default store when `Memory=true` and `Store==nil`? | (A) InMemory only (B) require explicit Store | **A** — zero surprise *(unchanged by DEV thread)* |
| **D-M2** | FileStore in root vs `memory/` subpackage? | (A) root (B) subpackage | **A** if no import cycle (v1 speed); **B** `memoryfile` if FileStore grows or cycles appear *(unchanged by DEV thread; still a product pick)* |
| **D-M3** | Inject when task already has `Context`? | (A) never (today) (B) append memory block (C) policy flag | **C** with default **A** (`InjectWhenEmptyContext=true`). **Reinforced:** even when inject runs, never expose **in-wave / uncommitted** entries (D-M7). Auto-inject must not become a hidden sibling-merge channel. |
| **D-M4** | Automatic injection query text | (A) empty → latest N (B) task description keywords (C) embed description | **A** for v1; **B** later. **Reinforced:** “latest N” over **committed snapshot** only, stable order `(commit seq / declaration index, ID)` — not live completion/append order. |
| **D-M5** | Corrupt JSONL line | (A) fail Open (B) skip line + warning | **B** with counter in meta *(unchanged)* |
| **D-M6** | Cross-Kickoff FileStore lifecycle | (A) app opens/closes Store (B) Crew.Close | **A** + docs; Crew does not Close app stores unless optional `StoreOwner` *(unchanged)* |
| **D-M7** | When do task `Save`/`Put`s become visible to inject/`Query` used by orchestration? | (A) live event-log (completion order) (B) per-task buffer + fold at wave/stage barrier in declaration order (C) hybrid: raw `Put` immediate, orchestrator reads only committed | **B** (preferred). Matches Staged Facts/TasksOutput contract and answers DEV follow-up: API makes finish-order dependency hard to stumble into. **C** only if apps need a true wall-clock tail API. **A** rejects for prompt path. |

#### 2.10.1 Orchestration invariants (API should make hard to violate)

| Channel | Deterministic merge? | Enforcement |
|---|---|---|
| `Task.Context` / `WithContext` | Yes | Only deps already **done**; same-wave / cycle → hard error |
| Facts / `TasksOutput` / `Final` | Yes | Per-task slot + fold by **declaration index** after barrier |
| Memory auto-save → auto-inject (same Kickoff, parallel waves) | **Yes (D-M7=B)** | Buffer per task; commit at barrier in declaration order; inject sees committed only |
| Memory as cross-Kickoff / debug log | May be chronological | Outside hot prompt path; document separately |

Public docs must promote the DEV caveat to an **invariant**: *do not use Memory as the merge channel for parallel siblings — use `WithContext`.*

**G12 implementation:** validate cycle / same-**wave** `Task.Context` at Kickoff start against the **planned waves**, not “any Context pointing at a later slice index”. Sequential with no `Async` still allows `WithContext` of an earlier task (today). Same-stage Context under Staged becomes a **hard error** (may break anyone relying on the race — that is intended).

---

## 3. Part B — Async beyond Staged

### 3.1 Goals

1. Run **independent** tasks concurrently outside Staged when the user opts in.
2. Honor **dependencies** via existing `Task.Context` (DAG).
3. Deterministic **aggregation order** (declaration order), like Staged.
4. Clear failure policy (fail-fast vs collect).
5. Keep Staged semantics unchanged; avoid two conflicting parallel runtimes if possible by **sharing a scheduler primitive**.

### 3.2 Mental model

```
Tasks with edges t → d in t.Context means: t depends on d (d before t).

Wave 0: tasks with no unmet deps
Wave 1: tasks unblocked after wave 0 completes
…
```

This is a **DAG scheduler**. Cycles → hard error before execution (`ErrTaskDependencyCycle`).

### 3.3 API sketch

```go
// Task.Async, when true, marks the task as eligible to run concurrently
// with other eligible tasks once dependencies are satisfied.
// Default false ⇒ behavior identical to today (fully serial in Sequential/
// Hierarchical, except Staged which ignores this flag and uses stage batches).
Async bool

// Optional future sugar:
func (t *Task) WithAsync() *Task { t.Async = true; return t }

// Crew-level defaults:
// AsyncMaxWorkers int // default 8; 0 = unlimited (bounded by ready tasks)
// AsyncFailFast bool  // default true — cancel siblings on first error
```

**Process interaction:**

| Process | Behavior |
|---|---|
| `Sequential` | Build DAG from `c.Tasks`. Schedule ready `Async` tasks in parallel; non-Async tasks still wait for deps but run alone in their slot (no sharing a wave with others) **or** simpler rule: **D-A1**. |
| `Hierarchical` | Same DAG on `c.Tasks`, but agent resolution still via manager when `task.Agent==nil` **before** execute (serial manager calls or parallel — **D-A2**). |
| `Staged` | **Unchanged.** Stage membership defines batches; `Task.Async` ignored (document). Reuse internal `runParallelTasks` helper underneath both. |

### 3.4 Recommended Sequential rule (D-A1 recommendation)

**Wave-based for all tasks; `Async` only controls whether multiple ready tasks may start together:**

- Ready set R = tasks whose deps are done.
- Let P = { t in R | t.Async }, S = { t in R | !t.Async }.
- If P non-empty: run all of P concurrently (up to MaxWorkers); then continue.
- Else: run one task from S (stable order by index) serially.
- Rationale: non-Async tasks never race each other; Async tasks get parallelism; deps always respected.

Alternative (stricter): only tasks with `Async=true` ever parallelize; others always global-serial even if independent — simpler but weaker.

### 3.5 Shared primitive (implementation core)

Extract from `runStaged`:

```go
// runTaskGroup runs tasks concurrently (or serial if len==1), preserves
// results by index, optional fail-fast cancel, panic recovery.
func (c *Crew) runTaskGroup(
    ctx context.Context,
    label string, // stage name or "async-wave-N"
    tasks []*Task,
    agents []*Agent, // pre-resolved, parallel to tasks
    failFast bool,
) (results []groupResult, firstErr error)
```

Then:
- `runStaged` → per stage call `runTaskGroup`.
- New `runSequential` / hierarchical path → DAG waves call `runTaskGroup` for the parallel subset.

### 3.6 Failure & cancel semantics

Align with Staged non-optional:
- **FailFast true (default):** cancel wave context; wait WaitGroup; return first real error (ignore sibling `context.Canceled` when possible).
- **FailFast false:** run all ready tasks in the wave; accumulate errors; decide whether to schedule downstream (**D-A3**: block dependents of failed tasks only vs abort whole Kickoff).

Recommendation **D-A3-A:** dependents of a failed task are skipped with error; unrelated branches continue if FailFast false; if FailFast true, whole Kickoff aborts.

### 3.7 Memory / output safety under async (data races **and** semantic races)

`-race` is necessary but not sufficient (DEV.to / freerave). Merge is part of the **orchestration contract**, not an accident of scheduling.

**Data-plane (mutex / ownership):**
- `MemoryStore` / `*Memory` mutexed for concurrent `Save`/`Put` calls from workers.
- `task.setOutputWithJail` — one goroutine per task.
- `CrewOutput` aggregation **after** wave join — main goroutine only.
- Progress callbacks must stay concurrent-safe.
- Input interpolation remains in Kickoff before schedule (single-threaded).

**Semantic-plane (visibility / order) — D-M7 + D-A1:**
- Intra-wave tasks are **independent**: they must not read each other’s outputs, facts, or **committed** memory while the wave is open.
- Same-wave `Task.Context` edges are invalid (cycle / not-yet-done dep → error before run), same spirit as “no same-stage Context” in Staged.
- After `wg.Wait()`, fold results by **declaration index** (not completion order) into `TasksOutput`, Facts, Warnings, Final — same as today’s Staged barrier.
- Memory: workers write into a **per-task buffer**; barrier commits buffers in declaration order into the store. Next wave inject/`Query` sees only the post-commit snapshot.
- **Invariant tests:** extend the slow-first/fast-second pattern beyond outputs to Memory commit order.

Do **not** document Memory completion order as “by design” on the prompt path. Residual nondeterminism (traces, raw metrics) stays off the prompt assembly path.

### 3.8 Hierarchical + async caveat

Manager `delegate()` is an LLM call. Options:
1. Resolve all agents serially first, then async execute (predictable).
2. Resolve agent lazily in each worker (manager LLM must be concurrent-safe — providers are).

Recommendation: **serial resolve for tasks that need manager, then async execute** for the wave (simpler mental model).

### 3.9 Async — PR breakdown

| PR | Deliverable |
|---|---|
| **A1** | Extract `runTaskGroup` from `runStaged`; Staged behavior golden tests unchanged |
| **A2** | DAG builder + cycle detection + wave planner unit tests |
| **A3** | Wire Sequential (+ Hierarchical) with `Task.Async` + Crew `AsyncMaxWorkers` / `AsyncFailFast` |
| **A4** | Docs EN/PT (`crews.md`, `tasks.md`), example `examples/async_tasks`, CHANGELOG |
| **A5** (opt) | `Process=DAG` alias or `Crew.Schedule=Dependency` if Sequential+Async proves confusing |

### 3.10 Async — tests & gates

- Unit: cycle detection, topo waves, stable order.
- Integration: two independent Async tasks run with overlapping time (channel rendezvous test); dependent task starts only after upstream `done`.
- FailFast cancel test; optional continue test.
- Panic in one Async task does not crash process.
- `-race` on async Kickoff with Memory store saves.
- Semantic: Memory commit order == declaration order after parallel wave; next-wave inject cannot see in-flight sibling saves.
- Coverage ≥ 90% on scheduler files.

### 3.11 Async — decisions (closed 2026-08-21)

| ID | Question | Options | Recommendation |
|---|---|---|---|
| **D-A1** | Scheduling rule for mixed Async/sync | (A) wave rule above (B) global serial except pure-Async subgraph | **A** — **reinforced by DEV thread**: wave + barrier + aggregate by declaration index (same contract as Staged). Document explicitly in public docs. |
| **D-A2** | Hierarchical agent resolution | (A) serial pre-resolve (B) parallel lazy | **A** *(unchanged by thread)* |
| **D-A3** | FailFast false + failed upstream | (A) skip dependents only (B) abort crew | **A** *(unchanged)* |
| **D-A4** | Default `AsyncMaxWorkers` | (A) 0 unlimited (B) `GOMAXPROCS` (C) 8 | **C + A-as-escape:** default **8**; set programmatically; **0 = unlimited**. Closes P8 (LLM fan-out) without removing the unlimited knob. |
| **D-A5** | Staged interaction with `Task.Async` | (A) ignore flag (B) error if set | **A** — **reinforced**: Staged already owns batch parallelism; don’t create a second parallel rule inside stages. Optional one-shot `slog` Warn if `Async=true` under Staged. |
| **D-A6** | New process constant vs Sequential flag | (A) Sequential+Task.Async only (B) `Process=Async` | **A** for v1 — fewer concepts. **Reinforced:** real contract is “DAG + barrier + fold”; if “Sequential with Async” confuses users, ship optional **A5** alias `Process=DAG` later without breaking A. |

---

## 4. Cross-cutting concerns

### 4.1 Zero dependencies

| Need | Approach |
|---|---|
| UUID/id | `crypto/rand` hex |
| Persistence | JSON/JSONL + files |
| Embeddings | App-provided `EmbeddingFunc` |
| Vector DB | Out of core |
| SQLite | Out of core (example/external module later) |

### 4.2 Security

- FileStore roots are **trusted paths** (same class as `OutputDir`).
- Bound entry size and query limits (DoS / prompt flooding).
- Redact paths in errors if they might contain home directories? Optional; prefer consistent `redactError` on surfaced errors.
- Async cancel must not leave FileStore partially corrupted — append line fully under write lock; use `bufio` + flush.

### 4.3 Observability

- Progress events: reuse `task_started` / `task_completed`; optional `wave_started` / `wave_completed` (**D-X1** default skip in v1 to avoid event spam).
- Debug logs: wave index, worker count, memory hit count (not full contents at Info).

### 4.4 Versioning

| Slice | Suggested release |
|---|---|
| M1–M2 (interfaces + policy + in-RAM) | v0.6.0 minor |
| M3 FileStore | v0.6.0 or v0.6.1 |
| M4 embeddings | v0.6.x |
| A1–A4 async DAG | v0.6.0 if ready together; else v0.7.0 |

Prefer **one minor (v0.6.0)** shipping M1–M2 + A1–A4 if capacity allows — marketed as “durable memory foundations + async tasks”. FileStore/embeddings can trail.

### 4.5 Quality gates (every PR)

Copied/adapted from security residuals:

1. Code review checklist (trust boundaries, caps, ctx, race).
2. `go test ./...` and `go test -race` on touched packages.
3. Coverage ≥ 90% on new/changed packages.
4. Docs EN + PT-BR + godoc + CHANGELOG bilíngue.
5. No new `require` in `go.mod`.
6. `gofmt` / `go vet` clean.

---

## 5. Suggested implementation order

```
Phase 0  ~~Close open decisions D-M1–D-M7 and D-A1–D-A6~~ **done 2026-08-21** (gaps G1–G12 closed in the same pass)
Phase 1  A1 extract runTaskGroup  ∥  M1 MemoryStore interface
         (disjoint files: A1 touches crew.go; M1 touches memory.go only)
Phase 2  M2 MemoryPolicy + D-M7 buffer **on runTaskGroup**  (requires A1 merged)
         A2 DAG planner may start after A1; do **not** land M2 before A1
Phase 3  A3 Sequential/Hierarchical Async  ∥  M3 FileStore
Phase 4  A4 docs/examples; M4 embeddings (can split)
Phase 5  Optional M5 memory tools / A5 Process sugar
```

**Why A1 before M2:** D-M7/G9 commit lives in the stage/wave barrier. If M2 ships first, the buffer is copied into `runStaged` and then rewritten when A1 extracts `runTaskGroup`. M1 is free to parallelize with A1.

File ownership:
- Async: `crew.go`, `process.go`, new `schedule.go`, crew tests — **A1 then A2/A3**.
- Memory types: `memory.go`, `memory_store.go`, `filestore.go` — M1/M3/M4.
- M2 is the **handoff**: policy + wave buffer in `runTaskGroup` (touches `crew.go` after A1).

---

## 6. Documentation plan

| Doc | Updates |
|---|---|
| `docs/memory.md` + pt-BR | Store interface, FileStore, policy, embeddings hook, vs Facts; **D-M7 visibility / commit barrier**; invariant: Memory ≠ sibling merge channel — use `WithContext` (promote DEV caveat to API contract) |
| `docs/crews.md` + pt-BR | Async waves, barrier+declaration-order fold, FailFast, MaxWorkers, Staged unchanged |
| `docs/tasks.md` + pt-BR | `Task.Async`, Context as DAG deps; same-wave Context forbidden |
| `README` EN/PT | Feature bullets when shipped |
| `examples/memory_file` | Persist across two Kickoffs |
| `examples/async_tasks` | Two parallel research tasks → merge task (explicit `WithContext`, not Memory) |
| `Plan/PLAN.md` | Checkboxes when PRs merge |
| DEV.to reply (optional) | Point to shipped invariant / D-M7 when implemented |

---

## 7. Acceptance criteria (epic done)

### Long-term memory
- [ ] `MemoryStore` interface + in-memory implementation; existing `Memory bool` tests pass.
- [ ] Policy-based inject with hard caps; no unbounded dump by default when policy uses limits.
- [ ] **D-M7:** parallel wave commits Memory in declaration order; next-wave inject cannot see in-flight sibling writes.
- [ ] FileStore survives process restart in tests.
- [ ] Optional embedding path tested with fake embedder (cosine rank).
- [ ] Bilingual docs + example; Memory/WithContext invariant documented (not only in a blog reply).

### Async beyond staged
- [ ] Independent `Task.Async` tasks overlap in time under Sequential.
- [ ] `Task.Context` dependencies enforced; cycles / same-wave edges error clearly.
- [ ] Staged golden behavior unchanged (same tests); aggregation remains declaration-order after barrier.
- [ ] FailFast cancel + panic recovery covered.
- [ ] `-race` clean with memory saves under async **and** semantic Memory order test green.
- [ ] Bilingual docs + example (wave/barrier/fold contract called out).

### Global
- [ ] `go.mod` still free of new deps.
- [ ] Coverage gates met.
- [ ] CHANGELOG EN/PT under Unreleased until release tag.

---

## 8. Risk register

| Risk | Mitigation |
|---|---|
| Prompt bloat from memory | Default MaxChars + Limit; metrics in debug |
| DAG UX confusion vs Staged | Docs comparison table; Staged ignores Async |
| FileStore growth | Document compaction later; entry size cap |
| Hierarchical manager bottleneck | Serial pre-resolve agents |
| Scope creep into RAG product | Embeddings are hooks only; no chunking pipeline in core |
| Import cycles Crew ↔ memory file | **D-M2=A:** FileStore in root `filestore.go`; no subpackage unless a cycle appears |
| Semantic race via Memory completion order | **D-M7=B** Crew-owned buffer+fold; inject only committed snapshot; tests like Staged deterministic order. Raw `store.Query` is not the prompt path |
| Users treating Memory as sibling merge bus | Docs invariant + prefer `WithContext`; default inject policy conservative (D-M3) |
| `crew.go` dual-owner (A1 vs M2) | **A1 before M2**; M1 parallel OK |
| Go zero-value vs D-A4/policy defaults | `NewCrew` ⇒ MaxWorkers 8; literal `0` = unlimited. `NewMemoryPolicy()` / nil policy for bool defaults |
| Same-wave Context false positive | G12 uses **wave plan**, not slice order (Sequential serial `WithContext` stays valid) |

---

## 9. Decision log (closed 2026-08-21)

Closed in one pass to match residuals D1–D7 process, v0.5.0 strategy (P1–P10), and DEV.to semantic-race thread (freerave). Implementation still **not** scheduled.

### 9.1 Official (D-M* / D-A*)

| ID | Decision | Date | Notes |
|---|---|---|---|
| D-M1 | **A** | 2026-08-21 | `Memory=true` + `Store==nil` ⇒ InMemoryStore (zero surprise / P6) |
| D-M2 | **A** | 2026-08-21 | FileStore in root (`filestore.go`); move to `memoryfile` only if import cycle or FileStore grows |
| D-M3 | **C** (default A) | 2026-08-21 | `InjectWhenEmptyContext` policy flag; default true (today). Never inject uncommitted / in-wave entries (D-M7) |
| D-M4 | **A** | 2026-08-21 | Empty query → latest N on **committed** snapshot; stable order `(commit seq / declaration index, ID)`. Keywords/embed later |
| D-M5 | **B** | 2026-08-21 | Corrupt JSONL: skip line + warning + counter in meta (P3 warn+capture) |
| D-M6 | **A** | 2026-08-21 | App opens/closes Store. Crew does not Close app stores unless optional `StoreOwner` (Crew-created default only) |
| D-M7 | **B** | 2026-08-21 | Per-task buffer + fold at wave/stage barrier in **declaration order**. Orchestrator inject/`Query` sees committed only. Live event-log rejected on prompt path (DEV thread / P4 / P5) |
| D-A1 | **A** | 2026-08-21 | Wave rule: ready Async run together; sync never share a wave; aggregate by declaration index after barrier |
| D-A2 | **A** | 2026-08-21 | Hierarchical: serial manager pre-resolve, then async execute |
| D-A3 | **A** | 2026-08-21 | `FailFast=false`: skip dependents of failed tasks only; unrelated branches continue. `FailFast=true` (default) aborts Kickoff |
| D-A4 | **C** (default 8; **0 = unlimited**) | 2026-08-21 | Default **8** (P8 — cap LLM fan-out). Programmatic override on Crew. **0 = unlimited** (A as escape hatch, bounded by ready set). Docs: warn that 0 disables the cap ($$/429). |
| D-A5 | **A** | 2026-08-21 | Staged ignores `Task.Async` (stages own batches). See G5 for one-shot Warn |
| D-A6 | **A** | 2026-08-21 | No new `Process` constant. Sequential/Hierarchical + `Task.Async` only. Optional `Process=DAG` alias later (A5) if naming confuses |

### 9.2 Gaps closed in the same pass (G1–G12)

Not originally numbered in §2.10/§3.11; recorded here so implementation PRs do not re-litigate them.

| ID | Decision | Date | Notes |
|---|---|---|---|
| G1 | **A** | 2026-08-21 | Fold/commit order = index in `Crew.Tasks` for this Kickoff (same contract as Staged / DEV reply). No mandatory `Task.ID` in v1 |
| G2 | **A** | 2026-08-21 | AutoSave only on **successful** task output. Failures stay on `CrewOutput` / logs, not the recall channel |
| G3 | **B** | 2026-08-21 | Default `MemoryPolicy.Scope` = `Crew.Name` when non-empty; else empty (default partition) |
| G4 | **A** | 2026-08-21 | `Memory bool` remains a permanent v0.x alias (`true` ⇒ ensure InMemoryStore for the Kickoff). No deprecation noise |
| G5 | **B** | 2026-08-21 | If `Task.Async=true` under `Process=Staged`: one-shot `slog` Warn (P3). Not an error (D-A5=A) |
| G6 | **A** | 2026-08-21 | `CreatedAt` = task **finish** time (informational). **Commit / latest-N order** is declaration index (D-M7), not wall-clock |
| G7 | **A** | 2026-08-21 | FileStore v1 is **single-writer**; document it. No `flock`. Path is caller-trusted |
| G8 | **A** | 2026-08-21 | `AutoEmbed` runs **serially at the barrier fold**. No extra embed fan-out in v1 |
| G9 | **A** | 2026-08-21 | **M2 includes the commit barrier on Staged now**, not only Sequential+Async. Closes the published Memory caveat |
| G10 | **A** | 2026-08-21 | Prefer one minor **v0.6.0** shipping M1–M2 + A1–A4 if capacity allows; FileStore/embeddings may trail |
| G11 | **A** | 2026-08-21 | `MaxMemoryEntryBytes`: **reject** on Put (hard limit). AutoSave failure → warn+capture (P3), do not abort Kickoff |
| G12 | **A** | 2026-08-21 | Cycle and same-wave `Task.Context` validated at **Kickoff start** (P2 fail-fast). Hard error, not mid-wave |

---

## 10. Status tracking

| Workstream | Phase | PRs | Status |
|---|---|---|---|
| Memory interfaces + policy + **D-M7 commit barrier** | P2 | M1–M2 | M1 shipped 2026-08-21 (branch feat/phase1-a1-m1); M2 planned |
| FileStore | P2 | M3 | Planned |
| Embeddings hook | P2 | M4 | Planned |
| runTaskGroup extract | P1 | A1 | Shipped 2026-08-21 (branch feat/phase1-a1-m1) — Staged tests golden unchanged |
| DAG + Async wire-up | P1 | A2–A4 | A2–A3 shipped 2026-08-21 (branch feat/phase1-a1-m1); A4 docs mostly in PR |
| Optional tools / sugar | P3 | M5/A5 | Deferred |

