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

## Long-term store interface (v0.6 preview)

`*Memory` also implements `crewai.MemoryStore`, the pluggable long-term
memory contract the durable backends will use:

```go
var store crewai.MemoryStore = crew.NewMemory() // or existing *Memory

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

`Crew.Memory` still wires only the in-RAM short-term memory in v0.5; the Crew
never calls `MemoryStore` yet — a `MemoryPolicy` integration arrives with M2.
FileStore/JSONL (`filestore.go`) and embedding ranking follow as M3/M4.
