# Plan — Deferred backlog: open/spike items (P-XAI-OAUTH, M5, A5, D-S10, D-J9, O-J2)

> **Status:** **Design settled** — product ack 2026-08-21 (A1–A5). O-J2 closed → **D-JT1** shipped in schema. M5/D-S10/D-J9 design settled but still **unscheduled** (implement only on product request). P-XAI-OAUTH still blocked externally; A5 remains deferred. This plan defines the **conditions to unblock** each, the **design surface** when they start, and the **decision IDs** they will mint.  
> **Decisions:** Pre-allocated IDs live in [`DECISIONS.md`](DECISIONS.md) §7A. O-J2 closed as **D-JT1**. M5/D-S10/D-J9 design settled (D-MT\*/D-ST\*/D-JE\*) but still unscheduled.  
> **Related:** [`DECISIONS.md`](DECISIONS.md) §9 (open table), `PLAN.streaming.md` (D-S10), `PLAN.p2-callbacks-schema.md` (D-J9, O-J2), `PLAN.memory-async.md` (M5, A5), `llm/xai/oauth.go` (P-XAI-OAUTH), `schema.go` (D-J9, O-J2).  
> **Constraints:** Same gates as all epic plans — zero new core deps, ≥ 90% coverage on touched packages, race-clean, EN+PT docs, CHANGELOG.

---

## 1. Purpose

The main roadmap (PLAN.md §6) shipped **P0–P2** and left a small set of items that are either **blocked on an external dependency** (xAI docs) or **deferred until product demand justifies them** (M5, A5, D-S10, D-J9, O-J2). This plan is the **single place** that defines:

- what unblocks each item,
- what the design space looks like,
- what decision IDs it will mint,
- and what it must *not* do when it starts.

This is not an implementation plan. Code only starts when the unblock condition is met **and** a specific sub-plan or spike confirms the design.

---

## 2. Item index

| ID | Item | Category | Unblock condition | Complexity |
|----|------|----------|-------------------|------------|
| **P-XAI-OAUTH** | Hard-coded xAI OAuth client_id + endpoints | Blocked external | xAI publishes official OAuth docs | Low (config only) |
| **M5** | `recall_memory` / `remember` agent tools | Deferred — product demand | Product requests agent-driven memory tools | Medium |
| **A5** | `Process=DAG` alias | Deferred — naming sugar | Users confused by "Sequential+Async" naming | Trivial |
| **D-S10** | Stream native tool-call partial JSON | Deferred — technical complexity | Product demand + provider spike | High |
| **D-J9** | `unevaluatedProperties` / `unevaluatedItems` | Deferred — correctness cost | Real schemas need it; annotation model spike | High |
| ~~**O-J2**~~ → **D-JT1** | `format: time` | **Closed — ship** | shipped (next patch) | Trivial |

---

## 3. Item details

### 3.1 P-XAI-OAUTH — xAI OAuth defaults

**Blocked on:** xAI publishing official OAuth 2.0 Device Flow documentation (public client_id, device code URL, token URL, scope).

**Current state** ([`llm/xai/oauth.go`](../llm/xai/oauth.go)):
- Implements RFC 8628 Device Authorization Grant + RFC 7636 PKCE.
- `ClientID` is required from the caller.
- Endpoints default to `{base}/oauth2/device/code` and `{base}/oauth2/token`; overridable via `WithDeviceCodeURL` / `WithTokenURL`.
- Works if the caller provides the correct `client_id`.

**What will happen when unblocked:**

1. Read the official xAI docs.
2. Set `DefaultXAIClientID` and default endpoint constants if they differ from our guesses.
3. If the flow matches RFC 8628 exactly: only constants change. If xAI adds non-standard parameters, mint **D-XA1** (custom parameter set).
4. No interface change to `oauth.DeviceFlow`.

**Will mint:** 
- **D-XA1** — default client_id / endpoint constants (or "no change needed").
- **D-XA2** (if non-standard params) — extra form fields on device code / token requests.

**Non-goals:** Browser-based OAuth flows, token refresh beyond what RFC 8628 specifies, multi-tenant app registration.

---

### 3.2 M5 — Agent memory tools (`recall_memory` / `remember`)

**Deferred until:** product explicitly wants agents to **call** memory as tools (not just have memory auto-injected).

**Why deferred (from PLAN.memory-async §10):**
- Auto-inject + auto-save via `MemoryPolicy` covers the common case.
- Making memory a Tool surface adds prompt complexity: when should the agent call `recall_memory` vs. what's already in context?
- Tool output size is capped (`MaxToolOutputBytes`), so recall hits would need their own budget or the same one.
- `remember` writes directly to `MemoryStore.Put`, bypassing D-M7 barrier — would need a policy decision on whether tool-driven writes are immediate or buffered.

**Design space when started:**

| Question | Options | Will mint |
|----------|---------|-----------|
| Are `recall_memory` / `remember` separate tools or one? | (A) two tools (B) one `memory` tool with `action` param | D-MT1 |
| Tool attachment mechanism | (A) `Crew.MemoryTools` flag (B) explicit `WithTools` per agent (C) auto-attach when `Memory=true` and a new flag set | D-MT2 |
| Recall query text | (A) tool args `query` string (B) embedding via `Crew.Embed` (C) both | D-MT3 |
| Write visibility | (A) immediate Put (bypasses D-M7) (B) buffered like AutoSave (C) new `PutMode` field | D-MT4 |
| Recall budget | (A) `MemoryPolicy.DefaultLimit` (B) tool-specific (C) `MaxToolOutputBytes` governs | D-MT5 |

**Implementation sketch:** two small tools, each `Fact`-free (not FactSource). Agent calls them explicitly via ReAct or native tool calling.

**Non-goals:** Vector DB in core, RAG patterns, changing D-M7 barrier semantics.

---

### 3.3 A5 — `Process=DAG` alias

**Deferred until:** enough users find "Sequential + `Task.Async`" confusing.

**Why deferred (from PLAN.memory-async D-A6):** fewer concepts is better. `Sequential` with `Async=true` tasks already is "DAG with barrier + fold". Creating a fourth `Process` value adds a name, docs, compatibility surface, and tests for what's already supported.

**What would change when unblocked:**

```go
// ProcessDAG is an alias — sets Sequential with all tasks implicit Async.
const ProcessDAG Process = Process("dag")
```

OR

```go
// Crew.Schedule could become a string alias field. Both are naming sugar only.
```

**Will mint:** **D-A7** — alias name and mechanism (Process constant vs. Crew field vs. Crew.WithAsyncAll helper).

**Non-goals:** Changing any scheduling semantics. The wave scheduler is already the implementation.

---

### 3.4 D-S10 — Stream native tool-call partial JSON

**Deferred because:** `CallWithTools` returns `*ToolCallResponse` with complete `ToolCalls []ToolCall`. Streaming partial tool-call argument JSON means parsing incomplete JSON incrementally per provider, which is hard and provider-specific.

**Current state (D-S4):** Native `CallWithTools` rounds are buffered. Final text-only turn could stream, but that's not yet wired.

**Unblock conditions:**
- Product demand for real-time tool-call UIs (function name + partial args as they arrive).
- At least one provider's wire format confirmed to send partial `arguments`.

**Design space when started:**

| Question | Options | Will mint |
|----------|---------|-----------|
| Chunk shape | (A) extend StreamChunk with `ToolCallDelta` fields (B) new `ToolCallStreamChunk` type (C) reuse `Delta` with Attrs marker | D-ST1 |
| Buffer assembly | (A) provider buffers partials and emits on complete (B) pass partials through | D-ST2 |
| Final answer turn | (A) stream when no tool_calls (B) still buffered | D-ST3 |
| Provider scope | (A) OpenAI only (B) all four | D-ST4 |

**Non-goals:** Streaming `FunctionCall` for models that don't emit them, changing D-S4 buffered behavior for existing CallWithTools paths without an additional stream method.

---

### 3.5 D-J9 — `unevaluatedProperties` / `unevaluatedItems`

**Deferred because:** correct implementation needs an **annotation model** — tracking which properties/items a schema already "evaluated" across applicators (`properties`, `allOf`, `$ref`, `if`/`then`/`else`, `not`). Our validator currently accumulates errors without tracking which fields were consumed.

**Current state:** `WithStrictSchema` rejects schemas containing these keywords. At runtime, they're silently ignored (schema walks them as known-but-non-functional).

**Design space when started:**

| Question | Options | Will mint |
|----------|---------|-----------|
| Annotation scope | (A) full draft-2020-12 annotation (B) minimal "evaluated set" per object/array | D-JE1 |
| Interaction with `$ref` siblings | (A) evaluated set is per-node only (B) merged across $ref resolution | D-JE2 |
| False schema as value | (A) `unevaluatedProperties: false` (B) only schema maps | D-JE3 |

**Non-goals:** Full annotation vocabulary output. We only need "is this property/item already handled?"

---

### 3.6 O-J2 — `format: time`

**Closed 2026-08-21 (D-JT1 = Ship).** JSON Schema `format: time` per RFC 3339
section 5.6. Accepts `HH:MM:SS[.fff]` with optional `Z`/offset
(`14:30:00`, `14:30:00Z`, `14:30:00.5+02:00`); date-time strings are
rejected. Implementation tries `time.Parse("15:04:05.999999999Z07:00")`
then fallback without offset so a bare `14:30:00` is valid. Implemented
in `schema.go`.

---

## 4. Decision allocation

| Item | New IDs minted here | When |
|------|-------------------|------|
| P-XAI-OAUTH | **D-XA1**, maybe **D-XA2** | On xAI docs publication |
| M5 | **D-MT1–D-MT5** | On product request |
| A5 | **D-A7** | On demand signal |
| D-S10 | **D-ST1–D-ST4** | On provider spike + product demand |
| D-J9 | **D-JE1–D-JE3** | On annotation model spike |
| ~~O-J2~~ | **D-JT1** | **Closed 2026-08-21 (ship)** |

All new IDs go into the right family table in [`DECISIONS.md`](DECISIONS.md) when closed.

---

## 5. What unblocks what

```
P-XAI-OAUTH ──► waits on xAI ──► D-XA1 close
M5 ──► waits on product request ──► D-MT* close → implement
A5 ──► waits on user confusion signal ──► D-A7 → alias only
D-S10 ──► waits on product + provider spike ──► D-ST* → new stream path
D-J9 ──► waits on schema demand ──► D-JE* → annotation model
~~O-J2~~ ──► **done** ──► D-JT1 in checkFormat (this patch)
```

None of them block each other or any P3 item.

---

## 6. Order of implementation (if all unblocked tomorrow)

Recommended order (smallest first):

| # | Item | Why first |
|---|------|-----------|
| 1 | ~~**O-J2**~~ — `format: time` | **Done** this patch (D-JT1) |
| 2 | **P-XAI-OAUTH** | Constant-level change, no interface risk |
| 3 | **A5** — `Process=DAG` | Naming sugar, ~10 lines, additive |
| 4 | **M5** — memory tools | Medium complexity; two small tools + policy decision |
| 5 | **D-S10** — tool-call partial stream | High complexity; touches provider wire protocols |
| 6 | **D-J9** — unevaluated* | Highest complexity; needs annotation model |

Item 1 shipped in this patch. Items 2–3 are candidates for a minor when unblocked. Items 4–6 need their own epic-level design before code.

---

## 7. Documentation plan

When each item starts:

| Doc | Update |
|-----|--------|
| [`DECISIONS.md`](DECISIONS.md) | Close item, add new IDs |
| `PLAN.md` / PT | Uncheck backlog row or mark shipped |
| `docs/` | Guide for the feature + SECURITY note if applicable |
| `CHANGELOG` + PT | What shipped |

---

## 8. Summary

Six items, one blocked (xAI), five deferred. All have clear design space and decision IDs pre-allocated in [`DECISIONS.md`](DECISIONS.md) §9. When an unblock condition is met, the sub-plan is written here or in its own `PLAN.<item>.md`, and coding starts only after the new decisions close.
