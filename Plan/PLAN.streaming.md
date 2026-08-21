# Plan — Streaming LLM tokens

> **Status:** **Design** (not started). Next product epic after v0.6.0 (memory + async).  
> **Decisions:** D-S1–D-S12 proposed below (§9); close before coding Phase 1.  
> **Related:** `llm.go` (`LLM`), `toolcall.go` (`ToolCallingLLM` pattern), `executor.go` / `structured.go` / `loop.go`, providers under `llm/*`, roadmap `PLAN.md` §6 P1.  
> **Constraints:** zero external module dependencies in the core library (`go.mod` stays stdlib-only). Quality gates from §6.1 of `PLAN.security-residuals.md` apply to every implementation PR (coverage ≥ 90% on touched packages, race-clean, docs EN+PT-BR, CHANGELOG).  
> **Non-goals of sibling epics:** this plan does **not** include callbacks/telemetry beyond stream delivery (P2), JSON Schema remainder (P2), Flows (P3), or memory-async M5/A5.

---

## 0. Why streaming now

| Capability | Today (v0.6.0) | Gap |
|---|---|---|
| **LLM I/O** | `LLM.Call(ctx, messages) (string, error)` — full completion buffered | Long answers feel frozen; UIs cannot show tokens as they arrive |
| **Observability** | `WithProgress` metadata only (no prompt/output bodies) | Apps that need live text must re-implement provider SSE themselves |
| **Providers** | OpenAI/Anthropic/Ollama/xAI all buffer `io.ReadAll` | Wire protocols already support stream (`stream: true`, SSE, NDJSON) |

Streaming is the last **P1** roadmap item. It is independent of memory/async (those ship without it) but benefits the same UIs that already use `WithProgress` for task lifecycle.

**Non-goals (this plan):**
- Replacing `LLM.Call` or breaking existing implementers.
- Streaming **partial native tool-call argument JSON** in v1 (hard across providers; see D-S4).
- Turning `Progress` into a token bus (bodies stay out of Progress — D-S5).
- OpenTelemetry / full telemetry bus (P2 callbacks epic).
- Multiplexed multi-agent token merge UI framework.
- Changing structured-output validation to accept incomplete JSON mid-stream.

---

## 1. Baseline (code truth as of v0.6.0)

### 1.1 Core interfaces

```go
// llm.go
type LLM interface {
    Call(ctx context.Context, messages []Message) (string, error)
    Model() string
}

// toolcall.go — optional capability pattern (precedent for StreamingLLM)
type ToolCallingLLM interface {
    CallWithTools(ctx context.Context, messages []Message, tools []ToolSpec) (*ToolCallResponse, error)
}
```

`LLM` must stay minimal. Optional capabilities use **type assertion** in the executor (`ToolCallingLLM`, `WebSearcher`, planned `StreamingLLM`).

### 1.2 Where `Call` is used

| Path | File | Needs full string before continue? |
|---|---|---|
| ReAct loop (tools) | `executor.go` | **Yes** — parse `Final Answer:` / `Action:` after each turn |
| ReAct no-tools | `executor.go` | **No** for UX — result is the whole completion |
| Native tools | `toolcall.go` | **Yes** for tool_calls assembly; final text turn could stream |
| Structured capture | `structured.go` | **Yes** — JSON Schema validate needs complete object |
| AgenticLoop plan/eval/refine | `loop.go` | **Yes** for eval JSON; final execute path follows executeTaskDefault |
| Hierarchical manager pick | `crew.go` `delegate` | **Yes** — role name parse; short completion |

### 1.3 Provider HTTP today

| Provider | Endpoint | Stream flag today | Natural stream framing |
|---|---|---|---|
| `llm/openai` | `POST /chat/completions` | absent (`stream` default false) | SSE `data: {json}\n\n` + `[DONE]` |
| `llm/xai` | wraps openai | same | same (OpenAI-compatible) |
| `llm/anthropic` | `POST /messages` | absent | SSE `event:` + `data:` (`content_block_delta`, etc.) |
| `llm/ollama` | `POST /api/chat` | **`Stream: false` hardcoded** | NDJSON lines, `done: true` |
| `llm/mock` | in-process | n/a | synthetic chunking of `Responses` |

All non-stream paths use `io.ReadAll(io.LimitReader(body, MaxProviderResponseBytes))`. Stream paths must honor the **same byte ceiling on the accumulated text** (D-S8).

### 1.4 Progress contract (must not regress)

`Progress` **never** carries prompt bodies, LLM outputs, or tool inputs — metadata only. Streaming text delivery is a **separate** channel (D-S5).

---

## 2. Goals

1. **Opt-in streaming** for apps that want token/delta delivery without breaking any existing `LLM` implementer.
2. **One consumption API** in the root package so UIs do not depend on provider-specific SSE parsers.
3. **Executor integration** that streams when safe, and **falls back to `Call`** when the path requires a full string or the LLM lacks `StreamingLLM`.
4. **Provider coverage** for first-party clients (OpenAI, xAI-via-openai, Ollama, Anthropic) + mock for hermetic tests.
5. **Cancellation**: `ctx` cancel aborts the HTTP body read and closes the chunk stream.
6. **Zero new module dependencies**; stdlib `bufio` / `encoding/json` only.
7. **Backward compatible**: default behavior of `Kickoff` with non-streaming LLMs is bit-identical (no extra goroutines, no required callbacks).

---

## 3. Public API sketch

### 3.1 Chunk + optional interface (root package)

```go
// StreamChunk is one unit of a streaming completion.
// v1 is text-centric: providers map their wire deltas into Delta.
type StreamChunk struct {
    // Delta is the incremental text fragment (may be empty on control chunks).
    Delta string

    // Done is true on the terminal successful chunk. Delta may still hold
    // a final fragment. After a Done chunk the channel is closed.
    Done bool

    // Err is non-nil on a terminal failure chunk. Delta is empty.
    // After an Err chunk the channel is closed. Callers may also see the
    // same error as the error return of CollectStream / helper APIs.
    Err error
}

// StreamingLLM is an optional capability. The executor type-asserts it
// when a stream sink is configured (D-S1, D-S3). Implementers that only
// support Call remain valid forever.
type StreamingLLM interface {
    // CallStream starts a completion and returns a channel of chunks.
    // The channel is closed after the terminal Done or Err chunk, or when
    // ctx is cancelled. Implementers MUST:
    //   - respect ctx cancellation promptly;
    //   - not block forever if the caller stops receiving (prefer ctx);
    //   - enforce MaxProviderResponseBytes on accumulated text (D-S8);
    //   - be safe for concurrent Call/CallStream on the same client.
    // The returned channel MUST have a small buffer (e.g. 16) or be
    // paired with an internal producer goroutine that exits on ctx.Done.
    CallStream(ctx context.Context, messages []Message) <-chan StreamChunk
}
```

**Compile-time checks** in each provider (same style as `ToolCallingLLM`):

```go
var _ crewai.StreamingLLM = (*Client)(nil)
```

### 3.2 Helpers (root package)

```go
// CollectStream drains ch and returns the concatenated Delta text.
// Returns the first non-nil chunk.Err, or ctx.Err(), or nil.
func CollectStream(ctx context.Context, ch <-chan StreamChunk) (string, error)

// CallOrStream prefers StreamingLLM when sink != nil, otherwise Call.
// Always returns the full text for the executor; sink receives deltas.
func CallOrStream(ctx context.Context, llm LLM, messages []Message, sink StreamFunc) (string, error)

// StreamFunc is invoked for each non-empty Delta (and optionally Done).
// MUST be safe for use on the calling goroutine (executor does not add
// its own fan-out). Panics are recovered at the Crew boundary only if
// routed through emitStream (D-S5) — see below.
type StreamFunc func(StreamChunk)
```

`CollectStream` lets providers and tests share one drain path; the executor uses `CallOrStream` so non-streaming LLMs need zero changes.

### 3.3 How apps attach a sink (D-S3)

**Recommended v1: context-attached sink** (mirrors `ContextWithProgress`):

```go
type streamKey struct{}

// ContextWithStream attaches a StreamFunc to ctx. Nil is a no-op.
func ContextWithStream(ctx context.Context, fn StreamFunc) context.Context

// Crew.WithStream(fn) stores fn and injects it into Kickoff's ctx
// (same pattern as WithProgress).
func (c *Crew) WithStream(fn StreamFunc) *Crew
```

Rationale:
- No new field on every `Agent`/`Task` required for the common “UI wants tokens” case.
- Nested `executeTask` / agentic loop / delegation automatically see the same sink.
- When `fn == nil`, executor never type-asserts `StreamingLLM` for side-effect streaming (still may use Call only) — **zero behavior change**.

Alternative rejected for v1: per-call `Agent.StreamFunc` field only (easy to forget under Hierarchical manager agents). May add later as override.

### 3.4 What the sink receives

| Chunk | When | Notes |
|---|---|---|
| `{Delta: "Hel"}` | provider text delta | may be multi-rune; no guarantee of token boundaries |
| `{Delta: "lo", Done: true}` | last text + terminal | or separate empty `{Done: true}` — **D-S2 chooses one style**; recommendation: **allow final Delta on Done chunk** |
| `{Err: err}` | HTTP/decode/cap failure | channel then closes |

No `Role`, no tool-call partials, no token counts in v1 (keeps the type stable and redaction-simple).

---

## 4. Executor integration

### 4.1 Helper used everywhere text is final-ish

```go
func callLLM(ctx context.Context, llm LLM, messages []Message) (string, error) {
    return CallOrStream(ctx, llm, messages, streamFuncFromCtx(ctx))
}
```

Replace direct `a.LLM.Call` in paths listed below **only when** streaming is meaningful.

### 4.2 Path matrix (D-S4)

| Path | Stream deltas to sink? | Mechanism |
|---|---|---|
| ReAct **no tools** | **Yes** | `CallOrStream` |
| ReAct **with tools** (intermediate turns) | **No** (v1) | plain `Call` — need full text to parse Action/Final Answer; streaming partial thoughts is noisy and protocol-brittle |
| ReAct **with tools** (optional later) | deferred | only stream turns that already look like Final Answer — out of v1 |
| Native `CallWithTools` loop | **No** for tool rounds | keep `CallWithTools` |
| Native final text-only response | **Yes** if provider adds stream-with-tools later | **not v1** unless cheap; default No |
| Structured output (all phases) | **No** | validation needs complete JSON; repair loop too |
| AgenticLoop plan / evaluate / refine | **No** | short control completions |
| AgenticLoop **execute** sub-step | follows `executeTaskDefault` | so no-tools execute **can** stream |
| Hierarchical `delegate` | **No** | tiny completion; avoid UI spam |

**Summary v1:** stream the **user-visible final answer path** that is already a single full-text `Call` (primarily no-tools ReAct / no-tools execute). Everything protocol-driven stays buffered.

### 4.3 Fallback (D-S7)

```go
func CallOrStream(ctx context.Context, llm LLM, messages []Message, sink StreamFunc) (string, error) {
    if sink != nil {
        if s, ok := llm.(StreamingLLM); ok {
            ch := s.CallStream(ctx, messages)
            return drainToSink(ctx, ch, sink)
        }
    }
    // Non-streaming LLM or no sink: one-shot Call.
    // Optional courtesy: if sink != nil, emit full text as one Delta+Done
    // so UIs still update once (D-S7-B). Decision: **yes, emit one chunk**.
    out, err := llm.Call(ctx, messages)
    if err != nil {
        if sink != nil {
            sink(StreamChunk{Err: err})
        }
        return "", err
    }
    if sink != nil {
        sink(StreamChunk{Delta: out, Done: true})
    }
    return out, nil
}
```

### 4.4 Interaction with existing features

| Feature | Interaction |
|---|---|
| **Guardrails** | run on final string after stream completes (unchanged) |
| **Memory AutoSave** | saves final string (unchanged); D-M7 unaffected |
| **Facts** | tool path only; unchanged |
| **Progress** | still metadata; optional `Event: "llm_stream_started" / "llm_stream_completed"` **without** body (D-S5-A default: **skip** new events in v1 to avoid spam; apps use StreamFunc timing) |
| **Verbose / slog** | Debug may log final assembled text only (not every delta) to avoid log floods + secret sprawl |
| **Async waves** | each task may stream concurrently → **StreamFunc MUST be concurrency-safe** (document like ProgressFunc) |
| **Structured** | no stream in v1 |
| **MaxIterations / cancel** | `ctx` cancel closes stream mid-flight |

---

## 5. Provider implementation notes

### 5.1 Shared discipline

1. Request with stream enabled; do **not** `ReadAll` the body.
2. Scan frames (SSE or NDJSON); map to `StreamChunk{Delta}`.
3. Accumulate text in a `strings.Builder`; if `builder.Len() > MaxProviderResponseBytes`, send `{Err: …}`, cancel body, return.
4. On clean EOF / provider done: send `{Done: true}` (with or without final delta), close channel.
5. Producer runs in a goroutine; exit on `ctx.Done()` and close channel.
6. Never log full deltas at Info; Debug optional and behind existing redaction guidance.
7. Errors from provider JSON (`error` object mid-stream) → `{Err}` chunk.

### 5.2 OpenAI / xAI

- Set `"stream": true` on chat completions.
- Read SSE: lines `data: …`; skip comments; terminal `data: [DONE]`.
- Parse `choices[0].delta.content` (string fragment).
- xAI: inherits via `inner *openai.Client` once openai implements `CallStream`.

### 5.3 Ollama

- Set `"stream": true` (today hardcoded false).
- NDJSON: each line is a `chatResponse`; append `message.content`; terminal when `done == true`.

### 5.4 Anthropic

- `POST /messages` with `"stream": true`.
- SSE events: `content_block_delta` → `delta.text`; `message_stop` → Done.
- Ignore `ping` / non-text blocks in v1.
- **Coverage note:** `llm/anthropic` historically sat near the 90% line — any PR that touches it must leave package ≥ 90% (security-residuals gate).

### 5.5 Mock

```go
// StreamChunks optionally supplies per-CallStream sequences.
// If nil, CallStream splits Call's string into fixed-size runes (e.g. 4)
// so executor tests can assert sink ordering without network.
func (m *LLM) CallStream(ctx context.Context, messages []Message) <-chan crewai.StreamChunk
```

Also record stream calls in the existing call log for assertions.

---

## 6. File changes (expected)

| Area | Files |
|---|---|
| API | `llm.go` or new `stream.go` (`StreamChunk`, `StreamingLLM`, `CollectStream`, `CallOrStream`, `ContextWithStream`) |
| Crew | `crew.go` — `WithStream`, inject ctx in `Kickoff` |
| Executor | `executor.go` — no-tools path via `CallOrStream`; leave tool loop on `Call` |
| Providers | `llm/openai`, `llm/ollama`, `llm/anthropic`, `llm/xai` (delegate), `llm/mock` |
| Tests | `stream_test.go`, provider `*_stream_test.go` with `httptest` SSE/NDJSON fixtures |
| Docs | `docs/llms.md` + pt-BR, `doc.go`, README What's new (when releasing), examples |
| Example | `examples/streaming` offline mock sinking to `os.Stdout` |
| CHANGELOG | EN + PT under Unreleased / release section |

No new packages required. Prefer `stream.go` in root to keep `llm.go` readable.

---

## 7. Security & safety

| Topic | Rule |
|---|---|
| **Byte cap** | Accumulated stream text ≤ `MaxProviderResponseBytes` (same as non-stream ReadAll) |
| **Secrets in deltas** | StreamFunc receives raw model text (may include echoed secrets). Docs: do not ship raw deltas to untrusted multi-tenant clients without the app's own filter; prefer `RedactHandler` only affects slog, **not** StreamFunc |
| **Progress purity** | Do not put Delta into `Progress` |
| **Error redaction** | `chunk.Err` messages passed to StreamFunc should use the same `redactError` posture as Progress when the error originates from provider HTTP layers (providers already avoid echoing bodies on bad status) |
| **Cancellation** | `ctx` cancel must close HTTP body and channel; no goroutine leak (tested with `go test -race` + leak-minded asserts) |
| **Slow consumer** | If sink blocks, producer may block on channel send — document that StreamFunc must be fast or app-buffer; channel buffer ≥ 16; ctx cancel is the escape hatch |
| **SSRF / auth** | unchanged; streaming uses same endpoints and auth as `Call` |

---

## 8. Tests & gates

### 8.1 Unit / hermetic

- `CollectStream` concatenates deltas; propagates Err; respects ctx cancel.
- `CallOrStream` with non-StreamingLLM + sink → single Delta+Done.
- `CallOrStream` with StreamingLLM + nil sink → still may Call only (no assert required) **or** CallStream without sink — **D-S9: if sink nil, always `Call`** (avoid extra goroutines).
- Executor no-tools: sink receives ordered deltas; final task output equals concat.
- Executor with tools: sink **not** called for intermediate turns (v1).
- Mock stream + Kickoff + `WithStream` under Sequential and one Async wave (concurrency-safe sink via mutex).
- Provider httptest: OpenAI SSE fixture; Ollama NDJSON; Anthropic SSE; cap exceeded → Err chunk.
- Cancel mid-stream: channel closes; `Kickoff` returns `context.Canceled` / deadline.

### 8.2 Quality gates (every PR)

Same as security-residuals §6.1:

| Gate | Rule |
|---|---|
| Coverage | touched packages ≥ 90% |
| Race | `go test -race` clean on touched packages / full tree before release |
| Docs | EN + PT-BR in the same PR |
| CHANGELOG | EN + PT |
| Deps | zero new `go.mod` requires |
| gofmt / vet | clean |

### 8.3 Acceptance (epic done)

- [ ] `StreamingLLM` + `StreamChunk` + `WithStream` / `ContextWithStream` public and godoc'd
- [ ] No-tools path streams when sink set and LLM implements `StreamingLLM`
- [ ] Fallback single-chunk behavior for non-streaming LLMs
- [ ] openai + ollama + anthropic + xai (delegate) + mock implement `CallStream`
- [ ] `examples/streaming` offline
- [ ] docs/llms.md + pt-BR + doc.go; PLAN.md checkbox
- [ ] race-clean; coverage gates; CHANGELOG
- [ ] Tag note: ship as **v0.7.0** (feature release) unless bundled with more P1 work

---

## 9. Decisions

### 9.1 Closed by this plan (defaults — confirm before Phase 1 code)

| ID | Question | Options | Decision |
|---|---|---|---|
| **D-S1** | API shape | (A) add `CallStream` to `LLM` (B) optional `StreamingLLM` type assert | **B** — matches PLAN §7; zero break for external implementers |
| **D-S2** | Terminal chunk style | (A) final delta may ride on `{Done:true}` (B) always empty `{Done:true}` after last delta | **A** — fewer channel ops; CollectStream treats both |
| **D-S3** | App sink attachment | (A) ctx + `Crew.WithStream` (B) only `Agent` field (C) callback on `LLM` | **A** — mirrors Progress; hierarchy-friendly |
| **D-S4** | Which executor paths stream in v1 | (A) all Call sites (B) final text / no-tools only (C) everything except structured | **B** — safe + high UX value; protocol paths stay buffered |
| **D-S5** | Progress vs stream | (A) no new Progress events (B) stream_started/completed metadata | **A** for v1 (avoid event spam); revisit in P2 telemetry |
| **D-S6** | Provider order | (A) all four in one PR (B) mock+openai first, then ollama, anthropic | **B** — smaller PRs; xai free via openai inner |
| **D-S7** | Non-streaming LLM + sink set | (A) silent no deltas (B) one Delta+Done with full text | **B** — UIs always get content |
| **D-S8** | Size limit | (A) new higher stream cap (B) reuse `MaxProviderResponseBytes` | **B** — one policy |
| **D-S9** | sink == nil | (A) still CallStream if available (B) always Call | **B** — no surprise goroutines / scheduling changes |
| **D-S10** | Partial tool-call streaming | (A) v1 scope (B) deferred | **B** — separate decision when native stream-with-tools is designed |
| **D-S11** | Channel buffer size | (A) unbuffered (B) 16 (C) 64 | **B** — 16 |
| **D-S12** | StreamFunc panic | (A) crash Kickoff (B) recover + slog like Progress | **B** — recover in `emitStream`; Kickoff continues assembling text |

### 9.2 Open until implementation spikes (non-blocking for plan merge)

| ID | Topic | Notes |
|---|---|---|
| **O-S1** | Anthropic tool-result message mapping under stream | Only if we later stream native paths |
| **O-S2** | Whether `CallStream` should share transport helpers with `Call` | Refactor opportunity inside each provider; no API impact |
| **O-S3** | Expose `CollectStream` as part of stable API vs internal | Recommendation: **public** — apps building custom executors need it |

---

## 10. PR / implementation order

```
Phase 0  Close D-S1–D-S12 on this document (this PR / review)
Phase 1  S1: stream.go API + CollectStream/CallOrStream + tests (no provider)
Phase 2  S2: llm/mock CallStream + executor no-tools wiring + Crew.WithStream
Phase 3  S3: llm/openai CallStream (httptest SSE) + xai compile-time assert via inner
Phase 4  S4: llm/ollama CallStream (NDJSON)
Phase 5  S5: llm/anthropic CallStream (SSE) + coverage ≥ 90%
Phase 6  S6: examples/streaming + docs EN/PT + doc.go + CHANGELOG
Phase 7  S7: release hygiene → tag v0.7.0 (or bundle)
```

**Dependency graph:** S1 → S2 → (S3 ∥ S4 ∥ S5) → S6 → S7.  
S3 before S5 is fine; S4 independent of S3 after S2.

**Do not** start provider work before S1 API is merged (avoids thrashing chunk shape).

---

## 11. Documentation plan

| Doc | Update |
|---|---|
| `docs/llms.md` + `docs/pt-BR/llms.md` | New “Streaming” section: WithStream, StreamingLLM, fallback, concurrency, security note on raw deltas |
| `doc.go` | Package overview paragraph + minimal example |
| `README.md` / `README.pt-BR.md` | Why bullet + What's new when releasing |
| `docs/crews.md` | `WithStream` field/method next to `WithProgress` |
| `examples/streaming` | Offline mock printing deltas |
| `PLAN.md` / `PLAN.pt-BR.md` | Link this plan; check Streaming when shipped |
| `SECURITY.md` | One line: stream sinks receive raw model text (app responsibility) |

---

## 12. Versioning & compatibility

- **Semver:** new APIs are additive → **minor** (v0.7.0 while in v0.x).
- **No change** to `LLM` interface method set.
- External LLMs that only implement `Call` keep working; with `WithStream`, they get D-S7 single-chunk behavior.
- Golden tests for non-stream Kickoff must remain identical when `WithStream` is nil.

---

## 13. Out of scope / later

| Item | Where |
|---|---|
| Stream native tool-call argument deltas | Follow-up after D-S10 reopen |
| Stream structured JSON with incremental parse | Unlikely; validate-at-end is enough |
| Progress token events / OTEL spans per delta | P2 callbacks/telemetry epic |
| Server-Sent Events helper for apps exposing Kickoff over HTTP | Example pattern only, not core |
| Token usage / `finish_reason` on StreamChunk | Additive fields later if needed (keep zero-value safe) |

---

## 14. Summary

Ship **optional `StreamingLLM`** + **`Crew.WithStream` / context sink**, reuse the **`ToolCallingLLM` type-assert pattern**, stream **final text paths only** in v1, **fallback to one-shot Call** (with single chunk if sink set), implement **mock → openai/xai → ollama → anthropic**, keep **Progress body-free** and **stdlib-only**.

When decisions D-S1–D-S12 are acknowledged, Phase 1 coding can start on a branch such as `feat/streaming-s1-api`.
