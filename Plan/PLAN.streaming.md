# Plan — Streaming LLM tokens

> **Status:** **Design** (not started). Next product epic after v0.6.0 (memory + async).  
> **Decisions:** D-S1–D-S14 closed by design review 2026-08-21 (§9) — ack before Phase 1 code.  
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

All non-stream paths use `io.ReadAll(io.LimitReader(body, MaxProviderResponseBytes))`. Stream paths must honor the **same ceiling on accumulated text and on raw body bytes read** (D-S8).

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
// v1 is text-centric: providers map their wire deltas into Delta only.
// Task/Agent are filled by the executor wrapper (D-S13), never by providers.
type StreamChunk struct {
    // Delta is the incremental text fragment (may be empty on control chunks).
    // Providers SHOULD omit empty/null wire deltas (no empty-Delta spam).
    Delta string

    // Task is the task label (Name or "Task N"). Set by the executor when
    // delivering to the app sink so concurrent Async waves can be demuxed.
    // Empty when CallStream is used outside a task (tests / CollectStream).
    Task string

    // Agent is the agent role for the in-flight task. Same rules as Task.
    Agent string

    // Done is true on the terminal successful chunk. Delta may still hold
    // a final fragment (D-S2-A). After a Done chunk the channel is closed.
    // Done and Err MUST NOT both be set (D-S14); CollectStream prioritizes Err.
    Done bool

    // Err is non-nil on a terminal failure chunk. Delta is empty.
    // After an Err chunk the channel is closed. Callers may also see the
    // same error as the error return of CollectStream / helper APIs.
    Err error
}

// StreamingLLM is an optional capability (same pattern as ToolCallingLLM:
// embeds LLM so Model/Call remain available on the asserted value).
// The executor type-asserts it when a stream sink is configured (D-S1, D-S3).
// Implementers that only support Call remain valid forever.
type StreamingLLM interface {
    LLM
    // CallStream starts a completion and returns a channel of chunks.
    // The channel is NEVER nil (D-S14). Setup failures (marshal, auth, dial)
    // are delivered as the first chunk {Err: ...} then the channel is closed.
    // The channel is closed after the terminal Done or Err chunk, or when
    // ctx is cancelled (producer exits; CollectStream maps bare close to
    // ctx.Err() or ErrStreamIncomplete). Implementers MUST:
    //   - respect ctx cancellation promptly;
    //   - not block forever if the caller stops receiving (prefer ctx);
    //   - enforce MaxProviderResponseBytes on accumulated text AND on
    //     raw HTTP body bytes read (D-S8); use ErrStreamResponseTooLarge;
    //   - be safe for concurrent Call/CallStream on the same client;
    //   - leave Task/Agent empty (executor fills them).
    // The returned channel MUST have a small buffer (DefaultStreamChanBuffer
    // = 16, D-S11) or be paired with an internal producer that exits on
    // ctx.Done.
    CallStream(ctx context.Context, messages []Message) <-chan StreamChunk
}

// Sentinel errors (errors.go):
//   ErrStreamIncomplete       — channel closed without Done/Err and ctx OK
//   ErrStreamResponseTooLarge — text or body bytes exceeded MaxProviderResponseBytes
```

**Compile-time checks** in each provider (same style as `ToolCallingLLM`):

```go
var _ crewai.StreamingLLM = (*Client)(nil)
```

### 3.2 Helpers (root package)

```go
// CollectStream drains ch and returns the concatenated Delta text.
// Public stable API (O-S3 closed). Returns the first non-nil chunk.Err,
// ctx.Err() if ctx is done when the channel closes without terminal chunk,
// ErrStreamIncomplete if the channel closes with neither Done nor Err while
// ctx is still OK, or nil on clean Done. Ignores Task/Agent for concat.
func CollectStream(ctx context.Context, ch <-chan StreamChunk) (string, error)

// CallOrStream prefers StreamingLLM when sink != nil, otherwise Call.
// Always returns the full text for the executor; sink receives deltas.
// All sink invocations go through emitStream (panic recover, D-S12).
func CallOrStream(ctx context.Context, llm LLM, messages []Message, sink StreamFunc) (string, error)

// StreamFunc is invoked for each chunk delivered to the app (deltas, Done,
// Err). MUST be safe for concurrent use when Async waves / Staged stages
// run in parallel (same contract as ProgressFunc). Set via WithStream
// BEFORE Kickoff. Panics are recovered in emitStream (D-S12).
type StreamFunc func(StreamChunk)
```

`CollectStream` is **public** so apps and custom executors share one drain path; the crew executor uses `CallOrStream` so non-streaming LLMs need zero changes.

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
| `{Delta: "Hel", Task, Agent}` | provider text delta | may be multi-rune; no token-boundary guarantee; Task/Agent from executor (D-S13) |
| `{Delta: "lo", Done: true, Task, Agent}` | last text + terminal | final Delta may ride on Done (D-S2-A) |
| `{Err: err, Task, Agent}` | HTTP/decode/cap/setup failure | channel then closes; Done unset (D-S14) |

No `Role`, no tool-call partials, no token counts in v1 (keeps the type stable and redaction-simple). Empty/null provider deltas are not forwarded. AgenticLoop refine may emit **multiple** Done sequences for the same Task/Agent (one per execute round) — apps key off Done boundaries; optional `Phase` field is out of v1.

---

## 4. Executor integration

### 4.1 Helper — only on matrix **Yes** paths

```go
// callLLMText streams when the path matrix allows it. Do NOT use as a
// global drop-in for every a.LLM.Call site (D-S4).
func callLLMText(ctx context.Context, llm LLM, messages []Message, task *Task, agent *Agent) (string, error) {
    sink := streamFuncFromCtx(ctx)
    if sink != nil {
        sink = withTaskAgent(sink, taskLabel(task), agentRole(agent)) // D-S13
    }
    return CallOrStream(ctx, llm, messages, sink)
}
```

**Hard rule:** replace `a.LLM.Call` **only** where §4.2 says **Yes**. Structured, ReAct+tools intermediate turns, plan/eval/refine/rewrite, and hierarchical `delegate` stay on plain `Call`.

### 4.2 Path matrix (D-S4)

| Path | Stream deltas to sink? | Mechanism |
|---|---|---|
| ReAct **no tools** | **Yes** | `callLLMText` / `CallOrStream` |
| ReAct **with tools** (all turns) | **No** (v1) | plain `Call` — need full text to parse Action/Final Answer |
| ReAct **with tools** (stream Final Answer only) | deferred | out of v1 |
| Native **no tools** (`ToolModeNative` + empty tool list) | **Yes** | same plain-`Call` fallthrough in `toolcall.go` → `callLLMText` |
| Native `CallWithTools` rounds (including final text-only response) | **No** (v1) | keep buffered `CallWithTools` (D-S10) |
| Structured output (all phases) | **No** | validation needs complete JSON; repair loop too |
| AgenticLoop plan / evaluate / refine / rewrite | **No** | short control completions |
| AgenticLoop **execute** sub-step | follows `executeTaskDefault` | no-tools execute **can** stream (may Done multiple times across refine rounds) |
| Hierarchical `delegate` | **No** | tiny completion; avoid UI spam |

**Summary v1:** stream the **user-visible final answer path** that is already a single full-text `Call` — ReAct no-tools, native no-tools, and agentic execute when those apply. Everything protocol-driven stays buffered.

### 4.3 Fallback (D-S7)

```go
func CallOrStream(ctx context.Context, llm LLM, messages []Message, sink StreamFunc) (string, error) {
    if sink != nil {
        if s, ok := llm.(StreamingLLM); ok {
            ch := s.CallStream(ctx, messages) // never nil (D-S14)
            return drainToSink(ctx, ch, sink)   // emitStream per chunk; concat Deltas
        }
    }
    // Non-streaming LLM or no sink: one-shot Call.
    // D-S7-B: if sink != nil, emit full text as one Delta+Done via emitStream.
    out, err := llm.Call(ctx, messages)
    if err != nil {
        if sink != nil {
            emitStream(ctx, sink, StreamChunk{Err: err})
        }
        return "", err
    }
    if sink != nil {
        emitStream(ctx, sink, StreamChunk{Delta: out, Done: true})
    }
    return out, nil
}
```

`withTaskAgent` copies Task/Agent onto every chunk before `emitStream` so providers stay metadata-free (D-S13).

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
2. Count **raw body bytes read** (counter or `io.LimitedReader`); if exceeded → `{Err: ErrStreamResponseTooLarge}`, close body, close channel (D-S8).
3. Scan frames (SSE or NDJSON); map text fragments to `StreamChunk{Delta}` only (Task/Agent empty). Skip null/empty wire deltas.
4. Accumulate text in a `strings.Builder`; if `builder.Len() > MaxProviderResponseBytes`, same too-large error path.
5. On clean EOF / provider done: send `{Done: true}` (with or without final delta), close channel. Never set Done and Err together.
6. Producer runs in a goroutine; exit on `ctx.Done()` and close channel (no further sends).
7. Setup failures before the producer starts: still return a non-nil channel that immediately yields `{Err}` (D-S14).
8. Never log full deltas at Info; Debug optional and behind existing redaction guidance.
9. Errors from provider JSON (`error` object mid-stream) → `{Err}` chunk.

### 5.2 OpenAI / xAI

- Set `"stream": true` on chat completions.
- Read SSE: lines `data: …`; skip comments; terminal `data: [DONE]`.
- Parse `choices[0].delta.content` (string fragment).
- xAI: thin `CallStream` delegates to `inner.CallStream` plus
  `var _ crewai.StreamingLLM = (*Client)(nil)` in the xai package (same as Call).
- Ignore `delta.content == null` / omitted; do not emit empty Delta chunks.

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
| **Byte cap** | Accumulated **text** ≤ `MaxProviderResponseBytes` **and** raw **body bytes read** ≤ same cap (D-S8). Prevents JSON-frame DoS with empty deltas. Sentinel `ErrStreamResponseTooLarge`. |
| **Secrets in deltas** | StreamFunc receives raw model text (may include echoed secrets). Docs: do not ship raw deltas to untrusted multi-tenant clients without the app's own filter; prefer `RedactHandler` only affects slog, **not** StreamFunc |
| **Progress purity** | Do not put Delta into `Progress` |
| **Error redaction** | `chunk.Err` messages passed to StreamFunc should use the same `redactError` posture as Progress when the error originates from provider HTTP layers (providers already avoid echoing bodies on bad status) |
| **Cancellation** | `ctx` cancel must close HTTP body and channel; no goroutine leak (tested with `go test -race` + leak-minded asserts) |
| **Slow consumer** | If sink blocks, producer may block on channel send — document that StreamFunc must be fast or app-buffer; channel buffer ≥ 16; ctx cancel is the escape hatch |
| **SSRF / auth** | unchanged; streaming uses same endpoints and auth as `Call` |

---

## 8. Tests & gates

### 8.1 Unit / hermetic

- `CollectStream` concatenates deltas; propagates Err; respects ctx cancel; bare close → `ErrStreamIncomplete` or `ctx.Err()`.
- `CallOrStream` with non-StreamingLLM + sink → single Delta+Done via `emitStream`.
- `CallOrStream` with sink nil → always `Call` (D-S9).
- `CallStream` setup failure → non-nil chan, first chunk Err (D-S14).
- Executor ReAct no-tools **and** native no-tools: sink receives ordered deltas with Task/Agent set; final task output equals concat.
- Executor with tools: sink **not** called for intermediate turns (v1).
- Async wave: two concurrent tasks → sink sees distinct Task labels (D-S13); sink protected by mutex in test.
- StreamFunc panic recovered; Kickoff still returns assembled text (D-S12).
- Provider httptest: OpenAI SSE (incl. null content); Ollama NDJSON; Anthropic SSE; text cap **and** body-byte cap → `ErrStreamResponseTooLarge`.
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

- [ ] `StreamingLLM` (embeds `LLM`) + `StreamChunk` (Delta/Task/Agent/Done/Err) + `WithStream` / `ContextWithStream` + public `CollectStream` godoc'd
- [ ] ReAct no-tools **and** native no-tools stream when sink set and LLM implements `StreamingLLM`
- [ ] Fallback single-chunk behavior for non-streaming LLMs; setup/incomplete/too-large sentinels
- [ ] openai + ollama + anthropic + xai (`CallStream` delegate) + mock implement `CallStream`
- [ ] `examples/streaming` offline (include dual Async demux of Task labels)
- [ ] docs/llms.md + pt-BR + doc.go + SECURITY note; PLAN.md checkbox
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
| **D-S8** | Size limit | (A) new higher stream cap (B) reuse `MaxProviderResponseBytes` on **text only** (C) same cap on **text and raw body bytes read** | **C** — one constant, two meters; closes JSON-frame DoS with empty deltas |
| **D-S9** | sink == nil | (A) still CallStream if available (B) always Call | **B** — no surprise goroutines / scheduling changes |
| **D-S10** | Partial tool-call streaming | (A) v1 scope (B) deferred | **B** — separate decision when native stream-with-tools is designed |
| **D-S11** | Channel buffer size | (A) unbuffered (B) 16 (C) 64 | **B** — 16 (`DefaultStreamChanBuffer`) |
| **D-S12** | StreamFunc panic | (A) crash Kickoff (B) recover + slog like Progress | **B** — recover in `emitStream` (all sink paths incl. D-S7 fallback); Kickoff continues assembling text |
| **D-S13** | Concurrent-task demux | (A) global sink, deltas only (B) Task/Agent on chunk filled by executor (C) per-task sinks API) | **B** — providers stay text-only; executor `withTaskAgent` wrapper |
| **D-S14** | CallStream error / channel contract | (A) `(<-chan, error)` setup return (B) chan never nil; setup → first `{Err}`; bare close → `ErrStreamIncomplete` / `ctx.Err()`; forbid Done∧Err | **B** — one consumption shape; sentinels in `errors.go` |

### 9.2 Open until implementation spikes (non-blocking for Phase 1)

| ID | Topic | Notes |
|---|---|---|
| **O-S1** | Anthropic tool-result message mapping under stream | Only if we later stream native paths |
| **O-S2** | Whether `CallStream` should share transport helpers with `Call` | Refactor opportunity inside each provider; no API impact |

~~**O-S3** `CollectStream` public vs internal~~ — **closed: public**.

---

## 10. PR / implementation order

```
Phase 0  ~~Close D-S1–D-S14~~ **done in design review 2026-08-21** (this patch)
Phase 1  S1: stream.go API + CollectStream/CallOrStream/emitStream + sentinels + tests (no provider)
Phase 2  S2: llm/mock CallStream + executor ReAct **and** native no-tools + Crew.WithStream + D-S13 wrap
Phase 3  S3: llm/openai CallStream (httptest SSE) + xai CallStream delegate + compile-time assert
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
| `examples/streaming` | Offline mock printing deltas; dual `WithAsync` tasks show Task demux |
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

Ship **optional `StreamingLLM` (embeds `LLM`)** + **`Crew.WithStream` / context sink**, reuse the **`ToolCallingLLM` type-assert pattern**, stream **ReAct no-tools + native no-tools** in v1 (not protocol paths), **fallback to one-shot Call** (single chunk if sink set), **Task/Agent demux on chunks**, **non-nil channel + setup-as-Err**, **text and body byte caps**, implement **mock → openai/xai → ollama → anthropic**, keep **Progress body-free** and **stdlib-only**.

Decisions **D-S1–D-S14** are closed in this document. Phase 1 coding can start on a branch such as `feat/streaming-s1-api` after product ack (or proceed if no objections).
