# Flows

> **Languages:** **English** (current) · [Português](pt-BR/flows.md)

A **Flow** is an event-driven workflow over typed state `S`. It does **not**
replace Sequential / Hierarchical / Staged / Async waves — those stay on
`Crew`. A step may call `crew.Kickoff` (composition, not nesting).

## Builder

```go
type MyState struct {
    mu    sync.Mutex
    Notes []string
    Route string
}

f := crewai.NewFlow[MyState]().
    Start("bootstrap", stepBootstrap).
    Listen("research", stepResearch, "bootstrap").
    Listen("news", stepNews, "bootstrap").
    Router("choose", stepChoose, "research", "news").
    Listen("write", stepWrite, "choose")

res, err := f.Run(ctx, MyState{})
```

- `Start(name, fn)` — zero-dep listen step (D-F4). `Start` **with** deps is an error.
- `Listen(name, fn, deps...)` — runs after **all** deps succeed (D-F10). No deps ⇒ also a start.
- `Router(name, fn, deps...)` — returns exact next step names (D-F6). Unknown names fail.

Step signature: `func(ctx context.Context, state *S) error`.
Router signature: `func(ctx context.Context, state *S) ([]string, error)`.

## Contracts

| Topic | Rule |
|---|---|
| State locking | Library does **not** lock `S`. Parallel steps share `*S`; the app owns sync (D-F5). |
| Parallel join | Ready steps of the same barrier run concurrently; traces fold by **registration order** (D-F12). |
| Fail-fast | Default: first step error aborts at the **next barrier** (D-F11). `WithFlowContinueOnError` records `FlowResult.Errors` and skips dependents (D-F8). |
| Cancel | `ctx` is checked at the next barrier — in-flight steps are not hard-killed (D-F11). |
| Single-flight | Concurrent `Run` on the same `*Flow` returns `ErrFlowRunning` (D-F9). |
| Cycles | Detected before any step (`ErrFlowCycle`). |
| Events | `flow_started`, `flow_step_started`, `flow_step_completed`, `flow_completed` — metadata-only (D-F7 / D-C4). |

`FlowResult.Steps` is the barrier-fold trace (registration order, including
skipped router branches). `FlowResult.Errors` is filled only with
`WithFlowContinueOnError`.

Offline demo: `examples/flows_research`.
