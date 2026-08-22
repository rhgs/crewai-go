# Decision log (crewai-go)

> **Languages:** **English** (current) · [Português](DECISIONS.pt-BR.md)  
> **Status:** **Living document** — update this file whenever a product or design decision is taken or changed.  
> **Authority:** Epic plans (`PLAN.*.md`) keep the long rationale; **this file is the index of closed and open IDs**.  
> **How to add an entry:** append under the right family with ID, date, question, options, choice, status, and a pointer to the source plan or PR. Do not renumber closed IDs.

---

## 0. Conventions

| Field | Meaning |
|-------|---------|
| **ID** | Stable code (`D-M7`, `D-S1`, `G12`, `D4`, …). Never reuse an ID for a different question. |
| **Status** | `closed` · `open` · `deferred` · `superseded` |
| **Choice** | Letter or short label matching an option (e.g. **B**). |
| **Options** | Alternatives considered when the decision was made. |
| **Shipped** | First release or PR that implemented the choice (if any). |

**ID families**

| Prefix | Domain | Source plan |
|--------|--------|-------------|
| **D1–D7** | Security residuals (v0.5) | `PLAN.security-residuals.md` |
| **D-M\*** | Long-term memory | `PLAN.memory-async.md` |
| **D-A\*** | Async beyond Staged | `PLAN.memory-async.md` |
| **G\*** | Gaps closed with memory-async | `PLAN.memory-async.md` §9.2 |
| **D-S\*** | Streaming | `PLAN.streaming.md` |
| **D-C\*** | Lifecycle events / telemetry | `PLAN.p2-callbacks-schema.md` |
| **D-J\*** | JSON Schema remainder | `PLAN.p2-callbacks-schema.md` |
| **P-\*** | Standing product choices (roadmap §7) | `PLAN.md` |

Open implementation spikes use **O-\*** in epic plans; promote to a **D-\*** when closed.

---

## 1. Security residuals (D1–D7) — closed 2026-08-20 · v0.5.0

Source: [`PLAN.security-residuals.md`](PLAN.security-residuals.md) §12.

| ID | Question | Options | Choice | Status | Shipped | Notes |
|----|----------|---------|--------|--------|---------|-------|
| **D1** | MCP default HTTP timeout | (A) 30s (B) other default (C) no default | **A** — 30s; override programmatic + JSON; `0` disables | closed | v0.5.0 | `WithHTTPTimeout`, config `timeout` |
| **D2** | OutputFile jail + symlinks | (A) EvalSymlinks when jail set (B) Clean only | **A** — EvalSymlinks on; fail closed on eval error | closed | v0.5.0 | `OutputDir`, `ErrOutputPathRejected` |
| **D3** | Where does RedactHandler live? | (A) root `crewai` (B) subpackage | **A** — `crewai.RedactHandler` | closed | v0.5.0 | |
| **D4** | Concurrent Kickoff on same `*Crew` | (A) TryLock + error (B) queue/mutex wait | **A** — `ErrCrewRunning` fail fast | closed | v0.5.0 | No queue |
| **D5** | How is delegation tool attached? | (A) always on (B) always off / manual only (C) opt-in flag | **C** — `EnableDelegationTool` default false; explicit `WithTools` still OK | closed | v0.5.0 | Targets need `AllowDelegation` |
| **D6** | Structured gather budget exhausted | (A) hard fail (B) silent capture (C) capture + warning | **C** — proceed to capture + `AddWarning` | closed | v0.5.0 | `AllowTools` path |
| **D7** | `minLength` / `maxLength` unit | (A) bytes `len(s)` (B) runes | **A** — bytes; documented | closed | v0.5.0 | Same unit kept in D-J11 |

---

## 2. Memory (D-M1–D-M7) — closed 2026-08-21 · v0.6.0

Source: [`PLAN.memory-async.md`](PLAN.memory-async.md) §2.10 / §9.1. PR #31.

| ID | Question | Options | Choice | Status | Shipped | Notes |
|----|----------|---------|--------|--------|---------|-------|
| **D-M1** | Default store when `Memory=true` and `Store==nil` | (A) InMemory (B) require explicit Store | **A** | closed | v0.6.0 | Zero surprise |
| **D-M2** | FileStore package location | (A) root `filestore.go` (B) subpackage | **A** | closed | v0.6.0 | Move only if cycle/growth |
| **D-M3** | Inject when task already has `Context` | (A) never (B) always append (C) policy flag | **C** — flag `InjectWhenEmptyContext`, default true (= A behavior) | closed | v0.6.0 | Never inject uncommitted entries (D-M7) |
| **D-M4** | Automatic injection query text | (A) empty → latest N committed (B) task keywords (C) embed description | **A** | closed | v0.6.0 | Stable order on committed snapshot |
| **D-M5** | Corrupt JSONL line on Open | (A) fail Open (B) skip + warning | **B** | closed | v0.6.0 | `CorruptSkipped` counter |
| **D-M6** | Who closes external FileStore? | (A) app owns Close (B) Crew.Close always | **A** | closed | v0.6.0 | Crew closes only store it created |
| **D-M7** | When do AutoSave/`Put`s become visible to orchestrator inject/`Query`? | (A) live event-log / completion order (B) per-task buffer + commit at wave/stage barrier in **declaration order** (C) hybrid: raw Put immediate, orchestrator reads committed only | **B** | closed | v0.6.0 | Prompt path never sees in-flight siblings. See `docs/concurrency.md` |

---

## 3. Async beyond Staged (D-A1–D-A6) — closed 2026-08-21 · v0.6.0

Source: [`PLAN.memory-async.md`](PLAN.memory-async.md) §3.11 / §9.1. PR #31.

| ID | Question | Options | Choice | Status | Shipped | Notes |
|----|----------|---------|--------|--------|---------|-------|
| **D-A1** | Mixed Async/sync scheduling | (A) wave rule: ready Async together; sync alone; fold by declaration index (B) global serial except pure-Async subgraph | **A** | closed | v0.6.0 | Same contract as Staged barrier/fold |
| **D-A2** | Hierarchical agent resolution under async | (A) serial pre-resolve then waves (B) parallel lazy resolve | **A** | closed | v0.6.0 | |
| **D-A3** | `FailFast=false` after a task fails | (A) skip dependents only (B) abort whole crew | **A** | closed | v0.6.0 | `FailFast=true` (default) aborts Kickoff |
| **D-A4** | Default `AsyncMaxWorkers` | (A) 0 unlimited (B) GOMAXPROCS (C) 8 | **C** default **8**; **0 = unlimited** escape | closed | v0.6.0 | `NewCrew` sets 8; bare `Crew{}` leaves 0 unlimited by design |
| **D-A5** | `Task.Async` under `Process=Staged` | (A) ignore flag (B) error if set | **A** | closed | v0.6.0 | Stages own batching; G5 one-shot Warn |
| **D-A6** | New process constant vs flags | (A) Sequential/Hierarchical + `Task.Async` only (B) new `Process=Async` | **A** | closed | v0.6.0 | Optional `Process=DAG` alias deferred (A5 backlog) |

---

## 4. Memory-async gaps (G1–G12) — closed 2026-08-21 · v0.6.0

Source: [`PLAN.memory-async.md`](PLAN.memory-async.md) §9.2. Not originally in D-M/D-A tables; closed in the same pass.

| ID | Question | Options | Choice | Status | Shipped | Notes |
|----|----------|---------|--------|--------|---------|-------|
| **G1** | Fold/commit order key | (A) index in `Crew.Tasks` (B) mandatory `Task.ID` | **A** | closed | v0.6.0 | No required Task.ID in v1 |
| **G2** | AutoSave on failed tasks | (A) only success (B) also failures | **A** | closed | v0.6.0 | Failures stay on output/logs |
| **G3** | Default `MemoryPolicy.Scope` | (A) always empty (B) `Crew.Name` when set | **B** | closed | v0.6.0 | |
| **G4** | Fate of `Memory bool` | (A) permanent v0.x alias (B) deprecate | **A** | closed | v0.6.0 | `true` ⇒ ensure InMemory for Kickoff |
| **G5** | Log when Async ignored under Staged | (A) silent (B) one-shot Warn | **B** | closed | v0.6.0 | Once per Kickoff |
| **G6** | Meaning of `CreatedAt` vs commit order | (A) CreatedAt = finish time; order = declaration (B) order = wall-clock | **A** | closed | v0.6.0 | |
| **G7** | FileStore multi-process | (A) single-writer v1, no flock (B) flock | **A** | closed | v0.6.0 | Path caller-trusted |
| **G8** | When AutoEmbed runs | (A) serial at barrier (B) parallel in workers | **A** | closed | v0.6.0 | |
| **G9** | D-M7 barrier on Staged | (A) yes, same as Async waves (B) Staged only live Save | **A** | closed | v0.6.0 | Closes Memory caveat on Staged |
| **G10** | Release packaging | (A) one minor with M1–M2+A1–A4 (B) trail FileStore) | **A** (M3+M4 also in v0.6.0) | closed | v0.6.0 | |
| **G11** | Oversized memory entry | (A) reject Put; AutoSave warn+capture (B) truncate (C) abort Kickoff | **A** | closed | v0.6.0 | `MaxMemoryEntryBytes` |
| **G12** | When to validate Context DAG | (A) Kickoff start vs planned waves (B) mid-wave | **A** | closed | v0.6.0 | `ErrTaskDependencyCycle`; same-wave hard error |

---

## 5. Streaming (D-S1–D-S14) — closed 2026-08-21 · v0.7.0

Source: [`PLAN.streaming.md`](PLAN.streaming.md) §9. PRs #35–#36.

| ID | Question | Options | Choice | Status | Shipped | Notes |
|----|----------|---------|--------|--------|---------|-------|
| **D-S1** | API shape | (A) `CallStream` on `LLM` (B) optional `StreamingLLM` | **B** | closed | v0.7.0 | Type assert; no break for Call-only LLMs |
| **D-S2** | Terminal chunk | (A) last delta may ride on `{Done:true}` (B) empty Done after last delta | **A** | closed | v0.7.0 | |
| **D-S3** | App sink attachment | (A) ctx + `Crew.WithStream` (B) Agent field only (C) on LLM) | **A** | closed | v0.7.0 | Mirrors Progress |
| **D-S4** | Which paths stream in v1 | (A) all Call sites (B) final text / no-tools only (C) all but structured | **B** | closed | v0.7.0 | ReAct+tools, structured stay on Call |
| **D-S5** | Progress stream metadata events | (A) none in v1 (B) stream_started/completed | **A** | closed | v0.7.0 | Telemetry is D-C\* |
| **D-S6** | Provider PR order | (A) all providers one PR (B) mock+openai first, then others | **B** | closed | v0.7.0 | xAI via openai inner |
| **D-S7** | Non-streaming LLM + sink set | (A) no deltas (B) one Delta+Done full text | **B** | closed | v0.7.0 | |
| **D-S8** | Size limit | (A) higher stream cap (B) cap text only (C) cap text **and** raw body bytes | **C** | closed | v0.7.0 | `MaxProviderResponseBytes` |
| **D-S9** | sink == nil | (A) still CallStream (B) always Call | **B** | closed | v0.7.0 | No surprise goroutines |
| **D-S10** | Partial tool-call streaming | (A) v1 (B) deferred | **B** | closed | — | Deferred |
| **D-S11** | Channel buffer | (A) unbuffered (B) 16 (C) 64 | **B** | closed | v0.7.0 | `DefaultStreamChanBuffer` |
| **D-S12** | StreamFunc panic | (A) crash Kickoff (B) recover + slog | **B** | closed | v0.7.0 | `emitStream` |
| **D-S13** | Concurrent-task demux | (A) deltas only (B) Task/Agent on chunk via executor (C) per-task sinks API | **B** | closed | v0.7.0 | `withTaskAgent` |
| **D-S14** | CallStream error contract | (A) `(chan, error)` setup (B) chan never nil; setup as first `{Err}`; bare close → incomplete/ctx; forbid Done∧Err | **B** | closed | v0.7.0 | Sentinels in `errors.go` |

**Spikes closed:** O-S3 → `CollectStream` is **public**.

---

## 6. Lifecycle events (D-C1–D-C10) — closed 2026-08-21 · v0.8.0

Source: [`PLAN.p2-callbacks-schema.md`](PLAN.p2-callbacks-schema.md) §9.1. PR #39.

| ID | Question | Options | Choice | Status | Shipped | Notes |
|----|----------|---------|--------|--------|---------|-------|
| **D-C1** | New API vs overload Progress | (A) expand Progress only (B) `CrewEvent` + `EventFunc` | **B** | closed | v0.8.0 | |
| **D-C2** | Progress fate in v0.x | (A) dual-emit forever (B) soft-deprecate Progress | **A** | closed | v0.8.0 | |
| **D-C3** | ReAct hook shape | (A) single `react_iteration` + llm_call pair (B) start/end only | **A** | closed | v0.8.0 | |
| **D-C4** | Bodies in events | (A) never in v1 (B) tiered detail | **A** | closed | v0.8.0 | Metadata-only |
| **D-C5** | OTEL in core | (A) depend on otel (B) example bridge only | **B** | closed | v0.8.0 | Zero new deps |
| **D-C6** | Kickoff correlation ID | (A) none (B) auto on ctx | **B** | closed | v0.8.0 | `KickoffID` |
| **D-C7** | Multiple EventFuncs | (A) single slot (B) handler slice | **A** | closed | v0.8.0 | Apps compose |
| **D-C8** | wave_* events | (A) skip (B) emit | **B** | closed | v0.8.0 | |
| **D-C9** | llm_call under streaming path | (A) one pair around CallOrStream (B) per delta | **A** | closed | v0.8.0 | |
| **D-C10** | EventFunc panic | (A) crash (B) recover | **B** | closed | v0.8.0 | `emitEvent` |

**Lean on spike O-C1:** include `model` via `LLM.Model()` in llm_call Attrs (metadata) — **implemented**.

---

## 7. JSON Schema remainder (D-J1–D-J12) — closed 2026-08-21 · v0.8.0

Source: [`PLAN.p2-callbacks-schema.md`](PLAN.p2-callbacks-schema.md) §9.2. PR #39.

| ID | Question | Options | Choice | Status | Shipped | Notes |
|----|----------|---------|--------|--------|---------|-------|
| **D-J1** | `$ref` scope | (A) local only (B) file (C) http | **A** | closed | v0.8.0 | No network fetch |
| **D-J2** | `$ref` + sibling keywords | (A) ignore siblings (B) apply as intersection | **B** | closed | v0.8.0 | |
| **D-J3** | `format` vocabulary | (A) none (B) allowlist (C) large set | **B** | closed | v0.8.0 | date-time, date, email, uri, uri-reference, uuid, ipv4, ipv6 |
| **D-J4** | Unknown `format` at runtime | (A) ignore (B) always error | **A** | closed | v0.8.0 | |
| **D-J5** | `const` | (A) defer (B) ship | **B** | closed | v0.8.0 | |
| **D-J6** | `if`/`then`/`else` | (A) defer (B) ship basic | **B** | closed | v0.8.0 | |
| **D-J7** | `not` | (A) defer (B) ship | **B** | closed | v0.8.0 | |
| **D-J8** | min/maxProperties, uniqueItems | (A) defer (B) ship | **B** | closed | v0.8.0 | |
| **D-J9** | unevaluated* | (A) ship full (B) defer | **B** | closed | — | Still StrictSchema-fail |
| **D-J10** | Boolean schemas true/false | (A) loose pass-through (B) draft-accurate | **B** | closed | v0.8.0 | true always ok; false fails |
| **D-J11** | String length unit | keep bytes vs switch to runes | **bytes** (`len`) | closed | v0.8.0 | Aligns D7 |
| **D-J12** | Ref depth / expansion caps | 32 / 256 | **yes** | closed | v0.8.0 | Anti bomb |

---

## 8. Standing product choices (P-*)

Roadmap-level choices not owned by a single epic table. Update when product direction changes.

| ID | Question | Options | Choice | Status | Notes |
|----|----------|---------|--------|--------|-------|
| **P-DEPS** | External modules in core | (A) allow (B) stdlib only | **B** | closed | Core design principle |
| **P-FC-REACT** | Native function calling vs ReAct | (A) ReAct only (B) native only (C) keep both | **C** — ReAct default/fallback; native opt-in | closed | Revisit if ReAct cost dominates |
| **P-MODULE** | Go module path | historical forks vs `github.com/rhgs/crewai-go` | **`github.com/rhgs/crewai-go`** | closed | Published |
| **P-XAI-OAUTH** | Hard-coded xAI OAuth client defaults | (A) invent defaults (B) configurable until official docs | **B** | open | RFC 8628; fix when xAI publishes |
| **P-STREAM-SHAPE** | Streaming API (historical open item) | (A) CallStream on LLM (B) StreamingLLM | **B** | closed | Superseded by D-S1; v0.7.0 |
| **P-M5** | Agent memory tools `recall_memory` / `remember` | (A) ship (B) defer | **B** | deferred | Only if product asks |
| **P-A5** | `Process=DAG` alias | (A) ship (B) defer | **B** | deferred | Naming sugar only |

---

## 9. Open decisions

Deferred / open items with unblock conditions and pre-allocated IDs: [`PLAN.deferred-backlog.md`](PLAN.deferred-backlog.md) ([PT](PLAN.deferred-backlog.pt-BR.md)).

Nothing in D1–D7, D-M\*, D-A\*, G\*, D-S\*, D-C\*, D-J\* is open for re-litigation without a new ID.

| ID | Topic | Status | Next step |
|----|-------|--------|-----------|
| **P-XAI-OAUTH** | Official xAI OAuth client_id / endpoints | open | Update defaults when xAI documents them |
| **P-M5** / **P-A5** | Memory tools / Process=DAG | deferred | Product request + design note |
| **D-S10** follow-up | Native tool-call partial streaming | deferred | New D-S\* when designed |
| **D-J9** follow-up | unevaluatedProperties/Items | deferred | J-extra plan if needed |
| **O-J2** | `format: time` | open spike | Add format or document deferral |

When closing an open item: move it into the right section table, set **Status=closed**, fill **Choice** and **Shipped**, and leave a one-line note here.

---

## 10. Changelog of this document

| Date | Change |
|------|--------|
| 2026-08-21 | Initial living log: D1–D7, D-M\*, D-A\*, G\*, D-S\*, D-C\*, D-J\*, P-\* |
| 2026-08-21 | Mark D-C\* / D-J\* shipped in **v0.8.0** |

---

## 11. Related docs

| Doc | Role |
|-----|------|
| [`PLAN.md`](PLAN.md) | Roadmap and maturity |
| [`docs/concurrency.md`](../docs/concurrency.md) | Runtime contract for parallel groups (implements D-M7, D-A1, G1, G9, G12, …) |
| Epic `PLAN.*.md` | Full rationale and rejected alternatives in prose |
