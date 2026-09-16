# Flows

> **Languages:** [English](../flows.md) · **Português** (atual)

Um **Flow** é um workflow event-driven sobre estado tipado `S`. **Não**
substitui Sequential / Hierarchical / Staged / waves Async — isso continua no
`Crew`. Um step pode chamar `crew.Kickoff` (composição, não aninhamento).

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

- `Start(name, fn)` — listen zero-dep (D-F4). `Start` **com** deps é erro.
- `Listen(name, fn, deps...)` — corre depois de **todos** os deps (D-F10). Sem deps ⇒ também é start.
- `Router(name, fn, deps...)` — devolve nomes exatos dos próximos steps (D-F6). Nome desconhecido falha.

Assinatura de step: `func(ctx context.Context, state *S) error`.
Assinatura de router: `func(ctx context.Context, state *S) ([]string, error)`.

## Contratos

| Tópico | Regra |
|---|---|
| Lock de estado | A lib **não** trava `S`. Steps paralelos compartilham `*S`; o app dono do sync (D-F5). |
| Join paralelo | Steps prontos da mesma barreira correm juntos; traces fazem fold por **ordem de registro** (D-F12). |
| Fail-fast | Default: o primeiro erro aborta na **próxima barreira** (D-F11). `WithFlowContinueOnError` grava `FlowResult.Errors` e pula dependentes (D-F8). |
| Cancel | `ctx` é checado na próxima barreira — steps em voo não são hard-killed (D-F11). |
| Single-flight | `Run` concorrente no mesmo `*Flow` devolve `ErrFlowRunning` (D-F9). |
| Ciclos | Detectados antes de qualquer step (`ErrFlowCycle`). |
| Eventos | `flow_started`, `flow_step_started`, `flow_step_completed`, `flow_completed` — só metadados (D-F7 / D-C4). |

Demo offline: `examples/flows_research`.
