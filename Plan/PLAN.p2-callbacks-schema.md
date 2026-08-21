# Plan — P2: Callbacks / Telemetry & JSON Schema remainder

> **Status:** **Design** (not started). Next product epic after v0.7.0 (Streaming).  
> **Decisions:** D-C1–D-C10 (callbacks) and D-J1–D-J12 (JSON Schema) proposed below (§9); close before Phase 1 code.  
> **Related:** `progress.go`, `stream.go` (emit patterns), `executor.go` / `toolcall.go` / `loop.go` / `crew.go`, `schema.go` / `structured.go`, roadmap `PLAN.md` §6 P2.  
> **Constraints:** zero external module dependencies in the core library (`go.mod` stays stdlib-only). Quality gates from §6.1 of `PLAN.security-residuals.md` apply to every implementation PR (coverage ≥ 90% on touched packages, race-clean, docs EN+PT-BR, CHANGELOG).  
> **Non-goals of sibling epics:** Flows (P3), YAML crews (P3), HTTP/file/RAG tools (P3), training (P3), M5/A5, OpenTelemetry SDK in core.

---

## 0. Why these two together

| Capability | Today (v0.7.0) | Gap |
|---|---|---|
| **Lifecycle observability** | `WithProgress` emits stage/task/tool metadata only; `slog` is free-form; `WithStream` is text deltas | No first-class hooks for ReAct iteration, LLM call boundaries, structured-output repair rounds, or exportable event records |
| **JSON Schema** | Stdlib subset: type/properties/required/enum/items/additionalProperties/bounds/pattern/oneOf|anyOf|allOf; `WithStrictSchema` rejects `$ref`, `format`, `if`/`then`/`else`, `const`, `unevaluated*`, … | Real-world schemas (OpenAPI fragments, MCP tool params, public JSON Schema docs) often need local `$ref` + a few formats; repair quality suffers when keywords are silently ignored |

They are independent **workstreams** in one P2 epic so the roadmap checkbox closes cleanly, with **separate PR trains** that never block each other.

**Non-goals (this plan):**
- Bundling OpenTelemetry, Prometheus, or any third-party telemetry SDK in core.
- Putting prompt bodies / full LLM outputs into default Progress-style events (bodies stay opt-in and redacted — D-C4).
- Full JSON Schema draft 2020-12 / dynamic remote `$ref` over the network.
- Replacing `Progress` or `StreamFunc` APIs (additive only).
- Schema-driven codegen or OpenAPI client generation.

---

## 1. Baseline (code truth as of v0.7.0)

### 1.1 Progress today

```go
type ProgressFunc func(Progress)
type Progress struct {
    Stage, Task, Agent, Event, Tool string
    Duration time.Duration
    Err error // redacted on task_completed failure
}
// Events: stage_started|completed, task_started|completed, tool_invoked
```

- Attached via `Crew.WithProgress` → `ContextWithProgress` at Kickoff.
- `emitProgress` recovers panics; concurrent-safe callback required.
- **Never** carries prompt/LLM/tool bodies (SECURITY contract).

### 1.2 Stream today (adjacent, not this epic)

`WithStream` / `StreamChunk` deliver **raw model text** on no-tools paths. Telemetry must **not** re-route stream deltas through Progress (D-S5 already locked).

### 1.3 Schema validator today

| Supported | Explicitly unsupported (StrictSchema fails) |
|---|---|
| `type`, `properties`, `required`, `enum`, `items` | `$ref` |
| `additionalProperties` (bool or schema) | `if` / `then` / `else` |
| string/number/array bounds, `pattern` (cap 512) | `format`, `const` |
| `oneOf` / `anyOf` / `allOf` | `unevaluatedProperties` / `unevaluatedItems` |
| Meta ignored: `$schema`, `$id`, `title`, `description`, `default`, `examples`, `definitions`, `$defs` | `not`, `dependent*`, `prefixItems`, `contains`, `propertyNames`, `min/maxProperties`, `uniqueItems`, … |

`definitions` / `$defs` are **parsed as nested maps for walk** but **`$ref` is not resolved** — so `$defs` is dead weight until D-J1.

`NewStructuredOutput(..., WithStrictSchema())` calls `checkSchemaSupported` and rejects unsupported keywords at construction.

### 1.4 Where hooks would fire

| Site | File | Candidate events |
|---|---|---|
| Task start/end | `crew.go` execute | already Progress `task_*` |
| Stage/wave start/end | `crew.go` | Progress `stage_*`; optional `wave_*` |
| ReAct iteration | `executor.go` | **missing** `react_iteration` / `llm_call` |
| Native tool round | `toolcall.go` | Progress `tool_invoked`; missing round index |
| Structured repair | `structured.go` | **missing** `structured_repair` |
| AgenticLoop plan/eval/refine | `loop.go` | **missing** `loop_phase` |
| Guardrail block | `guardrail.go` | **missing** `guardrail_blocked` |
| Memory barrier commit | `memory_policy.go` | optional later (out of v1 telemetry) |

---

## 2. Goals

### Part C — Callbacks / telemetry

1. **Richer lifecycle events** without breaking `Progress` / `WithProgress`.
2. **Exportable structured records** (JSON-serializable) for apps to ship to logs/metrics backends.
3. **Metadata-safe by default**; optional detail tiers that never dump secrets carelessly.
4. **Same concurrency / panic contracts** as Progress and Stream sinks.
5. **Zero OTEL dependency** in core; document how to bridge from events → OTEL in examples.

### Part J — JSON Schema remainder

1. **Local `$ref` resolution** against the root document (`#/...`, `#/$defs/...`, `#/definitions/...`).
2. **Practical `format` subset** with clear validation semantics.
3. **`const`**, and a **minimal useful** slice of remaining keywords prioritized by real schemas.
4. **`if`/`then`/`else`** when tractable without a full annotation model.
5. **`unevaluatedProperties` / `unevaluatedItems`** only if a correct-enough implementation fits stdlib scope — otherwise stay Strict-fail with a documented follow-up.
6. Preserve **fail-closed StrictSchema** for anything still unsupported.
7. No network `$ref`; no new deps.

---

## 3. Part C — Public API sketch

### 3.1 Event model (additive)

```go
// CrewEvent is an exportable lifecycle record. Safe for JSON encoding.
// Default fields are metadata-only (D-C4).
type CrewEvent struct {
    // Type is a stable string enum (see Event* constants).
    Type string `json:"type"`

    // Time is when the library emitted the event (UTC).
    Time time.Time `json:"time"`

    // KickoffID correlates all events from one Kickoff (D-C6).
    KickoffID string `json:"kickoff_id,omitempty"`

    Stage string `json:"stage,omitempty"`
    Task  string `json:"task,omitempty"`
    Agent string `json:"agent,omitempty"`

    // Iteration is the ReAct / native / repair loop index when applicable (0-based).
    Iteration int `json:"iteration,omitempty"`

    // Phase distinguishes agentic-loop steps: plan|execute|evaluate|refine|rewrite.
    Phase string `json:"phase,omitempty"`

    Tool string `json:"tool,omitempty"`

    // DurationMs is wall time for the span when Type ends a timed unit.
    DurationMs int64 `json:"duration_ms,omitempty"`

    // Err is a redacted error string when Type represents failure.
    // Empty on success. Never a raw provider body.
    Err string `json:"err,omitempty"`

    // Attrs holds small scalar metadata (counts, finish reasons, schema path).
    // Values MUST be bool/int/float64/string; no nested maps in v1.
    Attrs map[string]any `json:"attrs,omitempty"`
}

// Stable type strings (extend carefully — additive only).
const (
    EventKickoffStarted   = "kickoff_started"
    EventKickoffCompleted = "kickoff_completed"
    EventStageStarted     = "stage_started"
    EventStageCompleted   = "stage_completed"
    EventWaveStarted      = "wave_started"    // optional D-C8 default on
    EventWaveCompleted    = "wave_completed"
    EventTaskStarted      = "task_started"
    EventTaskCompleted    = "task_completed"
    EventToolInvoked      = "tool_invoked"
    EventLLMCallStarted   = "llm_call_started"   // NEW
    EventLLMCallCompleted = "llm_call_completed" // NEW
    EventReactIteration   = "react_iteration"    // NEW (or start/end pair — D-C3)
    EventStructuredRepair = "structured_repair"  // NEW
    EventLoopPhase        = "loop_phase"         // NEW
    EventGuardrailBlocked = "guardrail_blocked"  // NEW
)

// EventFunc receives CrewEvents. Concurrent-safe; panics recovered.
type EventFunc func(CrewEvent)

// Crew.WithEvents(fn) injects EventFunc into Kickoff ctx (mirrors WithProgress).
func (c *Crew) WithEvents(fn EventFunc) *Crew
func ContextWithEvents(ctx context.Context, fn EventFunc) context.Context
```

### 3.2 Bridge Progress → Events (D-C2)

**Recommendation:** keep emitting `Progress` unchanged for back-compat.  
Additionally, when `EventFunc` is set, emit the corresponding `CrewEvent`.  
Optional helper for apps that only want one sink:

```go
// ProgressAsEvents wraps an EventFunc as a ProgressFunc (lossy: only Progress-shaped events).
func ProgressAsEvents(fn EventFunc) ProgressFunc
```

Do **not** remove or deprecate Progress in v0.x.

### 3.3 What is NOT in default events

| Excluded by default | Why |
|---|---|
| System/user/assistant message bodies | Secret / PII sprawl |
| Full tool arguments / observations | Same; tool name + duration only (as Progress) |
| Stream deltas | Belong to `WithStream` |
| Raw provider HTTP payloads | Cap / redaction already elsewhere |

**Opt-in detail (D-C4-B deferred):** `EventDetail` level on Crew — `metadata` (default) vs `summary` (hashes/lengths only) vs `full` (explicit, documented footgun). **v1 ships metadata only**; `summary` may land in the same epic if cheap; `full` stays out or behind a loud godoc + SECURITY note.

### 3.4 KickoffID (D-C6)

Generate a random ID per Kickoff (`crypto/rand` or `encoding/hex` of 16 bytes) stored on ctx. All events from that run share it. Do not require the app to set it; allow override via context for tests.

### 3.5 slog bridge (optional helper)

```go
// EventSlogHandler returns an EventFunc that logs each event at Info via logger.
// Does not replace Crew.Logger; pure convenience.
func EventLogger(logger *slog.Logger) EventFunc
```

---

## 4. Part C — Integration matrix

| Site | Emit when EventFunc set |
|---|---|
| Kickoff enter/exit | `kickoff_started` / `kickoff_completed` (+ duration, err) |
| Existing Progress sites | dual-emit matching CrewEvent types |
| `executor.go` before/after each `Call` / `callLLMText` | `llm_call_started` / `llm_call_completed` (iteration, duration, err); **no messages** |
| ReAct loop each iteration | `react_iteration` with Attrs `{"has_action": bool}` or start/end pair |
| `structured.go` each repair attempt | `structured_repair` with Attrs `{"attempt": n}` |
| `loop.go` phase boundaries | `loop_phase` with Phase=plan\|execute\|evaluate\|refine\|rewrite |
| Guardrail failure | `guardrail_blocked` |
| Waves (async) | `wave_started` / `wave_completed` (D-C8 default **on**, low cardinality) |

**Fan-out:** single `emitEvent` helper (panic recover + redact Err string).

---

## 5. Part J — JSON Schema design

### 5.1 `$ref` (D-J1) — local only

```go
// resolveRef expands local JSON Pointer refs against root.
// Supported forms:
//   "#/definitions/Foo"
//   "#/$defs/Foo"
//   "#/properties/..." (pointer into root)
// Rejected:
//   "https://..." remote URLs
//   "other.json#/..." external files
//   empty / non-fragment refs
//   cycles exceeding MaxSchemaRefDepth
const MaxSchemaRefDepth = 32
const MaxSchemaRefExpansions = 256 // per validateSchema call
```

Implementation sketch:

1. On `validateSchema`, keep `root map[string]any` + memo map `pointer → node`.
2. When a schema node contains `"$ref"`, resolve pointer (RFC 6901 subset: `/`-split, `~1`/`~0` unescape).
3. Apply **adjacent keywords** policy (D-J2):  
   **Recommendation D-J2-A (2020-12-ish pragmatic):** treat `$ref` as the base schema; **also apply sibling keywords** as `allOf`-like intersection (common in OpenAPI 3.0 docs). Document clearly; Strict mode unchanged.
4. Cycle detection via in-progress stack; error `ErrSchemaRefCycle`.
5. Missing target → validation/schema error (fail closed).

`$defs` / `definitions` become **live** targets (already walked today).

### 5.2 `format` (D-J3) — allowlisted validators

| format | Rule (stdlib) |
|---|---|
| `date-time` | `time.RFC3339` or `RFC3339Nano` parse |
| `date` | `2006-01-02` |
| `time` | `15:04:05` optional fraction / offset subset — **or** defer if ambiguous |
| `email` | pragmatic regexp (not full RFC 5322); length cap |
| `uri` / `uri-reference` | `url.Parse` + require scheme for `uri` |
| `uuid` | parse 8-4-4-4-12 hex |
| `ipv4` / `ipv6` | `net.ParseIP` |

**Unknown `format`:**  
- Default validate mode: **ignore** (JSON Schema historically annotation-only for unknown formats) — D-J4-A.  
- `WithStrictSchema` / new `WithStrictFormats`: **reject schema** if unknown format present — D-J4-B for strict construction; at instance validation unknown format ignored unless `FormatEnforce=true` on StructuredOutput.

**Recommendation:**  
- Construction StrictSchema: unknown `format` values are **allowed** (format is in known keywords) but listed in docs.  
- Instance validation: known formats enforced; unknown formats **ignored** with optional Attr/log once.  
- Remove `format` from `unsupportedStrictKeywords`; add to `knownSchemaKeywords`.

### 5.3 `const` (D-J5)

Deep equality via `reflect.DeepEqual` on decoded JSON values (same as enum membership). Move `const` from unsupported → supported.

### 5.4 `if` / `then` / `else` (D-J6)

```
if validate(instance, ifSchema) succeeds → apply then (if present)
else → apply else (if present)
```

No annotation collection required for this basic form. Nested `$ref` allowed inside branches.

### 5.5 `not` (D-J7)

If instance validates against subschema → error. Small and useful; include in v1.

### 5.6 `minProperties` / `maxProperties` / `uniqueItems` (D-J8)

Cheap and common — **include in v1**.

### 5.7 `unevaluatedProperties` / `unevaluatedItems` (D-J9)

Correct implementation needs an **annotation / evaluated-props** model across applicators (`allOf`, `$ref`, `if`, …).  

**Recommendation D-J9-B:** **defer** full unevaluated* to a follow-up (J-extra). Keep them in `unsupportedStrictKeywords` for StrictSchema. Document why.

### 5.8 Still deferred (remain unsupported)

`dependentRequired`, `dependentSchemas`, `prefixItems`, `contains`, `propertyNames`, content* , `default` as validation, remote `$ref`, boolean-schema edge cases beyond today's pass-through.

### 5.9 Error surface

```go
var (
    ErrSchemaRefCycle      = errors.New("crewai: JSON Schema $ref cycle")
    ErrSchemaRefInvalid    = errors.New("crewai: JSON Schema $ref invalid or external")
    ErrSchemaRefNotFound    = errors.New("crewai: JSON Schema $ref target not found")
    ErrSchemaRefDepth       = errors.New("crewai: JSON Schema $ref depth exceeded")
)
```

Instance failures stay `ValidationError` / `ValidationErrors` with JSON-pointer paths.

### 5.10 StructuredOutput interaction

- `WithStrictSchema` uses updated `unsupportedStrictKeywords` (smaller set).
- Existing schemas without new keywords: **bit-identical** validation results (golden tests).
- Schemas that only failed Strict because of `$ref`/`format`/`const` become constructible and validated.

---

## 6. File changes (expected)

### Callbacks (C*)

| Area | Files |
|---|---|
| API | new `event.go` (`CrewEvent`, constants, `WithEvents`, `emitEvent`, helpers) |
| Crew | `crew.go` — field + Kickoff inject + kickoff/wave events |
| Executor | `executor.go` — llm_call + react_iteration |
| Native | `toolcall.go` — align tool events / iteration attrs |
| Structured | `structured.go` — structured_repair |
| Loop | `loop.go` — loop_phase |
| Guardrail | `guardrail.go` — guardrail_blocked |
| Progress bridge | `progress.go` or `event.go` — optional dual-emit helper |
| Tests | `event_test.go`, extend executor/crew tests |
| Docs | `docs/crews.md`, `docs/llms.md` (brief), doc.go, SECURITY |
| Example | `examples/events` offline mock printing JSON lines |

### Schema (J*)

| Area | Files |
|---|---|
| Validator | `schema.go` — ref resolve, format, const, if/then/else, not, min/maxProperties, uniqueItems |
| Structured | `structured.go` — godoc only if needed |
| Errors | `errors.go` — ref sentinels |
| Tests | `schema_test.go`, `schema_ref_test.go`, `schema_format_test.go` |
| Docs | `docs/tasks.md` structured section, CHANGELOG |
| Example | optional schema fixture in existing structured example |

No new packages required.

---

## 7. Security & safety

| Topic | Rule |
|---|---|
| **Event default metadata** | Same as Progress: no prompts, no raw tool args, no stream text |
| **Err fields** | Always `redactError` / redactString before EventFunc |
| **Panic** | `emitEvent` recovers like Progress/Stream |
| **Concurrency** | EventFunc MUST be concurrency-safe (Async waves) |
| **KickoffID** | Random; not derived from secrets/inputs |
| **`$ref`** | Local fragments only — **no network fetch** (SSRF) |
| **`format: uri`** | Validation only; does not fetch |
| **Regex** | Keep `MaxSchemaPatternLen`; format email regexp must be linear |
| **Expansion bombs** | Cap ref depth + expansion count |
| **Detail full mode** | If ever added, SECURITY.md footgun + default off |

---

## 8. Tests & gates

### 8.1 Callbacks

- Dual-emit: WithProgress + WithEvents both fire for task_started.
- llm_call pair around mock Call; duration ≥ 0; no message bodies in Attrs.
- react_iteration count equals loop turns.
- structured_repair fires N times on failing schema then success.
- loop_phase order plan→execute→evaluate for AgenticLoop smoke.
- guardrail_blocked on rejecting guardrail.
- Panic in EventFunc does not fail Kickoff.
- Async wave: events from two tasks distinguishable by Task + KickoffID.
- EventLogger smoke with slogtest/buffer.

### 8.2 Schema

- `$ref` to `$defs` and `definitions`; nested refs; sibling keywords applied (per D-J2).
- External/URL ref → ErrSchemaRefInvalid at validate or Strict construct.
- Cycle A→B→A → ErrSchemaRefCycle.
- Depth bomb → ErrSchemaRefDepth.
- `format` table: good/bad for each supported format; unknown format ignored.
- `const` / `not` / `if-then-else` truth table.
- `minProperties` / `maxProperties` / `uniqueItems`.
- Golden: pre-v0.7 schemas still validate identically.
- StrictSchema: unevaluated* still rejected; `$ref`/`format`/`const` accepted.

### 8.3 Quality gates

Same as security-residuals §6.1: ≥90% coverage on touched packages, `-race`, EN+PT docs, CHANGELOG, zero new deps, gofmt/vet.

### 8.4 Acceptance (epic done)

**Callbacks**
- [ ] `CrewEvent` + `WithEvents` / `ContextWithEvents` public
- [ ] New event types: llm_call_*, react_iteration, structured_repair, loop_phase, guardrail_blocked, kickoff_*, wave_*
- [ ] Progress unchanged; bridge helper optional
- [ ] `examples/events` offline
- [ ] docs EN+PT + SECURITY note

**Schema**
- [ ] Local `$ref` + caps + cycle detection
- [ ] `format` allowlist enforced; `const`, `not`, `if`/`then`/`else`, property counts, `uniqueItems`
- [ ] StrictSchema keyword set updated; unevaluated* still strict-fail
- [ ] Golden non-regression tests
- [ ] docs EN+PT structured/schema section

**Release**
- [ ] CHANGELOG; target **v0.8.0** (minor, additive)

---

## 9. Decisions

### 9.1 Callbacks (D-C*)

| ID | Question | Options | Decision |
|---|---|---|---|
| **D-C1** | New API vs overload Progress | (A) expand Progress.Event only (B) new CrewEvent + EventFunc | **B** — cleaner export + versioning |
| **D-C2** | Progress fate | (A) dual-emit forever (B) soft-deprecate Progress | **A** in v0.x |
| **D-C3** | ReAct hook shape | (A) single `react_iteration` per turn (B) start/end pair | **A** + llm_call start/end for timing |
| **D-C4** | Bodies in events | (A) never (B) tiered detail later | **A** for v1; tiers deferred |
| **D-C5** | OTEL in core | (A) depend on otel (B) bridge in example only | **B** |
| **D-C6** | Kickoff correlation ID | (A) none (B) auto ID on ctx | **B** |
| **D-C7** | Fan-in multiple EventFuncs | (A) single slot like Progress (B) slice of handlers | **A** v1 (apps compose) |
| **D-C8** | wave_* events | (A) skip (B) emit | **B** |
| **D-C9** | llm_call on streaming path | (A) one pair around CallOrStream (B) per delta | **A** |
| **D-C10** | EventFunc panic | (A) crash (B) recover | **B** |

### 9.2 JSON Schema (D-J*)

| ID | Question | Options | Decision |
|---|---|---|---|
| **D-J1** | `$ref` scope | (A) local only (B) file (C) http | **A** |
| **D-J2** | `$ref` + siblings | (A) ignore siblings (B) apply as intersection | **B** |
| **D-J3** | `format` set | (A) none (B) allowlist above (C) large set | **B** |
| **D-J4** | unknown `format` at runtime | (A) ignore (B) always error | **A** |
| **D-J5** | `const` | (A) defer (B) ship | **B** |
| **D-J6** | `if`/`then`/`else` | (A) defer (B) ship basic | **B** |
| **D-J7** | `not` | (A) defer (B) ship | **B** |
| **D-J8** | min/maxProperties, uniqueItems | (A) defer (B) ship | **B** |
| **D-J9** | unevaluated* | (A) ship full (B) defer | **B** |
| **D-J10** | boolean schemas `true`/`false` | (A) keep today's loose pass (B) draft-accurate | **B** if low risk — `true` always ok, `false` always fail at that node |
| **D-J11** | string length unit | keep **bytes** (`len`) as today (document; do not switch to runes) | **keep bytes** |
| **D-J12** | Max ref depth / expansions | 32 / 256 | **yes** |

### 9.3 Open until spikes

| ID | Topic |
|---|---|
| **O-C1** | Exact Attr keys for llm_call (model name via `LLM.Model()` yes/no — lean **yes**, model id is metadata) |
| **O-J1** | JSON Pointer escapes edge cases + fragment-only refs to root `#` |
| **O-J2** | Whether `format: time` is included or deferred for ambiguity |

---

## 10. PR / implementation order

```
Phase 0   Close D-C* / D-J* on this document
── Callbacks train ──
Phase C1  event.go API + emitEvent + KickoffID + tests
Phase C2  dual-emit from existing Progress sites + kickoff/wave events
Phase C3  executor llm_call + react_iteration
Phase C4  structured_repair + loop_phase + guardrail_blocked
Phase C5  examples/events + docs EN/PT + SECURITY
── Schema train (parallel after Phase 0) ──
Phase J1  $ref resolver + caps + tests
Phase J2  const, not, min/maxProperties, uniqueItems, boolean schemas
Phase J3  format allowlist
Phase J4  if/then/else
Phase J5  StrictSchema set update + docs + golden non-regression
── Release ──
Phase R   CHANGELOG + PLAN checkboxes + tag v0.8.0
```

**Parallelism:** C1–C5 ∥ J1–J5 after Phase 0; no shared file conflicts if schema stays in `schema.go` and events in `event.go` / executor touch points are coordinated (C3 touches executor — J does not).

**Do not** start OTEL example until C5 API is stable.

---

## 11. Documentation plan

| Doc | Update |
|---|---|
| `docs/crews.md` + PT | WithEvents, CrewEvent types table, concurrency, vs Progress vs Stream |
| `docs/tasks.md` + PT | Structured/schema keyword table (supported vs strict-fail) |
| `doc.go` | Short Events + schema bullets |
| `README.md` + PT | Why bullet + What's new at release |
| `SECURITY.md` | Events metadata-only; `$ref` local-only |
| `examples/events` | JSONL dump of CrewEvents offline |
| `PLAN.md` / PT | Link this plan; check items when shipped |
| `CHANGELOG` | EN+PT |

---

## 12. Versioning

- Additive APIs → **v0.8.0** minor while on v0.x.
- No breaking changes to `Progress`, `StreamFunc`, or existing validate results for previously supported schemas.
- StrictSchema becomes **less** rejecty (more keywords supported) — that is backward compatible for constructors that used to fail.

---

## 13. Out of scope / later

| Item | Where |
|---|---|
| OpenTelemetry SDK | Example bridge only |
| Event detail tiers with bodies | Follow-up after security review |
| unevaluated* full model | J-extra plan |
| Remote `$ref` | Never in core without explicit allowlist epic |
| Metrics histograms in core | App-side from events |
| Multiplexed multi-crew analytics product | Out of library |

---

## 14. Summary

Ship **two parallel P2 trains**: (1) **`CrewEvent` + `WithEvents`** extending lifecycle observability beyond Progress without breaking it, metadata-safe and slog/OTEL-bridge friendly; (2) **JSON Schema remainder** centered on **local `$ref`**, **format allowlist**, **const/not/if-then-else**, and small object/array keywords, keeping **unevaluated*** and remote refs out. Zero new deps; target **v0.8.0**.

When D-C1–D-C10 and D-J1–D-J12 are acknowledged, Phase C1 / J1 coding can start on branches such as `feat/p2-events-c1` and `feat/p2-schema-j1`.
