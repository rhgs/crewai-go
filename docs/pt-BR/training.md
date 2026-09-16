# Training / export de traces

> **Languages:** [English](../training.md) · **Português** (atual)

Captura no lado da lib de runs do Kickoff em JSONL. O core **não** treina
modelos — escreve um trace portátil para curadoria offline (D-X1–D-X8).

## Anexar

```go
rec := crewai.NewTraceRecorder() // metadata-only default (D-X4 / D-C4)
crew.WithTracer(rec)
out, err := crew.Kickoff(ctx, nil)
_ = rec.Save("traces/success.jsonl") // path confiável do caller, modo 0600
```

Bodies (prompt, output, args de tool) são **opt-in**:

```go
rec := crewai.NewTraceRecorder(crewai.WithTraceBodies(true))
```

Mesmo com bodies, strings passam por `redactString`. Filtro de publicação:

```go
rec := crewai.NewTraceRecorder(crewai.WithTraceFilter(func(r crewai.TaskTraceRecord) bool {
    return r.Task == "keep"
}))
```

## Forma do registro

Uma linha JSONL por task **dobrada com sucesso** (ordem de declaração, D-X8).
Captura pós-execute / pré-guardrail (D-X2); publicação na fold da barreira.
Tasks que falham são capturadas mas não publicadas.

Default (bodies off): `kickoff_id`, `wave`, `task`, `agent`, `duration_ms`,
campos de proveniência de facts (sem `claim`), nomes/duração de tools (sem
args/output).

Demo offline: `examples/trace_export`.
