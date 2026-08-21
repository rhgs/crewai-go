# Memory

> **Languages:** **English** (current) · [Português](pt-BR/memory.md)

**Memory** stores task outputs during a crew's execution, allowing later tasks
to access what has already been produced — even without an explicit
`WithContext`.

## Enabling it

```go
crew := crewai.NewCrew(agents, tasks)
crew.Memory = true
crew.Kickoff(ctx, nil)
```

With `Memory = true`, each task with no explicit context receives, in its
prompt, a summary of the memory accumulated up to that point.

## Reading the memory

```go
mem := crew.MemorySnapshot() // *crewai.Memory (nil if Memory == false)
for _, r := range mem.Records() {
	fmt.Printf("[%s] task=%q → %s\n", r.Agent, r.Task, r.Content)
}
```

Each record is a `MemoryRecord`:

```go
type MemoryRecord struct {
	Agent   string // the role of the agent that produced it
	Task    string // the related task's name
	Content string // the memorized content
}
```

## Searching

Simple text search (substring, case-insensitive):

```go
for _, r := range mem.Search("sales") {
	fmt.Println(r.Content)
}
```

An empty query returns all records.

## Using memory standalone

`Memory` can also be used in isolation:

```go
m := crewai.NewMemory()
m.Save(crewai.MemoryRecord{Agent: "Analyst", Content: "revenue grew 12%"})
fmt.Println(m.String())
```

## Context vs Memory

- **Context** (`WithContext`) is **explicit and directed**: you say exactly
  which outputs feed into a task.
- **Memory** is **implicit and cumulative**: it is available to all following
  tasks that have not defined their own context.

Use context for precise dependencies; use memory to give the team a general
"awareness" of what has been done.

## Custom implementations

The built-in `Memory` is in-RAM and concurrency-safe. For semantic search
(embeddings) or persistence, you can wrap/replace this logic in your
application — the `MemoryRecord` structure is intentionally simple.

## MemoryPolicy + commit barrier (M2)

`Crew.MemoryPolicy` controls automatic save/inject. Nil means
`NewMemoryPolicy()` defaults (`AutoSave=true`, `InjectWhenEmptyContext=true`,
budgeted `DefaultLimit` / `DefaultMaxChars`). A literal `MemoryPolicy{}` is
**not** those defaults — use `NewMemoryPolicy` and override fields.

```go
crew.Memory = true // ensures InMemory store when MemoryStore is nil
crew.Name = "my-crew" // default MemoryPolicy.Scope (G3)
crew.MemoryPolicy = crewai.NewMemoryPolicy()
crew.MemoryPolicy.DefaultMaxChars = 2000
// or supply an external store (app owns Close — D-M6):
// crew.MemoryStore = myStore
```

**D-M7 visibility invariant:** during a parallel wave/stage, AutoSave writes
go into a per-task buffer. They are committed to the store **only at the
barrier**, in declaration order. The next wave's inject/`Query` sees only the
committed snapshot — never in-flight sibling writes. Do **not** use Memory as
the merge channel for parallel siblings; use `WithContext`.

Failed tasks are never AutoSaved (G2). AutoSave errors are warned and
captured; they do not abort Kickoff (G11).


## FileStore (JSONL, M3)

`OpenFileStore(dir)` opens a durable stdlib-only backend under a
**caller-trusted** root (never pass model-controlled paths):

```go
store, err := crewai.OpenFileStore("/var/lib/myapp/crew-memory")
if err != nil { /* … */ }
defer store.Close() // app owns lifecycle (D-M6)

crew.Name = "finance-crew"       // default MemoryPolicy.Scope (G3)
crew.MemoryStore = store
crew.MemoryPolicy = crewai.NewMemoryPolicy()
```

Layout:

```
{root}/scopes/{urlsafeScope}/
  entries.jsonl   # append-only MemoryEntry JSON lines (+ delete tombstones)
  meta.json       # schema version, corrupt-skip counter
```

- Files `0600`, directories `0700`.
- v1 is **single-writer** (G7): one process per root; one `*FileStore` is
  mutex-safe for concurrent Puts/Queries inside that process.
- Corrupt JSONL lines are **skipped** on Open and counted in `meta.json`
  / `CorruptSkipped()` (D-M5).
- Survives process restart: Put → Close → Open → Query.

See `examples/memory_file`.

## Long-term store interface

`*Memory` also implements `crewai.MemoryStore`, the pluggable long-term
memory contract the durable backends will use:

```go
var store crewai.MemoryStore = crewai.NewMemory() // or existing *Memory

e, _ := store.Put(ctx, crewai.MemoryEntry{Agent: "Analyst", Content: "revenue grew 12%"})
hits, _ := store.Query(ctx, crewai.MemoryQuery{Limit: 5})
_ = store.Delete(ctx, "", e.ID)
```

- `Put` assigns a stable `ID` and `CreatedAt`; entries over
  `MaxMemoryEntryBytes` are rejected (`ErrMemoryEntryTooLarge`).
- `Query` honors `Limit` (default `DefaultMemoryQueryLimit`, hard cap
  `MaxMemoryQueryLimit`) and `MaxChars` (default `DefaultMemoryMaxChars`,
  negative = uncapped), searching `Content`/`Task` case-insensitively. An
  empty `Text` returns the latest entries first.
- `Delete` is idempotent; `Close` is a no-op for the in-memory store.
- Entries are partitioned by `MemoryScope`; short-term `Save` records live in
  the default (empty) scope.

`Crew.Memory` remains the permanent v0.x alias that ensures an InMemory store
when `MemoryStore` is nil. Embedding ranking follows as M4.
