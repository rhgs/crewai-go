# Training / trace export

> **Languages:** **English** (current) · [Português](pt-BR/training.md)

Library-side capture of Kickoff runs as JSONL. The core does **not** train
models — it writes a portable trace you can curate offline (D-X1–D-X8).

## Attach

```go
rec := crewai.NewTraceRecorder() // metadata-only default (D-X4 / D-C4)
crew.WithTracer(rec)
out, err := crew.Kickoff(ctx, nil)
_ = rec.Save("traces/success.jsonl") // caller-trusted path, mode 0600
```

Bodies (prompt, output, tool args) are **opt-in**:

```go
rec := crewai.NewTraceRecorder(crewai.WithTraceBodies(true))
```

Even with bodies on, prompt / output / fact-claim / tool-arg / tool-output
strings pass through `redactString` (fact `payload_hash` is a hash, not a
secret, and stays as-is). Filter what is
published (e.g. only named tasks):

```go
rec := crewai.NewTraceRecorder(crewai.WithTraceFilter(func(r crewai.TaskTraceRecord) bool {
    return r.Task == "keep"
}))
```

## Record shape

One JSONL line per **successfully folded** task (declaration order, D-X8).
Capture happens post-execute / pre-guardrail (D-X2); publication happens at
the barrier fold so async waves stay deterministic. Failed tasks are captured
but not published unless they make it into the fold.

Default (bodies off): `kickoff_id`, `wave`, `task`, `agent`, `duration_ms`,
fact provenance fields (no `claim`), tool names/durations (no args/output).

Offline demo: `examples/trace_export`.
