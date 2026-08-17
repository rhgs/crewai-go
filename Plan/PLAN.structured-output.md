# Plan — Structured Output with JSON Schema Validation and Bounded Repair Loop

> **Status:** Draft for implementation
> **Scope:** Add structured (JSON-validated) output support to `crewai-go`
> tasks, with a bounded repair loop, zero new dependencies, and a non-breaking
> `LLM.Call` interface.

---

## 1. Goals and Constraints

### 1.1 Goals

1. Allow a `Task` to require the model to produce JSON that validates against
   a JSON Schema.
2. Validate the model output in Go using only `encoding/json` plus a minimal
   in-house schema validator (no external libraries).
3. Provide a bounded repair loop: when the output fails validation, re-prompt
   the model with the error details, up to `RepairMax` attempts.
4. Store only the validated, canonicalized JSON as the task output.
5. Never return invalid JSON or invented data; fail explicitly with
   `ErrRepairBudgetExceeded` when the budget is exhausted.

### 1.2 Constraints (must be preserved)

| Constraint | Enforcement |
|---|---|
| Zero external dependencies | `go.mod` unchanged; validator uses only `encoding/json`. |
| LLM-agnostic, non-breaking `LLM.Call` | The `Call(ctx, []Message) (string, error)` signature is never modified. Structured output works by prompt engineering + Go-side validation, not provider-native function calling. |
| English comments, no accents | All godoc, error messages, prompts, and identifiers follow this rule. |
| Hermetic tests | `llm/mock` + `httptest`; no real network. |
| Sentinel errors, context on all I/O, functional options | New sentinels in `errors.go`; `ctx` propagated through the repair loop; functional options for `StructuredOutput`. |
| Concurrency safety | `Task` mutex on `Output()`/`setOutput()` is preserved; schema validation is stateless. |
| Backward compatibility | Existing ReAct executor and unstructured tasks continue to work without any modification. |

---

## 2. New API Surface

### 2.1 `StructuredOutput` type (root package)

```go
// StructuredOutput configures a Task to require JSON output validated
// against a JSON Schema. When non-nil on a Task, the executor instructs
// the model to reply with JSON only, validates the response in Go, and
// retries (repairs) up to RepairMax times if validation fails.
type StructuredOutput struct {
    // Schema is the JSON Schema the output must satisfy. Required.
    // It is stored as raw JSON so the caller can pass any schema object
    // marshaled with encoding/json.
    Schema json.RawMessage

    // RepairMax is the maximum number of repair attempts. If <= 0,
    // it defaults to 2. A repair attempt is one additional LLM.Call
    // after the initial call fails validation.
    RepairMax int
}
```

**Functional option** (optional convenience, consistent with project style):

```go
// WithRepairMax sets the repair attempt limit on a StructuredOutput.
func WithRepairMax(n int) func(*StructuredOutput) {
    return func(s *StructuredOutput) { s.RepairMax = n }
}

// NewStructuredOutput creates a StructuredOutput from a schema (any value
// marshalable by encoding/json) and optional functional options.
func NewStructuredOutput(schema any, opts ...func(*StructuredOutput)) (*StructuredOutput, error)
```

### 2.2 `Task.Structured` field

```go
type Task struct {
    // ... existing fields ...

    // Structured, when non-nil, requires this task to produce JSON output
    // validated against the embedded JSON Schema. The executor enters
    // structured mode and bypasses the ReAct tool-use loop.
    Structured *StructuredOutput
}
```

### 2.3 New sentinel errors

```go
var (
    // ErrInvalidOutput is returned when the model returns JSON that does
    // not validate against the required schema. It wraps the underlying
    // validation error(s).
    ErrInvalidOutput = errors.New("crewai: structured output failed schema validation")

    // ErrRepairBudgetExceeded is returned when repair attempts are
    // exhausted and the model never produced valid JSON. It wraps the
    // last validation error.
    ErrRepairBudgetExceeded = errors.New("crewai: repair budget exceeded, structured output could not be validated")
)
```

---

## 3. Architecture and File Changes

### 3.1 File map

| File | Change type | Description |
|---|---|---|
| `structured.go` | **New** | `StructuredOutput` type, `NewStructuredOutput`, `WithRepairMax`, prompt builders for structured mode, `executeStructured` function. |
| `schema.go` | **New** | Minimal JSON Schema validator (`validateSchema`, `ValidationError` type). Covers `type`, `properties`, `required`, `enum`, `items`. |
| `schema_test.go` | **New** | Unit tests for the validator. |
| `structured_test.go` | **New** | Unit tests for `executeStructured` and the repair loop (using `llmStub` / `llm/mock`). |
| `errors.go` | **Modified** | Add `ErrInvalidOutput` and `ErrRepairBudgetExceeded`. |
| `task.go` | **Modified** | Add `Structured *StructuredOutput` field; add `NewStructuredTask` constructor (optional convenience). |
| `executor.go` | **Modified** | Add a dispatch at the top of `executeTask`: if `t.Structured != nil`, call `executeStructured`; else current ReAct path. |
| `prompts.go` | **Modified** | Add `buildStructuredPrompt` and `buildRepairPrompt` helper builders. |
| `executor_test.go` | **Modified** | Add regression tests for structured dispatch and nil-LLM on structured task. |
| `task_test.go` | **Modified** | Add test for `Task.Structured` field and `NewStructuredTask`. |
| `docs/tasks.md` | **Modified** | New "Structured output" section. |
| `docs/pt-BR/tasks.md` | **Modified** | New "Saida estruturada" section (PT mirror). |
| `README.md` | **Modified** | Brief mention in features list and concepts table. |
| `README.pt-BR.md` | **Modified** | PT mirror. |
| `CHANGELOG.md` | **Modified** | New `[Unreleased]` entry. |
| `CHANGELOG.pt-BR.md` | **Modified** | PT mirror. |

### 3.2 Module dependency graph

```
executor.go
  └──> executeStructured (structured.go)
         ├──> buildStructuredPrompt (prompts.go)
         ├──> buildRepairPrompt    (prompts.go)
         └──> validateSchema        (schema.go)
```

No new import paths; everything stays in the root `crewai` package.

---

## 4. Detailed Design

### 4.1 Minimal JSON Schema Validator (`schema.go`)

#### 4.1.1 Supported schema keywords

| Keyword | Behavior |
|---|---|
| `type` | Validates JSON value type: `object`, `array`, `string`, `number`, `integer`, `boolean`, `null`. |
| `properties` | For `object` type: validates each property listed in `properties` against its sub-schema. Unknown properties are allowed (no `additionalProperties: false` support — documented limitation). |
| `required` | For `object` type: ensures every listed property key exists. |
| `enum` | Ensures the value is equal (by `reflect.DeepEqual` on the unmarshaled value) to one of the enum entries. |
| `items` | For `array` type: validates every element of the array against the sub-schema. |

#### 4.1.2 Types

```go
// ValidationError describes one validation failure.
type ValidationError struct {
    Path    string // JSON pointer-style path, e.g. "/name" or "/items/0"
    Message string // human-readable description
}

func (e *ValidationError) Error() string

// ValidationErrors is a collection of one or more validation errors.
type ValidationErrors []*ValidationError

func (e ValidationErrors) Error() string
// Returns a multi-line string joining all individual errors.
```

#### 4.1.3 Entry point

```go
// validateSchema validates a raw JSON document against a raw JSON Schema.
// It returns nil if the document satisfies the schema, or a
// ValidationErrors (possibly wrapping a single *ValidationError) on
// failure. It uses encoding/json and manual traversal; it is NOT a
// full JSON Schema validator and supports only a subset of keywords
// (type, properties, required, enum, items).
func validateSchema(doc, schema json.RawMessage) error
```

#### 4.1.4 Internal traversal

The validator unmarshals both `doc` and `schema` into `any` (interface{}),
then walks the schema tree recursively:

```
validateNode(value any, schemaNode map[string]any, path string) *ValidationErrors
```

- `type`: check `value` via a type switch. `integer` is satisfied by
  `float64` with no fractional part (JSON numbers in Go unmarshal as
  `float64`).
- `required`: assert the key is present in the `value` map.
- `properties`: recurse into each property sub-schema.
- `enum`: compare `value` against each enum entry with `reflect.DeepEqual`.
- `items`: iterate the array and recurse into `items` sub-schema for each
  element.

All discovered errors are collected (not short-circuited) and returned as a
single `ValidationErrors` slice, so the repair prompt can present the full
list to the model.

### 4.2 Structured Executor (`structured.go`)

#### 4.2.1 `executeStructured`

```go
// executeStructured runs the structured-output loop for a task whose
// Structured field is non-nil. It instructs the model to produce JSON
// only, validates the output against the schema, and retries up to
// RepairMax times if validation fails. It returns the canonicalized
// JSON string on success or a sentinel error on failure.
func executeStructured(ctx context.Context, a *Agent, t *Task, contextText string, log Logger) (string, error)
```

#### 4.2.2 Algorithm

```
1. If a.LLM == nil -> return ErrNoLLM

2. repairMax := t.Structured.RepairMax
   if repairMax <= 0 -> repairMax = 2

3. schema := t.Structured.Schema
   (pre-validate that the schema itself is valid JSON; if not,
    return an internal error wrapping ErrInvalidOutput — this is a
    programmer error, not a model error.)

4. system := buildSystemPrompt(a) + structuredSystemInstruction
   taskPrompt := buildStructuredPrompt(t, contextText, schema)

5. messages := [SystemMessage(system), UserMessage(taskPrompt)]

6. attempt := 0
   for {
       a. ctx cancellation check (select <-ctx.Done())

       b. out, err := a.LLM.Call(ctx, messages)
          if err != nil -> return wrapped error

       c. cleaned := extractJSON(out)
          (trim whitespace, strip optional ```json fences)

       d. canonical, valErr := validateAndCanonicalize(cleaned, schema)
          if valErr == nil -> return canonical  (SUCCESS)

       e. if attempt >= repairMax -> return ErrRepairBudgetExceeded
             wrapping valErr

       f. messages = append(messages,
             AssistantMessage(out),
             UserMessage(buildRepairPrompt(out, valErr, schema)),
          )
          attempt++
   }
```

**Key points:**
- The initial call counts as attempt 0. Each repair adds one to the
  counter. Total `LLM.Call` invocations = 1 + repairs performed.
- `ErrRepairBudgetExceeded` wraps the last `ValidationErrors`.
- `ErrInvalidOutput` is used internally as a classification; the public
  error returned to the caller is `ErrRepairBudgetExceeded` on budget
  exhaustion. If a caller wants to inspect the validation errors, they
  use `errors.Unwrap` / `errors.Is`.
- Context is checked before every `LLM.Call` and on each iteration.

#### 4.2.3 `extractJSON`

```go
// extractJSON extracts the JSON payload from a model response. It trims
// whitespace and strips optional markdown code fences (```json ... ```),
// returning the inner JSON text.
func extractJSON(s string) string
```

This handles the common case where the model wraps JSON in markdown fences
even when told not to. It does NOT attempt to find JSON inside free text
(except for the fence-stripping); if the result is not valid JSON, the
validator returns errors and the repair loop kicks in.

#### 4.2.4 `validateAndCanonicalize`

```go
// validateAndCanonicalize validates cleaned JSON text against the schema
// and, on success, returns the canonicalized (re-marshaled, compacted)
// JSON string. On failure it returns "" and a non-nil error (either
// a json.ParseError wrapped with ErrInvalidOutput, or ValidationErrors).
func validateAndCanonicalize(cleaned string, schema json.RawMessage) (string, error)
```

- Unmarshal `cleaned` into `json.RawMessage` first (syntax check).
- If syntax fails, return a `ValidationError` with `Message: "invalid JSON: ..."`
  wrapped via `fmt.Errorf("%w: %v", ErrInvalidOutput, err)`.
- Call `validateSchema(doc, schema)`.
- If validation fails, return the `ValidationErrors`.
- If valid, re-marshal `doc` with `json.Marshal` (or `json.MarshalIndent`
  then compact) to produce a canonical, stable string. This is what gets
  stored as `Task.output`.

### 4.3 Prompt Builders (`prompts.go`)

#### 4.3.1 `structuredSystemInstruction`

```go
const structuredSystemInstruction = `
You MUST respond with a single, valid JSON object that satisfies the
provided JSON Schema. Do NOT include any explanation, markdown, or
surrounding text. Output ONLY the JSON.
`
```

#### 4.3.2 `buildStructuredPrompt`

```go
func buildStructuredPrompt(t *Task, context string, schema json.RawMessage) string
```

Produces a user message containing:
- The task description and expected output (reuse `buildTaskPrompt`).
- The JSON Schema (pretty-printed for readability).
- An explicit instruction: "Reply ONLY with JSON conforming to this
  schema. No markdown, no prose."

#### 4.3.3 `buildRepairPrompt`

```go
func buildRepairPrompt(previousOutput string, valErr error, schema json.RawMessage) string
```

Produces a user message containing:
- "Your previous response did not validate against the schema."
- "Your previous response was:" + previousOutput
- "Validation errors:" + valErr.Error()
- "The schema is:" + pretty-printed schema
- "Fix the issues and reply ONLY with the corrected JSON."

### 4.4 Executor Dispatch (`executor.go`)

At the very top of `executeTask`, before any existing logic:

```go
func executeTask(ctx context.Context, a *Agent, t *Task, contextText string, log Logger) (string, error) {
    if a.LLM == nil {
        return "", ErrNoLLM
    }
    if t != nil && t.Structured != nil {
        return executeStructured(ctx, a, t, contextText, log)
    }
    // ... existing ReAct logic unchanged ...
}
```

This is a minimal, surgical change. The `ErrNoLLM` check is already
present; we just add the structured dispatch before the tool/ReAct logic.

---

## 5. Concurrency

- `Task.output` / `Task.done` remain guarded by `Task.mu` (unchanged).
- `executeStructured` holds no shared state; all local variables.
- `validateSchema` is a pure function (no I/O, no shared state).
- Multiple structured tasks can run concurrently via the crew's sequential
  or hierarchical process without any new synchronization.

---

## 6. Test Plan

All tests are hermetic, using the `llmStub` (already in `executor_test.go`)
or `llm/mock.LLM` with a `Handler` for dynamic responses.

### 6.1 Schema validator unit tests (`schema_test.go`)

| Test | Scenario |
|---|---|
| `TestValidateSchema_ObjectOK` | Valid object with all required fields and correct types. |
| `TestValidateSchema_MissingRequired` | Object missing a required field -> `ValidationErrors` with path. |
| `TestValidateSchema_WrongType` | Field has wrong type (e.g. string instead of integer). |
| `TestValidateSchema_Enum` | Value not in enum -> error; value in enum -> pass. |
| `TestValidateSchema_ArrayItems` | Array with an item failing sub-schema; array with all valid items. |
| `TestValidateSchema_Nested` | Nested object properties with `required` and `type`. |
| `TestValidateSchema_IntegerVsFloat` | `integer` type accepts `5.0` but rejects `5.5`. |

### 6.2 Structured executor tests (`structured_test.go`)

| Test | Scenario | Expected |
|---|---|---|
| `TestStructuredOutput_ValidJSON` | Mock returns correct JSON on first call. | Output is canonicalized JSON; exactly 1 `LLM.Call`. |
| `TestStructuredOutput_RepairConverges` | Mock returns free text (non-JSON) first, then valid JSON. | Output is valid JSON; 2 `LLM.Call`s; `RepairMax` defaults to 2. |
| `TestStructuredOutput_RepairBudgetExceeded` | Mock always returns invalid JSON. | Error is `ErrRepairBudgetExceeded`; call count = 1 + `RepairMax`. |
| `TestStructuredOutput_RepairBudgetCustom` | `RepairMax=1`, mock returns invalid then valid. | 2 calls (1 initial + 1 repair); error is nil. |
| `TestStructuredOutput_MissingRequiredRepair` | Mock returns JSON missing a required field, then valid JSON. | 2 calls; repair prompt mentions the missing field. |
| `TestStructuredOutput_RepairPromptContainsErrors` | Inspect messages sent on repair: must include the validation error text and the schema. | Verified via `mock.LLM` log inspection. |
| `TestStructuredOutput_Canonicalization` | Mock returns JSON with extra whitespace / different key order. | Output is compact, deterministic JSON. |
| `TestStructuredOutput_CodeFenceStripped` | Mock wraps JSON in ` ```json ... ``` `. | Output is valid JSON; fences stripped; 1 call. |
| `TestStructuredOutput_NoLLM` | `Agent.LLM == nil`, structured task. | `ErrNoLLM` returned. |
| `TestStructuredOutput_ContextCancel` | Cancelled context before call. | `context.Canceled`. |
| `TestStructuredOutput_InvalidSchema` | `Schema` is not valid JSON (programmer error). | Returns error wrapping `ErrInvalidOutput`. |

### 6.3 Regression tests (`executor_test.go`)

| Test | Scenario | Expected |
|---|---|---|
| `TestExecuteTaskNoTools` (existing) | Unstructured, no tools. | Unchanged — passes as-is. |
| `TestExecuteTaskWithTool` (existing) | Unstructured, with tools. | Unchanged — passes as-is. |
| `TestStructuredDispatchDoesNotReAct` | Structured task where mock would otherwise trigger ReAct. | No tool calls; structured path taken. |
| `TestStructuredNilStructuredIsReAct` | `Task.Structured == nil` with tools. | Standard ReAct path (regression). |

### 6.4 Integration via `Agent.Execute`

| Test | Scenario |
|---|---|
| `TestAgentExecuteStructured` | `Agent.Execute` on a structured task; verify `Task.Output()` returns canonical JSON. |
| `TestCrewSequentialStructured` | A crew with one structured task; `Kickoff` returns the JSON as final output. |

### 6.5 Call count verification

For `TestStructuredOutput_RepairBudgetExceeded` with `RepairMax=2`:
- `LLM.Call` is invoked 3 times total (1 initial + 2 repairs).
- `mock.LLM.Calls()` returns 3.

---

## 7. Documentation Updates

### 7.1 `docs/tasks.md` — new section

Add after "Saving the output to a file":

```markdown
## Structured output

When a task needs typed, trustworthy data (e.g. for persisting into a
database), set the `Structured` field with a JSON Schema. The executor
instructs the model to reply with JSON only, validates the output in Go,
and retries up to `RepairMax` times if validation fails.

```go
schema := map[string]any{
    "type": "object",
    "properties": map[string]any{
        "name":  map[string]any{"type": "string"},
        "count": map[string]any{"type": "integer"},
    },
    "required": []string{"name", "count"},
}

task := crewai.NewTask("Extract the product name and count.", "JSON", agent)
task.Structured = &crewai.StructuredOutput{
    Schema:    mustMarshal(schema),
    RepairMax: 3,
}
```

### Behavior

- The model is told to output **only** JSON — no markdown, no prose.
- The output is validated against the schema in Go (stdlib `encoding/json`).
- If invalid, the executor re-prompts the model with the validation
  errors and asks it to fix the JSON. This repeats up to `RepairMax`
  times (default 2).
- If the budget is exhausted, the task fails with
  `crewai.ErrRepairBudgetExceeded`. **The executor never returns invalid
  JSON or invents data.**
- On success, `Task.Output()` returns the **canonicalized** (compacted,
  stable) JSON string.

### Supported schema keywords

The built-in validator supports a subset of JSON Schema: `type`,
`properties`, `required`, `enum`, `items`. It is not a full JSON Schema
implementation.
```

### 7.2 `docs/pt-BR/tasks.md`

Faithful Portuguese mirror of the above section.

### 7.3 `README.md` / `README.pt-BR.md`

- Add "Structured output" bullet to the "Why crewai-go" feature list.
- Add `StructuredOutput` row to the Concepts table.

### 7.4 `CHANGELOG.md` / `CHANGELOG.pt-BR.md`

Add an `[Unreleased]` section:

```markdown
## [Unreleased]

### Added

- **Structured output**: `Task.Structured` (`*StructuredOutput`) requires the
  model to produce JSON validated against a JSON Schema, with a bounded repair
  loop (`RepairMax`). New sentinels `ErrInvalidOutput` and
  `ErrRepairBudgetExceeded`. Minimal in-house schema validator (type,
  properties, required, enum, items) — stdlib only, no new dependencies.
  The `LLM.Call` interface is unchanged.
```

---

## 8. Implementation Order

The work is divided into 6 phases. Each phase is independently testable.

| Phase | Files | Description | Tests |
|---|---|---|---|
| **1. Errors + Task field** | `errors.go`, `task.go` | Add sentinels and `Task.Structured` field. | `task_test.go` (field presence) |
| **2. Schema validator** | `schema.go`, `schema_test.go` | Implement `validateSchema` + `ValidationError` types. | `schema_test.go` (all unit tests from 6.1) |
| **3. Structured executor** | `structured.go`, `prompts.go` | `executeStructured`, `extractJSON`, `validateAndCanonicalize`, prompt builders. | `structured_test.go` (all tests from 6.2) |
| **4. Executor dispatch** | `executor.go` | Add structured dispatch in `executeTask`. | `executor_test.go` (regression tests from 6.3) |
| **5. Integration tests** | `structured_test.go` (or `crew_test.go`) | `Agent.Execute` + `Crew.Kickoff` structured paths. | Tests from 6.4 |
| **6. Documentation** | `docs/*.md`, `README*.md`, `CHANGELOG*.md` | Bilingual docs, changelog, README updates. | Manual review |

### 8.1 Phase dependencies

```
Phase 1 (errors + field) ──────┐
                               ├─> Phase 3 (executor) ──> Phase 4 (dispatch) ──> Phase 5 (integration)
Phase 2 (schema validator) ───┘                                                    │
                                                                                   └─> Phase 6 (docs)
```

Phases 1 and 2 are independent and can be done in parallel. Phase 3 depends
on both. Phase 4 depends on 3. Phases 5 and 6 depend on 4.

---

## 9. Edge Cases and Decisions

| Case | Decision |
|---|---|
| Model wraps JSON in markdown fences | `extractJSON` strips ` ```json ` and ` ``` ` fences. |
| Model returns valid JSON with extra whitespace / reordered keys | Canonicalized via `json.Marshal` so output is stable. |
| Schema itself is invalid JSON | Programmer error; return `ErrInvalidOutput` wrapped with the parse error. Do not enter the repair loop. |
| `RepairMax = 0` or negative | Defaults to 2. |
| Context cancelled mid-loop | Checked before every `LLM.Call`; returns `ctx.Err()`. |
| LLM returns empty string | Fails validation ("invalid JSON: unexpected end of JSON"); enters repair loop. |
| `Task.Structured != nil` but `Schema` is `nil` | Treated as invalid schema -> `ErrInvalidOutput` (programmer error). |
| Structured task WITH tools | The structured path bypasses ReAct entirely. Tools are ignored in structured mode (documented). A future extension could allow tool use before structured output. |
| `integer` type and `5.0` | Accepted (JSON numbers are `float64` in Go; no fractional part = integer). |
| Unknown properties in object | Allowed (no `additionalProperties: false` support — documented). |
| Multiple validation errors | All collected and returned in one `ValidationErrors`; repair prompt shows all. |

---

## 10. Future Extensions (out of scope, documented)

- **Native function calling**: some providers (OpenAI, Anthropic) support
  tool/function calling that returns structured data natively. This could
  be added as an optional interface (`StructuredLLM`) checked at runtime,
  falling back to the prompt-based approach. Not required now.
- **Full JSON Schema support**: `additionalProperties`, `oneOf`/`anyOf`,
  `pattern`, `minimum`/`maximum`, `minItems`/`maxItems`, etc. The current
  validator is intentionally minimal.
- **Tool use before structured output**: allow an agent to use tools to
  gather data, then produce structured output at the end.

---

## 11. Acceptance Criteria Checklist

- [ ] `go build ./...` passes with no new dependencies in `go.mod`.
- [ ] `go test ./...` passes (all existing + new tests).
- [ ] `LLM.Call(ctx, []Message) (string, error)` interface is unchanged.
- [ ] Existing ReAct executor and unstructured tasks work without modification
      (regression tests pass).
- [ ] `ErrInvalidOutput` and `ErrRepairBudgetExceeded` are sentinel errors
      usable with `errors.Is`.
- [ ] Structured task with valid JSON returns canonicalized JSON as output.
- [ ] Structured task with invalid JSON retries within budget and converges.
- [ ] Structured task that never validates fails with
      `ErrRepairBudgetExceeded` after exactly `1 + RepairMax` calls.
- [ ] nil LLM on structured task returns `ErrNoLLM`.
- [ ] `docs/tasks.md` and `docs/pt-BR/tasks.md` have a "Structured output"
      section.
- [ ] `CHANGELOG.md` and `CHANGELOG.pt-BR.md` updated.
- [ ] `README.md` and `README.pt-BR.md` updated.
- [ ] All comments and godoc in English, no accents.
- [ ] All tests hermetic (mock LLM, no real network).