# Plan — Security & Code-Review Residuals (post v0.4.x / PR #27)

> **Languages:** **English** (current) · [Português](PLAN.security-residuals.pt-BR.md)
>
> **Status:** Draft for implementation (plan only — do not implement until scheduled)
> **Scope:** Close the eight residual items from the post-merge code/security
> review of `feature/mcp-tool-call-progress-warnings` (merged as PR #27 /
> `a8f01b7`). Items range from small hardening defaults to larger feature
> work (real agent delegation, hybrid structured+tools).
>
> **Related:** `SECURITY.md`, `docs/en/mcp.md`, `Plan/PLAN.md` §6,
> `Plan/PLAN.structured-output.md`, `Plan/PLAN.guardrails.md`.

---

## 1. Goals and Constraints

### 1.1 Goals

1. Turn the eight review residuals into a **phased, prioritized backlog**
   with clear APIs, tests, docs, and non-goals.
2. Prefer **safe defaults** and **opt-in expansion** over breaking changes.
3. Keep every change **stdlib-only**, hermetically testable, and bilingual
   in docs (EN + PT-BR).
4. Separate **hardening** (timeouts, path policy, logging, concurrency
   contracts) from **product features** (delegation tool, structured+tools
   hybrid, schema keywords).

### 1.2 Constraints (must be preserved)

| Constraint | Enforcement |
|---|---|
| Zero external dependencies | `go.mod` unchanged; no JSON-Schema libraries. |
| LLM-agnostic | No provider-specific hacks outside `llm/*`. |
| English comments, no accents | godoc, errors, prompts, identifiers. |
| Hermetic tests | `llm/mock` + `httptest`; no real network. |
| Sentinel errors + `context.Context` | New sentinels in `errors.go`; `ctx` on all I/O. |
| Backward compatible by default | Safe defaults may change only where current behavior is unsafe or undefined; document any opt-out. |
| Do not weaken trust boundaries | MCP remains a **trusted-server** model unless an explicit untrusted mode is designed later (out of this plan's P0). |

### 1.3 Residual inventory

| # | Residual | Severity | Nature | Suggested phase |
|---|---|---|---|---|
| R1 | MCP default client has **no timeout** (`http.DefaultClient`) | Medium | Hardening default | **P0** |
| R2 | MCP **trusted-server** / prompt-injection surface | Medium (ops) | Policy + light mitigations | **P0** (docs) / **P1** (optional guards) |
| R3 | `Task.OutputFile` is a **free path** (mode `0600` only) | Medium | Path policy | **P0** |
| R4 | JSON Schema validator is a **subset** | Low–Med | Feature completeness | **P2** |
| R5 | Debug logs include LLM output / tool args; redactor only in example | Medium (ops) | Logging hygiene | **P1** |
| R6 | `Crew.Kickoff` **not safe** for concurrent reuse of the same `Crew` | Medium | Concurrency contract | **P1** |
| R7 | `AllowDelegation` still **informational** (no coworker tool) | Low (feature gap) | Product feature | **P2** |
| R8 | Structured path **bypasses tools** (except `WithToolCall` emit) | Med (capability) | Product feature | **P2** |

---

## 2. Current state (baseline after PR #27)

| Area | Today |
|---|---|
| MCP client | `mcp.New` defaults to `http.DefaultClient` (no timeout). `WithHTTPClient` exists. Docs already warn. |
| MCP trust | Docs/`SECURITY.md` say "treat as trusted". No tool-description sanitizer, no allowlist beyond what the caller attaches. |
| OutputFile | `task.setOutput` → `os.WriteFile(path, data, 0o600)` with **no** path clean/jail. Documented as trusted. |
| Schema | `schema.go` supports `type`, `properties`, `required`, `enum`, `items` only. Explicitly documented. |
| Logging | Core uses `log/slog`. `redactError`/`redactString` for **errors** only. Full redacting `slog.Handler` lives in `examples/logging/` only. Debug logs can include full LLM output and tool args. |
| Kickoff concurrency | Logger/progress "set before Kickoff" is documented. `interpolate` mutates task fields; `mem` is created inside Kickoff; **no** re-entrancy / single-flight guard on the same `*Crew`. Parallelism **inside** staged process is intentional and OK. |
| Delegation | Hierarchical manager picks one agent via LLM (`crew.delegate`). `Agent.AllowDelegation` is documented as informational. No in-reasoning "ask coworker" tool. |
| Structured | `executeTaskDefault`: if `t.Structured != nil` → `executeStructured` and **return** (no ReAct/native tools). `WithToolCall()` only uses a synthetic `emit_result` tool for JSON extraction. |

---

## 3. Phase P0 — Hardening defaults (small, ship soon)

**Theme:** make the safe path the default path without breaking careful callers.

### 3.1 R1 — MCP default HTTP timeout

#### Problem
`http.DefaultClient` has no timeout. A hung MCP server stalls `Initialize` /
`ListTools` / `CallTool` until the process is killed or `ctx` is cancelled
**and** the transport honors it. Callers who forget `WithHTTPClient` inherit
this.

#### Proposal

```go
// mcp/client.go
const DefaultHTTPTimeout = 30 * time.Second

// New: if no WithHTTPClient was supplied, use:
httpClient: &http.Client{Timeout: DefaultHTTPTimeout}

// WithHTTPClient(hc):
//   - nil → ignore (keep current / default)  [already true]
//   - non-nil → use as-is, even if Timeout == 0 (explicit opt-out)
```

Also apply the same default inside `LoadConfig` when building clients
(today it goes through `New`, so one change covers both).

#### Compatibility
- **Behavioral change** for callers relying on infinite hang. Acceptable:
  document in CHANGELOG under *Changed*; provide escape hatch
  (`WithHTTPClient` with `Timeout: 0`, `WithHTTPTimeout(0)`, or JSON
  `"timeout": "0s"`).
- Tests that assumed `c.httpClient == http.DefaultClient` must be updated
  to assert timeout default instead (see `TestNew_Defaults`,
  `TestWithHTTPClient_NilIgnored`).

#### Configuration (decided — D1)
- **Default:** 30s.
- **Programmatic:** `WithHTTPClient` and/or `WithHTTPTimeout(d)`.
- **JSON (`LoadConfig`):** optional per-server `timeout` (Go duration string).
  See §12 D1 addendum for schema and precedence.

#### Tests
- Default client has `Timeout == DefaultHTTPTimeout` (30s).
- `WithHTTPClient` custom client is preserved (including `Timeout: 0`).
- `WithHTTPTimeout` applies on the default-client path only.
- JSON: omit → 30s; `"45s"` → 45s; `"0s"` → 0; invalid → error naming server.
- Hung handler + short client timeout returns error (`httptest` that never
  writes; cancel via client timeout, not only ctx).
- Package `mcp` coverage remains ≥ 90%.

#### Docs
- `docs/en/mcp.md`, `docs/pt-BR/mcp.md`, `SECURITY.md`, CHANGELOG EN/PT:
  default 30s; programmatic + JSON configuration; how to disable;
  still recommend ctx deadlines.

#### Non-goals (R1)
- Per-RPC timeouts distinct from client timeout.
- Retry/backoff policy.
- Replacing the transport with a custom `RoundTripper` by default.

---

### 3.2 R3 — OutputFile path policy

#### Problem
`OutputFile` is attacker-influenced only if the **application** lets
untrusted input set it. Still, a misconfigured app can overwrite
sensitive paths. Mode `0600` does not constrain location.

#### Proposal (defense in depth, opt-in jail + always-on hygiene)

**Always on (non-breaking hygiene):**
1. Reject empty path after trim.
2. `path = filepath.Clean(path)`.
3. Reject paths that escape when a jail is set (see below).
4. Keep mode `0600`.
5. Optional: create parent dirs only if explicitly opted in
   (`WithOutputFileMkdir` — default **off** to preserve today).

**Opt-in jail (recommended for apps that accept external config):**

```go
// WithOutputDir restricts OutputFile to files under dir (after Clean +
// EvalSymlinks of the dir — D2 decided: always when jail set).
func WithOutputDir(dir string) /* task option or Crew-level default */

// Evaluation algorithm (setOutput) — D2 = A:
//   clean := filepath.Clean(t.OutputFile)
//   if jail != "" {
//       absFile := Abs(clean); absJail := Abs(jail)
//       // EvalSymlinks both sides; on error → ErrOutputPathRejected (fail closed)
//       // require absFile == absJail || strings.HasPrefix(absFile, absJail+sep)
//   }
//   os.WriteFile(clean, data, 0o600)
```

**Crew-level default (optional):**
`Crew.OutputDir string` applied when task has `OutputFile` set and task-level
jail is empty — convenient for staged pipelines.

New sentinel:

```go
var ErrOutputPathRejected = errors.New("crewai: output path rejected")
```

#### Compatibility
- No jail configured → same write behavior after `Clean` (document that
  relative paths are cleaned). Avoid changing relative-path resolution
  host CWD semantics beyond `Clean`.
- Jail configured → writes outside fail closed with `ErrOutputPathRejected`.

#### Tests
- Clean of `foo/../bar` → `bar`.
- Jail `/tmp/out`: allow `/tmp/out/a.txt`; reject `/tmp/out/../secret`,
  `/etc/passwd`, symlink escape if `EvalSymlinks` enabled.
- Mode remains `0600` (stat after write in temp dir).
- Concurrent `setOutput` still mutex-safe.

#### Docs
- `docs/tasks.md` / pt-BR, `SECURITY.md`: trusted path by default;
  recommend `OutputDir` when config is external; never pass model output
  into `OutputFile` unchecked.

#### Non-goals (R3)
- Full OS sandbox / chroot.
- Streaming writes or append mode.
- Encrypting output at rest.

---

### 3.3 R2 (P0 slice) — Trusted MCP: document + caller-side scope only

#### Problem
A compromised MCP server can return tool **descriptions** that jailbreak
the model, or tool **results** that exfiltrate prior context. This is
inherent to "tools are untrusted content inside an LLM prompt."

#### P0 proposal (no API break)
1. Expand `docs/en/mcp.md` + pt-BR + `SECURITY.md` with a short **Threat
   model** section:
   - Server is trusted code equivalent.
   - Scope tools per agent (minimum catalog).
   - Do not log raw MCP headers; already true.
   - Prefer network allowlists / private endpoints at deploy layer.
2. Add a **checklist** for operators (timeout, TLS, token file `0600`,
   least-privilege tool attach).
3. Cross-link from README MCP section.

#### P0 non-goals
- Automatic sanitization of tool descriptions (easy to get wrong; P1).
- MCP server allowlist inside the library (caller's job at P0).

---

## 4. Phase P1 — Contracts & operator safety

### 4.1 R5 — Logging redaction in core

#### Problem
`redactError` covers error strings. Debug attributes (`args`, full model
text) still flow to handlers. Production users must copy
`examples/logging` redactor by hand.

#### Proposal

Promote a minimal, opt-in helper to the **root package** (D3 decided):

```go
// RedactHandler wraps inner and applies redactString to all string
// attributes and to the record message. It does not drop fields.
func RedactHandler(inner slog.Handler) slog.Handler

// Typical wiring:
// crew.WithLogger(slog.New(RedactHandler(
//     slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}),
// )))
```

Implementation notes:
- Reuse `redactString` patterns (API keys, Bearer, query secrets).
- Do **not** change default logger behavior (still no redaction) — opt-in
  preserves performance and avoids surprising field mutation in tests.
- Document that redaction is **best-effort**, not a confidentiality boundary.
- Optionally lower default Verbose mapping guidance: prefer
  `LevelInfo` in prod; `LevelDebug` only locally.

#### Stretch (still P1 if cheap)
- Cap logged tool-arg length in native/ReAct debug lines
  (`truncateForLog(s, n)`) independent of redaction.

#### Tests
- Handler masks `sk-...`, `Bearer ...`, `access_key=` in attrs and message.
- `WithAttrs` / `WithGroup` preserve wrapper type.
- `Enabled()` delegates to inner.

#### Docs
- README Logging section: one-liner to enable `RedactHandler`.
- Keep `examples/logging` but make it a thin wrapper over the core helper
  (avoid drift).
- `SECURITY.md` hardening notes updated.

#### Non-goals (R5)
- Field-level allowlists / structured secret types.
- Encrypting logs.
- Automatically redacting `Progress` payloads (already metadata-only).

---

### 4.2 R6 — Kickoff single-flight / reuse contract

#### Problem
Reusing the same `*Crew` across concurrent `Kickoff` calls races on
logger init, memory init, task interpolation, and output aggregation.
Even sequential reuse mutates task descriptions via `interpolate`.

#### Proposal

**Document the contract** (P1.a) and **enforce single-flight** (P1.b) —
**D4 decided: TryLock + ErrCrewRunning** (fail fast, do not queue):

```go
type Crew struct {
    // ...
    runMu sync.Mutex // held for the duration of Kickoff
}

func (c *Crew) Kickoff(ctx context.Context, inputs map[string]string) (*CrewOutput, error) {
    if !c.runMu.TryLock() {
        return nil, ErrCrewRunning
    }
    defer c.runMu.Unlock()
    // ... existing body ...
}
```

Go 1.24 has `sync.Mutex.TryLock` — OK with current toolchain.

New sentinel:

```go
var ErrCrewRunning = errors.New("crewai: crew already running")
```

**Interpolation safety (P1.c — optional follow-up):**
- Snapshot task description/expected output into locals before mutate, **or**
- Document that sequential Kickoff re-applies `inputs` on already-interpolated
  text (footgun if placeholders were expanded). Prefer: interpolate from a
  preserved `rawDescription` if we add one later — **non-goal for P1** unless
  tests show breakage.

#### Tests
- Two goroutines calling `Kickoff`: one succeeds, one gets `ErrCrewRunning`.
- Sequential Kickoff still works.
- Staged parallel tasks **inside** one Kickoff still race-free (`-race`).

#### Docs
- `docs/crews.md`: "one Kickoff at a time per Crew value; create separate
  Crews for parallel runs."
- godoc on `Kickoff`.

#### Non-goals (R6)
- Making a single Crew fan-out multiple Kickoffs safely with isolated
  task clones (larger redesign).
- Deep copy of Agents/Tools graphs.

---

### 4.3 R2 (P1 slice) — Optional MCP tool-catalog guards

#### Proposal (opt-in)

```go
// mcp package
type AdapterOption func(*ToolAdapter)

// WithDescriptionLimit truncates Description() to n runes (default 0 = no limit).
func WithDescriptionLimit(n int) AdapterOption

// FilterTools keeps only tools whose names are in allow.
func FilterTools(tools []Tool, allow map[string]struct{}) []Tool
```

Also: strip ASCII control chars from descriptions when limit wrapper is on.

#### Tests
- Description truncated; name filter drops others.
- No default behavior change when options omitted.

#### Docs
- MCP security section: when to use allowlists; still not a substitute for
  trusting the server endpoint.

---

## 5. Phase P2 — Capability residuals (features)

### 5.1 R4 — JSON Schema validator expansion

#### Problem
Subset blocks real-world schemas from tools/OpenAPI-ish sources
(`additionalProperties: false`, numeric bounds, `pattern`, `oneOf`).

#### Proposal (incremental keywords, still stdlib)

Add in this order (each keyword behind tests + docs):

| Keyword | Behavior sketch |
|---|---|
| `additionalProperties` | `false` → reject unknown object keys; object schema → validate extras; default absent = allow (JSON Schema draft behavior). |
| `minLength` / `maxLength` | strings in **bytes** (`len(s)` — **D7 decided**); document in godoc + tasks docs. |
| `minimum` / `maximum` (+ `exclusiveMinimum` / `exclusiveMaximum`) | numbers via `float64` with documented precision limits. |
| `minItems` / `maxItems` | arrays. |
| `pattern` | `regexp.Compile` with size cap on pattern length; prefer schema-load-time error when `StrictSchema`. |
| `oneOf` / `anyOf` | validate against alternatives; `oneOf` requires exactly one match. |
| `allOf` | all must match (compose). |

Keep an **explicit non-support list** in godoc for `$ref`, `if/then/else`,
`unevaluatedProperties`, and most `format` values (`format` = P2 stretch,
default off).

#### API
- No API break: `validateSchema` gains keywords.
- Optional `StrictSchema bool` on `StructuredOutput` to error at construction
  if unsupported keywords are present (fail-fast for authors).

#### Tests
- Table-driven per keyword; repair loop still surfaces multi-error text.
- Invalid `pattern` at `NewStructuredOutput` when `StrictSchema`.

#### Docs
- `docs/tasks.md` supported keyword table update EN/PT.
- CHANGELOG *Added*.

#### Non-goals (R4)
- Full draft 2020-12 compliance.
- External lib (`santhosh-tekuri/jsonschema`, etc.).
- Binary/encoded content validation.

---

### 5.2 R8 — Structured output **with** prior tool use

#### Problem
`executeTaskDefault` short-circuits to `executeStructured` whenever
`Structured != nil`, so agents cannot gather facts via tools and then emit
validated JSON in one task (unless the user splits tasks).

#### Proposal

```go
type StructuredOutput struct {
    // ... existing ...

    // AllowTools, when true, runs a bounded tool loop (ReAct or native
    // per Agent.ToolMode) BEFORE the structured capture phase. Tools
    // cannot be called during the JSON-only capture call itself unless
    // ToolCall mode is also set (emit_result only).
    AllowTools bool
}

func WithAllowTools() func(*StructuredOutput) {
    return func(s *StructuredOutput) { s.AllowTools = true }
}
```

**Execution pipeline when `AllowTools`:**

1. **Gather phase** — existing ReAct or `executeTaskWithTools` with a
   hard iteration cap (`MaxIterations`), but final answer is **not**
   required to be JSON. Stop when:
   - model emits `Final Answer`, or
   - native path returns content without `tool_calls`, or
   - iterations exhausted → **D6 decided:** continue to capture with
     whatever context was collected and `Task.AddWarning("gather budget exhausted")`.
2. **Capture phase** — existing `executeStructuredJSON` or
   `executeStructuredToolCall` with messages/context including:
   - task prompt
   - compressed tool transcript (truncated; subject to
     `MaxToolOutputBytes` already)
   - schema instructions
3. Facts from gather phase **merge** into task facts (today structured
   returns `nil` facts — fix: plumb `[]Fact` out of gather).

**Defaults:** `AllowTools == false` preserves today's behavior.

#### Interaction with agentic loop
- If `Task.Loop` / `Agent.Loop` is set, loop remains outer strategy;
  document that `AllowTools` applies inside the execute step the loop
  calls (no double-plan). Exact wiring: `executeTask` order stays
  Loop → default; structured-with-tools lives inside default.

#### Tests
- Mock LLM: tool call then JSON → facts non-empty + schema valid.
- `AllowTools false` → tools never invoked (spy tool).
- Repair loop still works after gather.
- Native + `WithToolCall` + `AllowTools` matrix (table-driven).

#### Docs
- `docs/tasks.md` structured section: two-phase diagram.
- Example snippet (optional `examples/structured_tools` later).

#### Non-goals (R8)
- Calling arbitrary tools **during** JSON repair calls.
- Multi-schema routing.
- Streaming structured tokens.

---

### 5.3 R7 — Real inter-agent delegation tool

#### Problem
`AllowDelegation` does not expose a tool an agent can invoke mid-reasoning
to ask a coworker. Hierarchical process only assigns the whole task once.

#### Proposal

```go
// NewDelegationTool returns a Tool that asks a peer agent to solve a
// sub-question. Requires a DelegationRoster.
func NewDelegationTool(roster DelegationRoster) Tool

type DelegationRoster interface {
    // Agents returns peers eligible for delegation (excluding self).
    Agents() []*Agent
}
```

Tool contract:
- Name: `delegate_to_coworker`
- Input JSON: `{"coworker": "<role>", "request": "<text>", "context": "<optional>"}`
- Behavior:
  1. Resolve coworker by exact `Role` match among roster.
  2. Require `AllowDelegation == true` **on the target** so operators opt
     agents in as eligible coworkers.
  3. Run a **nested** `coworker.Execute` / single-task execution with
     depth budget.
  4. Return observation string (truncated).

**Depth / recursion guards:**

```go
const DefaultMaxDelegationDepth = 2

// ctx value incremented each nested call; exceed → error observation
// Cycles: role A → B → A blocked via stack in ctx.
```

**Who attaches the tool (D5 decided = C):**
- Explicit always works: `agent.WithTools(NewDelegationTool(crew))`.
- Opt-in auto via `Crew.EnableDelegationTool bool` (**default false**).
  When true (typically hierarchical), attach `delegate_to_coworker` to
  eligible agents. Targets still require `AllowDelegation == true`.

#### Tests
- Happy path mock coworkers.
- Unknown coworker → observation error (not Go panic).
- Depth exceeded / cycle.
- Target without `AllowDelegation` rejected.
- `-race` with staged + delegation.

#### Docs
- `docs/agents.md`, `docs/crews.md`: semantics of `AllowDelegation`
  updated from "informational" to "eligible delegation target".
- Example `examples/delegation`.

#### Non-goals (R7)
- Full multi-agent conversation bus.
- Pay-per-delegation budgeting beyond depth.
- Replacing hierarchical manager assignment.

---

## 6. Cross-cutting implementation rules

1. **Order of work:** P0 → P1 → P2. Do not start R7/R8 while P0 defaults
   are open.
2. **One PR per residual (or tight pair)** — e.g. R1 alone; R3 alone;
   R5+docs; R6 alone; R4 keyword batches; R8; R7.
3. **CHANGELOG** EN + PT-BR under *Security* / *Added* / *Changed* as
   appropriate.
4. **Bilingual docs** in the same PR as the code (see §6.1).
5. **`go test -race ./...`**, `gofmt`, `go vet` clean; pre-commit
   govulncheck remains local.
6. **No `//nolint`** for gosec unless false positive is documented in
   PLAN (existing OAuth G304/G117 policy).
7. **Code review + coverage gates** on every PR (see §6.1) — non-negotiable.

### 6.1 Standing quality gates (every PR A–J)

These gates apply to **every** residual PR. A PR that fails any gate is
not mergeable.

| Gate | Requirement | How to verify |
|---|---|---|
| **Code review** | Self-review checklist filled in the PR body (see template below). Prefer a second human review when the change touches trust boundaries (MCP, paths, delegation, schema). | PR template section "Code review" |
| **Coverage ≥ 90%** | Package(s) touched by the PR must stay at **≥ 90%** statement coverage. Do not lower a package that is already above 90%. New packages start ≥ 90%. | `go test ./<pkg>/... -cover` and `go test ./... -cover` |
| **Race-clean** | `go test -race` on touched packages (and `./...` before merge). | CI + local |
| **Docs EN + PT-BR** | User-facing behavior, new APIs, security notes, and CHANGELOG updated in **both** languages in the same PR. Godoc in English only. | Diff includes `docs/**`, `docs/pt-BR/**`, `CHANGELOG.md`, `CHANGELOG.pt-BR.md` as needed |
| **gofmt / go vet** | Clean. | CI + pre-commit |
| **No new deps** | `go.mod` require block unchanged. | `git diff go.mod` |

**Baseline at plan time (post PR #27, approximate):**

| Package | Coverage |
|---|---|
| `github.com/rhgs/crewai-go` (root) | ~96.7% |
| `mcp` | ~90.5% |
| `tools` | ~91.2% |
| `llm/openai` | ~92.6% |
| `llm/ollama` / `llm/xai` | ~90.7% |
| `llm/anthropic` | ~89.2% ⚠️ below 90% — any PR touching it must raise it to ≥ 90% or not worsen it *and* add a follow-up note; **prefer bring to ≥ 90%** |
| `llm/mock` | ~93.3% |

**Coverage rules (strict):**
1. If you touch package P and P is ≥ 90%, the PR must leave P ≥ 90%.
2. If you touch package P and P is < 90% (today: `llm/anthropic`), the PR
   **must** bring P to ≥ 90% or be split so the coverage fix lands first.
3. Examples (`examples/**`) are exempt from the 90% gate (no test files by
   design) but must still build (`go build ./examples/...`).
4. Prefer table-driven unit tests + `httptest`; no live network.

**PR body — Code review checklist (copy into every PR):**

```markdown
## Code review

- [ ] Trust boundary reviewed (paths, network, tool results, logs)
- [ ] Errors are sentinels / wrapped; no secret material in error strings
- [ ] ctx canceled/deadline honored on new I/O
- [ ] No unbounded reads/writes (LimitReader / size caps where applicable)
- [ ] Concurrency: no data races; single-flight / mutex documented
- [ ] Backward compatible or CHANGELOG *Changed* + migration note
- [ ] Tests cover happy path, failure path, and edge/escape cases
- [ ] Coverage on touched packages ≥ 90% (`go test -cover`)
- [ ] Docs EN + PT-BR + godoc updated
- [ ] `gofmt` / `go vet` / `go test -race` clean
```

---

## 7. Suggested API / file touch map

| Residual | Primary files | Tests | Docs |
|---|---|---|---|
| R1 | `mcp/client.go`, `mcp/config.go` | `mcp/client_test.go` | `docs/en/mcp.md`, pt-BR, SECURITY, CHANGELOG |
| R2 | docs (+ later `mcp/adapter.go`) | adapter tests if options | MCP + SECURITY |
| R3 | `task.go`, maybe `crew.go`, `errors.go` | `task_*_test.go` / new | tasks EN/PT, SECURITY |
| R4 | `schema.go` | `schema_*_test.go` | tasks EN/PT |
| R5 | `redact.go` (handler), maybe `log.go` | `redact_test.go` | README logging, example |
| R6 | `crew.go`, `errors.go` | `crew_*_test.go` | crews EN/PT |
| R7 | `delegation.go` (new), `agent.go`, `tool.go` | `delegation_test.go` | agents/crews, example |
| R8 | `structured.go`, `executor.go` | `structured_*_test.go` | tasks EN/PT |

---

## 8. Acceptance criteria (definition of done per residual)

### P0
- [ ] R1: default MCP client timeout 30s; escape hatch documented; tests green.
- [x] R3: path `Clean` always; optional `OutputDir` jail; `ErrOutputPathRejected`; tests for escape attempts.
- [x] R2-P0: threat model + operator checklist in MCP docs EN/PT + SECURITY.

### P1
- [x] R5: `RedactHandler` in core; example delegates to it; README snippet.
- [x] R6: concurrent Kickoff returns `ErrCrewRunning`; docs state single-flight.
- [x] R2-P1 (optional): description limit + name filter helpers.

### P2
- [x] R4: at least `additionalProperties`, bounds, `pattern`, `oneOf`/`anyOf` tested.
- [x] R8: `WithAllowTools` two-phase path; facts preserved; default unchanged.
- [x] R7: `delegate_to_coworker` tool + depth/cycle guards; `AllowDelegation` meaning updated.

### Global
- [ ] `go test -race ./...` pass; bilingual docs; CHANGELOG updated; no new deps.
- [ ] Code review checklist (§6.1) completed on the PR.
- [ ] Touched packages stay at **≥ 90%** coverage (`go test -cover`); `llm/anthropic` raised to ≥ 90% if touched.

---

## 9. Non-goals (entire plan)

- Breaking the zero-dependency rule.
- Full JSON Schema draft compliance.
- Untrusted multi-tenant MCP marketplace inside the library.
- OS-level sandboxing for tools or `OutputFile`.
- Streaming LLM APIs (separate roadmap item in `PLAN.md`).
- Long-term memory / embeddings (separate roadmap item).
- Automatic secret scanning of model outputs beyond log redaction.

---

## 10. Rollout & versioning sketch

| Slice | Version hint | Notes |
|---|---|---|
| P0 (R1, R3, R2 docs) | `v0.5.x` patch/minor | Default timeout is a mild *Changed*; jail opt-in. |
| P1 (R5, R6, R2 guards) | `v0.5.x` / `v0.6.0` | Additive APIs + single-flight error. |
| P2 (R4, R8, R7) | `v0.6.0+` | Feature minor; advertise in README. |

Exact version numbers left to release process (see also: still-open
"tag Unreleased as v0.5.0" hygiene item outside this plan).

---

## 11. Work breakdown (implementation checklist)

### PR-A — MCP default timeout (R1) — D1 decided: 30s + prog + JSON
1. Add `DefaultHTTPTimeout = 30s`; change `New` default client.
2. Add `WithHTTPTimeout(d time.Duration)`; wire JSON `timeout` in config/`LoadConfig`.
3. Unit tests + hang test + JSON duration table.
4. Docs EN+PT + SECURITY + CHANGELOG; `mcp` coverage ≥ 90%; code review checklist.

### PR-B — OutputFile policy (R3) — D2 decided: EvalSymlinks on in jail
1. `Clean` + reject empty; add `ErrOutputPathRejected`.
2. Task/Crew `OutputDir`; always `EvalSymlinks` when jail set; fail closed.
3. Tests (escape, symlink, mode 0600) + docs EN/PT + SECURITY; coverage ≥ 90%; code review.

### PR-C — MCP threat model docs (R2-P0)
1. Docs only EN/PT + SECURITY checklist.

### PR-D — RedactHandler (R5) — D3 decided: root package
1. Add `crewai.RedactHandler` in root; tests; example thin-wrap; README EN/PT.
2. Coverage ≥ 90% on root; code review checklist.

### PR-E — Kickoff single-flight (R6) — D4 decided: ErrCrewRunning
1. `TryLock` + `ErrCrewRunning`; concurrent race test; sequential still OK.
2. Docs crews EN/PT + godoc; coverage ≥ 90%; code review.

### PR-F — MCP catalog guards (R2-P1, optional)
1. Adapter options + `FilterTools`; tests; docs.

### PR-G — Schema keywords batch 1 (R4) — D7 decided: length in bytes
1. `additionalProperties`, length/item bounds (`minLength`/`maxLength` = bytes); tests; docs EN/PT.
2. Coverage ≥ 90%; code review.

### PR-H — Schema keywords batch 2 (R4)
1. `pattern`, `oneOf`/`anyOf`/`allOf`; `StrictSchema`; tests; docs.

### PR-I — Structured + tools (R8) — D6 decided: warn + capture
1. `AllowTools` pipeline; on gather exhaust → `AddWarning` then capture; facts plumbing.
2. Matrix tests; docs EN/PT; coverage ≥ 90%; code review.

### PR-J — Delegation tool (R7) — D5 decided: EnableDelegationTool opt-in
1. Tool + depth/cycle; `Crew.EnableDelegationTool` (default false) + explicit WithTools.
2. `AllowDelegation` = eligible target; example; docs EN/PT; coverage ≥ 90%; code review.

---

## 12. Open decisions — decide before implementation

> Status legend: ⏳ pending · ✅ decided
>
> **All D1–D7 decided (2026-08-20).** Implementation PRs A–J may proceed in
> phase order. Quality gates in §6.1 always apply.

### D1 — R1 MCP default HTTP timeout ✅

| | |
|---|---|
| **Question** | Default client timeout when caller omits `WithHTTPClient`? |
| **Option A** | **30s** — fail faster; good for interactive apps; cold MCP servers may need override |
| **Option B** | **60s** — friendlier to cold-start remote MCP; hung servers linger longer |
| **Option C** | **No default timeout** but document harder (rejects the residual) |
| **Escape hatch (all A/B)** | `WithHTTPClient(&http.Client{Timeout: 0})` or custom client |
| **Recommendation** | **A — 30s**. Matches common HTTP defaults, forces explicit long timeouts, pairs with ctx deadlines. Document cold-start override. |
| **Decision** | **A — 30s**, configurable **both programmatically and via JSON** (see addendum). |
| **PR** | A |
| **Date** | 2026-08-20 |

**Decided configuration surface (D1 addendum):**

1. **Default:** `DefaultHTTPTimeout = 30 * time.Second` when no custom client is supplied.
2. **Programmatic:**
   - `mcp.WithHTTPClient(&http.Client{Timeout: d})` — full control (including `Timeout: 0` to disable).
   - `mcp.WithHTTPTimeout(d time.Duration)` — convenience option for the library-built default client without requiring the caller to construct an `http.Client`.
   - **Precedence:** a caller-supplied `WithHTTPClient` is used **as-is**. `WithHTTPTimeout` only applies when the library builds the default client (no custom client).
3. **JSON (`LoadConfig` / config file):** extend existing `ServerConfig` (top-level key `servers`) with optional `Timeout` / `json:"timeout"`, e.g.:
   ```json
   {
     "servers": [
       {
         "name": "filesystem",
         "endpoint": "http://127.0.0.1:8080/mcp",
         "headers": { "Authorization": "Bearer …" },
         "timeout": "30s"
       }
     ]
   }
   ```
   - `timeout` accepts a Go duration string (`"30s"`, `"1m"`, `"0s"`).
   - Omitted / empty → `DefaultHTTPTimeout` (30s).
   - `"0s"` / zero → no client timeout (explicit opt-out; ctx deadlines still apply).
   - Invalid duration → `LoadConfig` error naming the server (no secrets in the error).
4. **Docs:** EN + PT MCP guides + SECURITY + CHANGELOG must document default, programmatic options, and JSON field.
5. **Tests / gates:** default 30s; `WithHTTPTimeout`; JSON omit / `"45s"` / `"0s"` / invalid; hang + short timeout; `mcp` coverage ≥ 90%; code review checklist; docs EN+PT.

### D2 — R3 OutputDir symlink evaluation ✅

| | |
|---|---|
| **Question** | When `OutputDir` jail is set, evaluate symlinks before the prefix check? |
| **Option A** | **On by default** inside jail (`filepath.EvalSymlinks` on jail + target) — closes symlink escapes |
| **Option B** | **Opt-in** via `WithOutputDirEvalSymlinks(true)` — fewer surprises on exotic FS |
| **Option C** | **Never** eval symlinks — only `Abs` + prefix (weaker) |
| **Recommendation** | **A — on by default** when jail is configured. Jail is already opt-in; once chosen it should be strong. If `EvalSymlinks` fails (missing parent), fail closed with `ErrOutputPathRejected`. |
| **Decision** | **A — EvalSymlinks on by default** whenever `OutputDir` jail is set. Fail closed on eval error. |
| **PR** | B |
| **Date** | 2026-08-20 |

### D3 — R5 RedactHandler package placement ✅

| | |
|---|---|
| **Question** | Where does `RedactHandler` live? |
| **Option A** | **Root package** `crewai.RedactHandler` — discoverable next to `WithLogger` |
| **Option B** | **Subpackage** e.g. `crewai/logredact` — keeps root API smaller |
| **Recommendation** | **A — root**. One function, no new deps, matches `redactError` already in root. Example becomes a thin demo. |
| **Decision** | **A — root package** `crewai.RedactHandler`. |
| **PR** | D |
| **Date** | 2026-08-20 |

### D4 — R6 concurrent Kickoff behavior ✅

| | |
|---|---|
| **Question** | Second concurrent `Kickoff` on the same `*Crew`? |
| **Option A** | **`TryLock` + `ErrCrewRunning`** — fail fast, caller retries/spawns another Crew |
| **Option B** | **Block** on mutex until the first Kickoff finishes — silent queueing |
| **Recommendation** | **A — error**. Queueing hides overload and complicates ctx cancel. Document: one Kickoff at a time per Crew value. |
| **Decision** | **A — `TryLock` + `ErrCrewRunning`** (fail fast). |
| **PR** | E |
| **Date** | 2026-08-20 |

### D5 — R7 delegation tool attachment ✅

| | |
|---|---|
| **Question** | How do agents get `delegate_to_coworker`? |
| **Option A** | **Explicit only** — `agent.WithTools(NewDelegationTool(crew))` |
| **Option B** | **Auto-attach** in hierarchical process when `AllowDelegation` is set |
| **Option C** | **Opt-in auto** via `Crew.EnableDelegationTool bool` (default false) + explicit still works |
| **Recommendation** | **C — opt-in auto flag, default off**. Safe default (no surprise tools), ergonomic for hierarchical demos when enabled. Targets still require `AllowDelegation`. |
| **Decision** | **C — `Crew.EnableDelegationTool` opt-in (default false)** + explicit `WithTools(NewDelegationTool(...))` still supported. Targets require `AllowDelegation`. |
| **PR** | J |
| **Date** | 2026-08-20 |

### D6 — R8 gather phase exhausted ✅

| | |
|---|---|
| **Question** | If `AllowTools` gather hits `MaxIterations` without a clean stop, what next? |
| **Option A** | **Proceed to capture** with whatever transcript/facts exist (best-effort) |
| **Option B** | **Fail the task** with a sentinel e.g. `ErrGatherBudgetExceeded` |
| **Option C** | **Proceed but `AddWarning`** on the task ("gather budget exhausted") then capture |
| **Recommendation** | **C — proceed + warning**. Structured capture may still succeed; warning preserves observability without being as harsh as B or as silent as A. |
| **Decision** | **C — proceed to capture + `AddWarning`** ("gather budget exhausted"). |
| **PR** | I |
| **Date** | 2026-08-20 |

### D7 — R4 string length units ✅

| | |
|---|---|
| **Question** | `minLength` / `maxLength` count what? |
| **Option A** | **Bytes** (`len(s)`) — stable, matches many JSON Schema validators in practice for ASCII-heavy LLM output |
| **Option B** | **Runes** (`utf8.RuneCountInString`) — better for user-facing Unicode text |
| **Recommendation** | **A — bytes**, documented explicitly in godoc + tasks docs. LLM JSON is usually ASCII keys/values; runes surprise on combining chars. Callers who need graphemes use a guardrail. |
| **Decision** | **A — bytes** (`len(s)`), documented in godoc + tasks docs EN/PT. |
| **PR** | G |
| **Date** | 2026-08-20 |

### Decision log (fill when resolved)

| ID | Residual | Choice | Date | Notes |
|---|---|---|---|---|
| D1 | R1 timeout | **A (30s)** + programmatic + JSON | 2026-08-20 | `WithHTTPTimeout` + config `timeout` duration; `0` disables |
| D2 | R3 symlinks | **A** EvalSymlinks on in jail | 2026-08-20 | fail closed on eval error |
| D3 | R5 package | **A** root `crewai.RedactHandler` | 2026-08-20 | |
| D4 | R6 Kickoff | **A** TryLock + `ErrCrewRunning` | 2026-08-20 | fail fast |
| D5 | R7 attach | **C** `EnableDelegationTool` opt-in default false | 2026-08-20 | explicit WithTools still OK |
| D6 | R8 gather | **C** proceed + `AddWarning` | 2026-08-20 | then capture |
| D7 | R4 length | **A** bytes (`len`) | 2026-08-20 | document in tasks docs |

---

## 13. References

- Merge: PR #27 — https://github.com/rhgs/crewai-go/pull/27
- MCP client default: `mcp/client.go` (`http.DefaultClient`)
- Output write: `task.go` `setOutput`
- Schema subset: `schema.go` `validateSchema` godoc
- Structured short-circuit: `executor.go` `executeTaskDefault`
- AllowDelegation: `agent.go`
- Example redactor: `examples/logging/main.go`
- Operator notes: `SECURITY.md`

---

## 14. Status tracking

| Residual | Phase | PR | Status |
|---|---|---|---|
| R1 MCP timeout | P0 | A | **Implemented** on branch (D1) |
| R2 MCP trust docs/guards | P0/P1 | C/F | **Implemented** (threat model + catalog guards) |
| R3 OutputFile policy | P0 | B | **Implemented** on branch (D2) |
| R4 Schema keywords | P2 | G/H | **Implemented** on branch (D7) |
| R5 RedactHandler | P1 | D | **Implemented** on branch (D3) |
| R6 Kickoff single-flight | P1 | E | **Implemented** on branch (D4) |
| R7 Delegation tool | P2 | J | **Implemented** on branch (D5) |
| R8 Structured+tools | P2 | I | **Implemented** on branch (D6) |
