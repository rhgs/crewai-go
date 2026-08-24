# Plan — P3: Flows, built-in tools, declarative YAML, training/trace export

> **Status:** **Design** (not started). Last major roadmap tier after v0.8.0.  
> **Decisions:** D-F1–D-F12 (Flows), D-T1–D-T10 (tools), D-Y1–D-Y10 (YAML), D-X1–D-X8 (training/export) proposed in §9; close before any Phase 1 code.  
> **Related:** [`DECISIONS.md`](DECISIONS.md), `process.go`, `crew.go` (`Kickoff`, `emitEvent`), `tool.go`/`toolcall.go`, `tools/websearch.go` (SSRf helpers), `task.go` (OutputDir jail), `memory_embed.go` (`EmbeddingFunc`), `schema.go` (validator), `loop.go` (AgenticLoop), `examples/` (22 offline demos).  
> **Constraints:** zero external module deps in core (`go.mod` stdlib-only); stdlib has **no YAML parser**; gates: ≥90% coverage on touched packages (per-package and aggregate), race-clean, gofmt+vet clean, EN+PT docs, CHANGELOG, SECURITY when applicable.  
> **Non-goals of this epic:** deferred backlog items (xAI OAuth, M5, A5, D-S10, D-J9, O-J2 — see `PLAN.deferred-backlog.md`); breaking changes to `Process`, `Tool`, `Progress`, `StreamFunc`, `EventFunc`; vector DB in core.

---

## 0. Why these four together

| Item | Today (v0.8.0) | Gap P3 closes |
|------|----------------|----------------|
| **Flows** | `Process` = Sequential / Hierarchical / Staged; Async waves via `Task.Async` | No event-driven routing with explicit user state (`@listen`, `@router`) — CrewAI-parity feature |
| **Tools** | `Tool` interface + ReAct + native FC; web search; Calculator/CurrentTime/WordCount | No safe built-in HTTP fetch or sandboxed file read/write; RAG only as docs pattern |
| **YAML** | Crews built programmatically in Go | No declarative loading (CrewAI `agents.yaml`/`tasks.yaml` parity) |
| **Training/export** | `ToolTrace`, `Progress`, `CrewEvent` exist | No curated record/export of successful runs for few-shot distillation |

They ship as **independent trains** (F/T/Y/X) — any subset can land without the others.

**Hard non-goals (reaffirmed):**
- No YAML evaluator in core (stdlib has none). YAML = **JSON-subset** YAML v1 (optional `yamlprep` submodule later).
- No vector DB, no model training, no OTEL SDK in core.
- Flows do **not** replace or deprecate existing processes.
- RAG tools never bundle a vector store; hooks via `EmbeddingFunc` / `MemoryStore` only.

---

## 1. Baseline anchors (code truth)

- **Dispatch:** `Crew.Kickoff` switches on `Process` (`process.go`): Sequential / Hierarchical / Staged; `Process.valid()` guards unknown values.
- **Hooks:** Kickoff emits `kickoff_started/completed`; tasks/stages/waves/tools have Progress + CrewEvent coverage (P2).
- **Tool contract:** `Tool{Name, Description, Call(ctx, string) (string, error)}`; native path uses `ToolSpec` + JSON Schema.
- **SSRF precedent:** `tools/websearch.go` — `isBlockedURL` blocks non-http(s), userinfo, IP literals (private/loopback/link-local/multicast/CGNAT), DNS resolves + fail-closed.
- **Jail precedent:** `resolveOutputPathInJail` (task.go) — `Clean` + `EvalSymlinks`, fail closed `ErrOutputPathRejected`.
- **Loop precedent:** `AgenticLoop` = value-style strategy pattern `for round …` (Flows should follow this shape, not a 4th Process in v1).
- **Validator reuse:** `validateSchema` (schema.go P2) powers StructuredOutput; same validator can check YAML configs with a generated JSON Schema.
- **Trace surface:** `ToolTrace` in `CrewOutput.TasksOutput`; `CrewEvent` stream; `slog` logger.

---

## 2. Train F — Flows (event-driven orchestration)

### 2.1 Goals

- CrewAI-style Flow: struct state + steps + rules `@start` → `@listen(dep)` → `@router`.
- Go-idiomatic: **methods with signature `func(ctx, *S) error`** registered declaratively; no reflection-heavy magic in v1.
- Deterministic concurrency: parallel listeners join with the same barrier + declaration-order fold contract (`docs/concurrency.md`).
- Telemetry: reuse CrewEvent types; new `Phase` values `flow`, or new event types `flow_step_*` (decision D-F7).

### 2.2 API sketch (v1)

```go
// Flow[S] is an event-driven workflow over user-defined state S.
type Flow[S any] struct{ /* steps, edges, logger, events */ }

// FlowStep is one node. Mutates state; returns error to abort/branch.
type FlowStep[S any] func(ctx context.Context, state *S) error

// Builder
f := crewai.NewFlow[MyState]().
    Start("bootstrap", stepBootstrap).          // exactly one or more start steps (D-F4)
    Listen("research", stepResearch, "bootstrap").
    Router("choose", stepChoose, "research").   // router returns next step names
    Listen("write", stepWrite, "choose").
    Listen("done", stepDone, "write")

out, err := f.Run(ctx, initial)

// Optional crew wiring: a Flow step can Kickoff a Crew (composition, not nesting).
// WithEvents forwards flow_step_* events into an EventFunc.
```

Rules:
- **DAG.** Listeners wait for **all** deps; cycles fail at `Run` start (`ErrFlowCycle`). Self-dep hard error.
- **Router.** Must return registered step names; unknown name = `ErrFlowUnknownStep`.
- **Concurrency.** Independent listeners of the same completed set run in parallel; state access is the user's responsibility — library does **not** lock `S` (D-F5 = doc contract, like `sync.Mutex` ownership). Parallel steps get sequential snapshots? **No: shared pointer, user synchronizes or uses channels.**
- **Join semantics.** A step with multiple deps runs **once** after all parents complete (join-and-fold in dependency order — parent order is declaration order of `Listen` calls). Parent state merge is user code (state is shared by pointer).
- **Terminal.** Run returns final state when all reachable steps complete; error propagation fail-fast by default; optional `WithFlowContinueOnError` records per-step errors in state? **No — in FlowOutput.Errors map (D-F8).**

```go
type FlowResult[S any] struct {
    State  S
    Steps  []FlowStepTrace // order = completion of barrier folds
    Errors map[string]error // per-step when ContinueOnError
    Duration time.Duration
}
```

### 2.3 Interaction with Crew

A step may call `crew.Kickoff` — Flows orchestrate **crews/tasks/tools as steps**, crews remain task engines. Manager/Process logic is untouched. `Flow` itself is not a `Process` (D-A6 consistency).

### 2.4 Examples

- `examples/flows_research` — mock LLM Flow: bootstrap → research ∥ news → router → write → done (deterministic offline).

---

## 3. Train T — Built-in tools (HTTP, files, RAG patterns)

### 3.1 T1 `HTTPTool` (SSRF-safe fetch)

```go
ht := tools.NewHTTPTool(
    tools.WithHTTPAllowlist("api.example.com", "docs.*.internal"), // regex on host
    tools.WithHTTPMethods("GET"),
    tools.WithHTTPMaxResponse(1 << 20),
    tools.WithHTTPTimeout(30*time.Second),
)
```

- Default policy: **deny-by-default**; empty allowlist = all requests rejected (D-T2).
- Reuses SSRF checks from `websearch.go` — **extract to shared `urlGuard`** unexported helper in tools package (no API break).
- Redirects: at most 3, each hop re-validated (D-T4).
- Request headers: caller-set map; **no implicit auth headers**; body capped both ways (`MaxToolOutputBytes` for output and new `MaxToolInputBytes` mirror? — reuse output cap for parity, D-T6).
- No model-controlled URL when allowlist empty — docs footgun.

### 3.2 T2 File read/write (`tools.FileReadTool`, `tools.FileWriteTool`)

- Root jail via same helper as `OutputDir` (extract shared `pathJail` from task.go into internal pkg or small exported helper in tools, D-T3).
- Modes: read-only default; write requires explicit `AllowWrite` + jail; sizes capped; text-only (NUL byte reject? D-T7 = reject binary by default with option).
- Never follow symlinks out of jail (EvalSymlinks pattern).

### 3.3 T3 RAG pattern (docs + tiny helpers — no vector DB)

- New short doc `docs/rag.md` (+PT): compose `MemoryStore` + `Crew.Embed` + `Query` as a tool (`MemoryQueryTool`? — product decision D-T9: **docs-only v1**, helper optional in example `examples/rag_file` using FileStore+embed mock).
- If helper ships: strictly additive type in core `tools` wrapping an injected `MemoryStore` — no chunking/embedder bundled.

### 3.4 Examples

- `examples/tools_http` (offline httptest server; allowlist demo).
- `examples/tools_files` (tempdir jail read/write).
- `examples/rag_file` (FileStore + bag-of-words embedder; query tool pattern) — confirms RAG-as-pattern stance.

---

## 4. Train Y — Declarative YAML (JSON-subset v1)

### 4.1 The constraint

Stdlib has no YAML parser and adding `gopkg.in/yaml.v3` breaks zero-deps. **D-Y1 recommendation:** accept **JSON-subset YAML** (flow-style mappings/lists — a superset of JSON read via `encoding/json`), documented loudly; full YAML stays out unless future optional submodule `github.com/rhgs/crewai-go/yamlprep` (separate module decision D-Y1-B, not in this epic's core).

> JSON-subset YAML covers real-world `agents.yaml` written as flow mappings (`{role: ..., goal: ...}` — invalid JSON keys must be quoted) — practical for copy-paste from docs. Docs ship canonical JSON-ish samples that parse.

### 4.2 API sketch

```go
cfg, err := crewai.LoadCrewFile("crew.json.yaml") // file ext irrelevant; JSON subset
// or
cfg, err := crewai.LoadCrew(io.Reader)

type CrewConfig struct {
    Agents []AgentConfig `json:"agents"`
    Tasks  []TaskConfig  `json:"tasks"`
    Crew   CrewMetaConfig `json:"crew"`
}
// Build:
crew, err := cfg.Build(crewai.WithLLMMap(map[string]crewai.LLM{...}),
                       crewai.WithToolMap(...),
                       crewai.WithGuardrailMap(...))
```

- Schema-validate the config itself with P2 validator (generated const schema, D-Y4); errors point at JSON pointer paths.
- No evaluation of arbitrary code; `llm`/`tools`/`guardrail` are **string references** into app-provided maps — unknown ref = load error (fail closed, D-Y5).
- `Task.Context` references by **name or index** (D-Y6 = name-first, index fallback); same validation as runtime (no same-wave cycles — reuse `planWaves` after build).
- Secrets: never interpolated from file (D-Y8); `${ENV_VAR}` interpolation for env only? — **v1: no interpolation at all** (docs show env wiring in Go).

### 4.3 Example

- `examples/declarative` — loads `crew.json.yaml` (JSON-subset), builds with mock LLM map, Kickoff offline.

---

## 5. Train X — Training / trace export

### 5.1 Goals

Capture **successful** runs in a portable shape for few-shot/fine-tune offline work. Library-side capture + write; training stays out.

### 5.2 API sketch

```go
// TraceRecorder wraps a run; additive to Crew.
rec := crewai.NewTraceRecorder(crewai.WithTraceMinScore(0)) // score hook: all pass default
crew.WithTracer(rec) // or crew.Tracer = rec
out, err := crew.Kickoff(ctx, nil)
rec.Save("traces/success.jsonl")

// Record shape (JSONL, one line per task):
type TaskTraceRecord struct {
    KickoffID string          `json:"kickoff_id"`
    Task      string          `json:"task"`
    Agent     string          `json:"agent"`
    Prompt    PromptSnapshot  `json:"prompt"`   // optional bodies (D-X4 default ON? — decision)
    Output    string          `json:"output"`
    Facts     []Fact          `json:"facts,omitempty"`
    Tools     []ToolTrace     `json:"tools,omitempty"`
    Events    []CrewEvent     `json:"events,omitempty"` // if WithEvents also set
    DurationMs int64          `json:"duration_ms"`
}
```

- **D-X4 recommendation:** bodies capture is **opt-in default-off** (`WithTraceBodies(true)`), redacted via `redactString` anyway; metadata-only default aligns with D-C4.
- Writer: JSONL `0600` under `OutputDir`-like jail option (D-X5) or caller path (caller-trusted).
- Filtering hook: `WithTraceFilter(func(record) bool)` (e.g., only guardrail-passed — D-X6).
- No OTEL; no remote sinks.

### 5.3 Example

- `examples/trace_export` — mock crew, writes JSONL, prints summary.

---

## 6. Gates per train (review + coverage + docs baked in)

These apply to **every implementation PR**, not just the epic end.

### 6.1 Code review checklist (mandatory before merge)

- [ ] Concurrency contract unchanged: barrier + declaration-order fold; no new shared state without mutex/channel ownership documented.
- [ ] Flows: single-flight per `*Flow[S]` Run? (D-F9 = yes, mirror `ErrCrewRunning` pattern → `ErrFlowRunning`).
- [ ] Tools: outputs capped (`MaxToolOutputBytes`); inputs validated; SSRF/jail helpers reused, not forked.
- [ ] YAML: build-time validation reuses `planWaves` semantics; no code executed from file; JSON-only parser path tested with adversarial docs.
- [ ] Training: default metadata-only; bodies opt-in flag explicitly named `WithTraceBodies`.
- [ ] No goroutine leaks: every new `go` statement has a documented join/cancel path; channels closed per contract.
- [ ] Errors: new sentinels in `errors.go`, wrapped with `%w`, exported godoc.
- [ ] Public API additive only.

### 6.2 Security review checklist

- [ ] HTTP tool: SSRF matrix tests (private/loopback/CGNAT/DNS-rebind/userinfo/non-http; redirect hop re-check; empty allowlist denies).
- [ ] File tools: escape attempts (`../`, symlink out, absolute outside jail) fail closed; write disabled by default.
- [ ] YAML loader: no env interpolation; unknown references fail; size cap on file (e.g. 1 MiB, D-Y9); no remote includes.
- [ ] Trace export: when bodies off, only labels/counts/durations recorded; redaction of error strings; file perms `0600`.
- [ ] Flows: state pointer ownership documented; no implicit secret propagation into events.
- [ ] SECURITY.md updated per train.

### 6.3 Coverage gate

- [ ] `pkgs=$(go list ./... | grep -v /examples)` aggregate stays **≥ 90%** (currently 92.8%).
- [ ] Each **touched package** ≥ 90% (new code ships with tests, not after).
- [ ] Race: `go test -race` on touched packages clean.
- [ ] Example mains excluded by CI as today.

### 6.4 Documentation review checklist (per train)

- [ ] New guide: `docs/flows.md`, `docs/tools.md` expansion, `docs/declarative.md`, `docs/training.md` (+ PT mirrors).
- [ ] README Why bullet + Concepts rows + What's new on release tag.
- [ ] `doc.go` short section.
- [ ] Examples README tables + run blocks.
- [ ] CHANGELOG EN+PT; PLAN checkbox; DECISIONS.md new IDs.
- [ ] Docs reviewer pass: verify every public symbol in godoc matches guide snippets (compile-checked via examples where possible).

---

## 7. File changes (expected)

| Train | New/changed files |
|---|---|
| **F** | `flow.go` (+`flow_test.go`), `errors.go` (+sentinels), `examples/flows_research/`, `docs/flows.md`(+PT), `doc.go`, README, PLAN |
| **T** | `tools/http.go`(+test), `tools/files.go`(+test), shared SSRF/jail extraction (internal or `tools/urlguard.go`), `docs/tools.md`(+PT), `docs/rag.md`(+PT), examples ×3, SECURITY |
| **Y** | `deck.go`/`load.go` (`LoadCrew`, `CrewConfig`, `Build` options)(+`load_test.go`), config schema const, `examples/declarative/`, `docs/declarative.md`(+PT), errors, README |
| **X** | `trace.go`(+`trace_test.go`), `Crew.Tracer` field + hook points in executor, `examples/trace_export/`, `docs/training.md`(+PT), SECURITY |

No changes to `process.go` dispatch in v1 (Flows is a separate runner type).

---

## 8. Implementation order (PR trains)

```
Phase 0   Close D-F*, D-T*, D-Y*, D-X* in this doc → move into DECISIONS.md
── T1 first: T1 urlguard extract + HTTPTool + tests + docs
── T2 pathJail extract + File tools + tests + docs
── F1 Flow core (Runner, D-F2 builder) + cycle validation
── F2 Flow concurrency (parallel listeners, join/fold) + events
── Y1 JSON-subset loader + schema validation + Build maps
── X1 TraceRecorder metadata default + Save
── RAG doc/example (docs-only train, can be anytime after M3 exists — already does)
── Release sync (CHANGELOG/PLAN/DECISIONS) → tag v0.9.0 (or split minors per train)
```

Recommended packaging: **one minor per train if cadence matters** (T→v0.9.0, F→v0.10.0, Y→v0.11.0, X→v0.12.0) or one **v0.9.0 bundle**. Decide at release time; plan accepts both (D-G-release = bundle default, split when any train lags).

---

## 9. Decisions to close (Phase 0)

### Flows (D-F*)

| ID | Question | Options | Recommendation |
|---|---|---|---|
| **D-F1** | Runner style | (A) new `Process` value (B) separate `Flow[S]` type | **B** — value pattern like AgenticLoop; no process-table churn |
| **D-F2** | Step registration | (A) struct tags/reflection (B) func builder `Listen(name, fn, deps...)` | **B** — explicit, typed, testable |
| **D-F3** | Generic state | `Flow[S any]` generics | **yes go1.24 generics** |
| **D-F4** | Starts | (A) implicit zero-dep steps (B) explicit `Start` | **A+B**: zero-dep steps are starts; `Start` optional marker for docs — decide in review |
| **D-F5** | State locking | (A) library locks per step (B) user-owned sync | **B** — documented ownership (matches Go norms) |
| **D-F6** | Router result | (A) exact names (B) prefix match | **A** — unknown name errors |
| **D-F7** | Events | (A) new types `flow_step_*` (B) reuse Phase | **A** — typed constants set |
| **D-F8** | ContinueOnError storage | (A) state (B) FlowResult.Errors | **B** |
| **D-F9** | Concurrent Run on same flow | (A) allow (B) single-flight | **B** — `ErrFlowRunning` |
| **D-F10** | Join semantics multi-dep | (A) all deps (B) any dep | **A** v1; router covers choice |
| **D-F11** | Cancellation | ctx aborts next barrier; fail-fast default true | standard |
| **D-F12** | Determinism of parallel step trace order | completion vs fold by registration order | **fold by registration order** (same doctrine) |

### Tools (D-T*)

| ID | Question | Options | Decision |
|---|---|---|---|
| **D-T1** | HTTP tool in `tools` pkg | yes | **yes** |
| **D-T2** | Allowlist default | (A) deny-by-default (B) public-net allow | **A** |
| **D-T3** | Jail helper sharing | extract shared internal helper | **yes** |
| **D-T4** | Redirect policy | max 3, re-validate each hop | **yes** |
| **D-T5** | File write | off by default, explicit `AllowWrite` | **yes** |
| **D-T6** | Byte caps | reuse `MaxToolOutputBytes`; input cap separate const `MaxToolInputBytes`? | **single cap reuse** v1 |
| **D-T7** | Binary content | reject by default (NUL/check), opt `AllowBinary` | **reject default** |
| **D-T8** | HTTP methods default | GET only | **yes** |
| **D-T9** | RAG helper | (A) none, docs pattern (B) `tools.MemoryQueryTool` | **A** v1 (example carries pattern) |
| **D-T10** | Tool naming | `tools.HTTPFetch`, `tools.FileRead`, `tools.FileWrite` | **as written** |

### YAML (D-Y*)

| ID | Question | Options | Decision |
|---|---|---|---|
| **D-Y1** | Parser | (A) JSON-subset in core (B) yaml dep (C) submodule | **A**, C deferred |
| **D-Y2** | API entry | `LoadCrew(io.Reader)` / `LoadCrewFile` | **both** |
| **D-Y3** | Build wiring | reference maps for llm/tools/guardrails | **yes** |
| **D-Y4** | Validation | internal JSON Schema via P2 validator | **yes** |
| **D-Y5** | Unknown refs | fail closed at Build | **yes** |
| **D-Y6** | Context refs | name-first, index fallback | **name-first** |
| **D-Y7** | Field parity with Go structs | full `Agent`/`Task` exported fields subset | subset + docs table |
| **D-Y8** | Interpolation | none in v1 (no env expansion) | **none** |
| **D-Y9** | Size cap | 1 MiB reader cap | **yes** |
| **D-Y10** | Error format | `ValidationError` pointer paths | **reuse** |

### Training/export (D-X*)

| ID | Question | Options | Decision |
|---|---|---|---|
| **D-X1** | Recorder attachment | `Crew.Tracer` field / `WithTracer` | **field + option** |
| **D-X2** | Record emission point | per task post-execute (post-guardrail? D-X6 filter) | **post-execute, pre-guardrail** + filter |
| **D-X3** | Format | JSONL | **yes** |
| **D-X4** | Bodies | metadata-only default; `WithTraceBodies(true)` opt-in | **opt-in** |
| **D-X5** | Save path safety | caller-trusted path or jail option | **caller-trusted + 0600** |
| **D-X6** | Filter hook | `WithTraceFilter(func(TaskTraceRecord) bool)` | **yes** |
| **D-X7** | Concurrency | recorder serializes records under mutex (declaration fold keeps order) | **mutex** |
| **D-X8** | Async wave records | fold order (not completion) in JSONL, or completion-marked? | **fold order + KickoffID + wave label** |

---

## 10. Risk register

| Risk | Mitigation |
|---|---|
| Flow state races blamed on library | D-F5 doc contract + race-tested example with `sync.Mutex` state |
| HTTP tool bypass via DNS re-resolution between guard and fetch | resolve-once + pin IP dial (D-T extension if needed — mark during review) |
| YAML JSON-subset surprises users | Docs call it “JSON-compatible YAML subset” + error messages show exact failure offsets |
| Trace bodies leak secrets when opted in | SECURITY footgun + redaction stays on + bodies flag named with `Bodies` |
| Coverage drops below 90 with 4 parallel trains | Per-train gate before merge, not at release |
| Flow API churn vs CrewAI parity expectations | docs/flows.md mapping table CrewAI decorators ↔ builder API |

---

## 11. Acceptance (epic done)

- [ ] D-F1–D-F12, D-T1–D-T10, D-Y1–D-Y10, D-X1–D-X8 closed in `DECISIONS.md`
- [ ] `Flow[S]` runner with DAG validation, barrier fold, events, `examples/flows_research`
- [ ] `tools.HTTPFetch`, `tools.FileRead`/`FileWrite` (jail, SSRF, caps) + 3 examples
- [ ] `docs/rag.md` pattern (+ optional example)
- [ ] `LoadCrew` JSON-subset loader + schema validation + `examples/declarative`
- [ ] `TraceRecorder` metadata-default JSONL + `examples/trace_export`
- [ ] Docs EN+PT for all four trains; README What's new; SECURITY notes
- [ ] Coverage ≥90% (aggregate + each touched pkg), `-race` clean
- [ ] CHANGELOG EN+PT; release tags per §8 packaging decision

**Target window:** after v0.8.0; do **not** bundle with deferred backlog items.

---

## 12. Summary

Four independent P3 trains with a shared doctrine: additive APIs, stdlib-only, deterministic concurrency, metadata-safe telemetry, deny-by-default tools. Phase 0 is decisions; then F/T/Y/X trains land in the §8 order with per-PR code review, security review, coverage and docs gates already enumerated (§6).
