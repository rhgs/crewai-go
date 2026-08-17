# Plan — Post-Output Guardrail / Validation System

> **Status:** Draft for implementation
> **Scope:** Add configurable code-enforced guardrails that run after task/crew
> output is produced, blocking publication of outputs that violate business
> invariants. This is the "anti-hallucination barrier as a code guarantee"
> layer.

---

## 1. Goals and Constraints

### 1.1 Goals

1. Allow a `Crew` to enforce, in Go code (not via prompt), that produced
   outputs are safe to publish.
2. Provide **crew-level** guardrails (run against the full `CrewOutput` after
   all tasks complete) and **task-level** guardrails (run against each task's
   output as it completes).
3. Block by default: if any guardrail returns a non-nil error, `Kickoff` must
   return `ErrBlockedByGuardrail` wrapping the error — never return a
   partially validated output as if it were valid.
4. Guardrails are pure validation hooks: they MUST NOT mutate the output.
5. Work with or without structured output: guardrails can inspect raw text and,
   when available, structured/validated fields.

### 1.2 Constraints (must be preserved)

| Constraint | Enforcement |
|---|---|
| Zero external dependencies | `go.mod` unchanged; guardrails use only stdlib. |
| LLM-agnostic | Guardrails run AFTER the LLM call; no provider dependency. |
| English comments, no accents | All godoc, error messages, identifiers. |
| Hermetic tests | `llm/mock`; no real network. |
| Sentinel errors, context on all I/O | New sentinel `ErrBlockedByGuardrail`; `ctx` passed to every guardrail call. |
| Backward compatible | No change to existing Agent/Task/Crew behavior when no guardrails are set. |

---

## 2. New API Surface

### 2.1 `Guardrail` function type (root package)

```go
// Guardrail is a post-output validation hook. It receives the crew output
// (or a task-level output wrapper) and returns nil if the output is safe to
// publish, or a descriptive error identifying what failed.
//
// Guardrails are pure validation: they MUST NOT mutate the output.
type Guardrail func(ctx context.Context, out *CrewOutput) error
```

Both crew-level and task-level guardrails use the same type signature for
simplicity. Task-level guardrails receive a `*CrewOutput` populated with only
the current task's output (Final = task output, TasksOutput = [this task]).

### 2.2 `Crew.Guardrails` field

```go
type Crew struct {
    // ... existing fields ...

    // Guardrails, when set, are run after all tasks complete successfully,
    // in order, against the full CrewOutput. If any guardrail returns a
    // non-nil error, Kickoff returns ErrBlockedByGuardrail.
    Guardrails []Guardrail
}
```

### 2.3 `Task.Guardrail` field

```go
type Task struct {
    // ... existing fields ...

    // Guardrail, when set, is run against this task's output as soon as the
    // task completes (before the next task starts). If it returns a non-nil
    // error, the crew execution halts and Kickoff returns
    // ErrBlockedByGuardrail.
    Guardrail Guardrail
}
```

### 2.4 New sentinel error

```go
// ErrBlockedByGuardrail is returned when a guardrail rejects an output. It
// wraps the guardrail's error so the caller can inspect the violated
// invariant via errors.Unwrap.
ErrBlockedByGuardrail = errors.New("crewai: output blocked by guardrail")
```

### 2.5 Functional option (convenience)

```go
// WithGuardrails adds crew-level guardrails to a Crew.
func WithGuardrails(guards ...Guardrail) func(*Crew) {
    return func(c *Crew) { c.Guardrails = append(c.Guardrails, guards...) }
}

// WithGuardrail sets a task-level guardrail on a Task.
func (t *Task) WithGuardrail(g Guardrail) *Task {
    t.Guardrail = g
    return t
}
```

---

## 3. Architecture and File Changes

### 3.1 File map

| File | Change type | Description |
|---|---|---|
| `guardrail.go` | **New** | `Guardrail` type, `WithGuardrails` option, `WithGuardrail` method, `runTaskGuardrail` and `runCrewGuardrails` helpers. |
| `guardrail_test.go` | **New** | All guardrail unit and integration tests. |
| `errors.go` | **Modified** | Add `ErrBlockedByGuardrail`. |
| `crew.go` | **Modified** | Add `Guardrails` field; call task-level guardrails in `execute`; call crew-level guardrails at end of `Kickoff`. |
| `task.go` | **Modified** | Add `Guardrail` field; `WithGuardrail` method. |
| `crew_test.go` | **Modified** | Add regression test for no-guardrails behavior. |
| `docs/crews.md` | **Modified** | New "Guardrails" section. |
| `docs/pt-BR/crews.md` | **Modified** | New "Guardrails" section (PT mirror). |
| `README.md` | **Modified** | Feature mention + concepts table. |
| `README.pt-BR.md` | **Modified** | PT mirror. |
| `CHANGELOG.md` | **Modified** | New `[Unreleased]` entry. |
| `CHANGELOG.pt-BR.md` | **Modified** | PT mirror. |
| `examples/guardrails/` | **New** | Provenance guardrail example. |
| `PLAN.md` | **Modified** | Status update. |
| `PLAN.pt-BR.md` | **Modified** | PT mirror. |

### 3.2 Module dependency graph

```
crew.go
  ├──> runTaskGuardrail (guardrail.go)  -- called per task in execute()
  └──> runCrewGuardrails (guardrail.go) -- called after all tasks in Kickoff()
```

No new import paths; everything stays in the root `crewai` package.

---

## 4. Detailed Design

### 4.1 `Guardrail` type and helpers (`guardrail.go`)

```go
package crewai

import (
    "context"
    "fmt"
)

// Guardrail is a post-output validation hook. It receives the crew output
// and returns nil if the output is safe to publish, or a descriptive error
// identifying what failed.
//
// Guardrails are pure validation: they MUST NOT mutate the output.
type Guardrail func(ctx context.Context, out *CrewOutput) error

// WithGuardrails returns a functional option that adds crew-level guardrails
// to a Crew.
func WithGuardrails(guards ...Guardrail) func(*Crew) {
    return func(c *Crew) { c.Guardrails = append(c.Guardrails, guards...) }
}

// runTaskGuardrail runs the task-level guardrail (if any) against the
// task's output. It returns nil if the guardrail passes or is not set,
// or an error wrapped with ErrBlockedByGuardrail if it fails.
func runTaskGuardrail(ctx context.Context, task *Task, result string) error {
    if task.Guardrail == nil {
        return nil
    }
    out := &CrewOutput{
        Final: result,
        TasksOutput: []TaskOutput{{
            Task:   task.Name,
            Output: result,
        }},
    }
    if err := task.Guardrail(ctx, out); err != nil {
        return fmt.Errorf("%w: task %q: %v", ErrBlockedByGuardrail, task.Name, err)
    }
    return nil
}

// runCrewGuardrails runs all crew-level guardrails in order against the
// full CrewOutput. It returns nil if all pass, or an error wrapped with
// ErrBlockedByGuardrail on the first failure (short-circuit).
func runCrewGuardrails(ctx context.Context, guards []Guardrail, out *CrewOutput) error {
    for _, g := range guards {
        if err := g(ctx, out); err != nil {
            return fmt.Errorf("%w: %v", ErrBlockedByGuardrail, err)
        }
    }
    return nil
}
```

### 4.2 Task changes (`task.go`)

Add the `Guardrail` field and `WithGuardrail` method:

```go
type Task struct {
    // ... existing fields ...

    // Guardrail, when set, is run against this task's output as soon as
    // the task completes (before the next task starts). If it returns a
    // non-nil error, the crew execution halts and Kickoff returns
    // ErrBlockedByGuardrail.
    Guardrail Guardrail
}

// WithGuardrail sets a task-level guardrail and returns the task for
// fluent chaining.
func (t *Task) WithGuardrail(g Guardrail) *Task {
    t.Guardrail = g
    return t
}
```

### 4.3 Crew changes (`crew.go`)

#### 4.3.1 New field

```go
type Crew struct {
    // ... existing fields ...

    // Guardrails, when set, are run after all tasks complete successfully,
    // in order, against the full CrewOutput. If any guardrail returns a
    // non-nil error, Kickoff returns ErrBlockedByGuardrail.
    Guardrails []Guardrail
}
```

#### 4.3.2 Task-level guardrail in `execute()`

After `task.setOutput(result)` and before returning, add:

```go
func (c *Crew) execute(ctx context.Context, agent *Agent, task *Task) (string, error) {
    // ... existing code ...

    result, err := executeTask(ctx, agent, task, contextText, c.logger)
    if err != nil {
        return "", err
    }
    if err := task.setOutput(result); err != nil {
        return "", fmt.Errorf("writing task output: %w", err)
    }
    if c.mem != nil {
        c.mem.Save(MemoryRecord{...})
    }

    // NEW: run task-level guardrail.
    if err := runTaskGuardrail(ctx, task, result); err != nil {
        return "", err
    }

    return result, nil
}
```

This means task-level guardrails run at **task completion** (before the next
task starts), which is the recommended order. This allows early termination
of the crew if a task produces invalid output.

#### 4.3.3 Crew-level guardrails in `Kickoff()`

After all tasks complete successfully and before returning, add:

```go
func (c *Crew) Kickoff(ctx context.Context, inputs map[string]string) (*CrewOutput, error) {
    // ... existing setup ...

    start := time.Now()
    var err error
    out := &CrewOutput{}

    switch c.Process {
    case Sequential:
        out, err = c.runSequential(ctx)
    case Hierarchical:
        out, err = c.runHierarchical(ctx)
    }
    if err != nil {
        return nil, err
    }
    out.Duration = time.Since(start)

    // NEW: run crew-level guardrails.
    if err := runCrewGuardrails(ctx, c.Guardrails, out); err != nil {
        return nil, err
    }

    return out, nil
}
```

### 4.4 Execution order (documented)

```
1. For each task (sequential or hierarchical):
   a. Execute the task (ReAct or structured output).
   b. Store the output (setOutput, memory).
   c. Run the task-level Guardrail (if set).         <-- per task
      If it fails -> return ErrBlockedByGuardrail immediately.
2. After all tasks complete:
   a. Run crew-level Guardrails in order.             <-- per crew
      First failure short-circuits -> ErrBlockedByGuardrail.
3. Return the validated CrewOutput.
```

### 4.5 Relationship to structured output

| Feature | What it checks | When it runs | Failure behavior |
|---|---|---|---|
| Structured output (`Task.Structured`) | JSON SHAPE (schema: type, properties, required, enum, items) | During LLM call loop | Repair loop, then `ErrRepairBudgetExceeded` |
| Guardrails (`Crew.Guardrails`, `Task.Guardrail`) | Business MEANING (provenance, invariants, anti-hallucination) | After output is produced | `ErrBlockedByGuardrail` (no repair, no retry) |

Schema validation checks the **shape**; guardrails check the **meaning**.
They are complementary and can both be used on the same task.

---

## 5. Concurrency

- Guardrails are pure functions (no mutation of `CrewOutput`).
- Task-level guardrails run synchronously within `execute()`, which is
  called sequentially in both process types.
- Crew-level guardrails run synchronously after all tasks complete.
- No shared state, no mutex needed for guardrail execution.
- Context is passed to every guardrail call for cancellation/timeout support.

---

## 6. Test Plan

All tests use `llm/mock`; hermetic, no real network.

### 6.1 Guardrail unit/integration tests (`guardrail_test.go`)

| Test | Scenario | Expected |
|---|---|---|
| `TestCrewGuardrail_Pass` | Crew with a passing guardrail; mock LLM returns valid output. | `Kickoff` succeeds; output unchanged. |
| `TestCrewGuardrail_Fail` | Crew with a failing guardrail. | `Kickoff` returns `ErrBlockedByGuardrail`; underlying error message preserved via `errors.Unwrap`. |
| `TestCrewGuardrail_Multiple` | Two crew guardrails; first passes, second fails. | `ErrBlockedByGuardrail`; first guardrail ran, second's error is wrapped. |
| `TestCrewGuardrail_ShortCircuit` | Two crew guardrails; first fails. | `ErrBlockedByGuardrail`; second guardrail did NOT run (verify via counter). |
| `TestCrewGuardrail_Order` | Two crew guardrails; first fails with msg "A", second would fail with "B". | Error message contains "A", not "B". |
| `TestTaskGuardrail_Pass` | Task with a passing guardrail. | `Kickoff` succeeds. |
| `TestTaskGuardrail_Fail` | Task with a failing guardrail. | `Kickoff` returns `ErrBlockedByGuardrail`. |
| `TestTaskGuardrail_BlocksCrew` | Task 1 guardrail fails; task 2 should not execute. | `ErrBlockedByGuardrail`; task 2 never runs (verify via mock call count). |
| `TestTaskGuardrail_ReceivesOutput` | Task guardrail receives `CrewOutput` with Final and TasksOutput populated. | Verify inside the guardrail. |
| `TestCrewGuardrail_ReceivesFullOutput` | Crew guardrail receives full `CrewOutput` with all tasks. | Verify `len(TasksOutput) == N` and `Final` is set. |
| `TestNoGuardrails_Regression` | No guardrails on crew or tasks. | Existing behavior unchanged (same as before guardrail feature). |
| `TestCrewGuardrail_DoesNotMutate` | Guardrail attempts to mutate `CrewOutput` (verify the output is unchanged after Kickoff). | Output is the same as without the guardrail. |
| `TestCrewGuardrail_ContextCancel` | Context cancelled before guardrail runs. | `context.Canceled` propagates. |
| `TestTaskGuardrail_StructuredOutput` | Structured task + task guardrail that inspects the JSON. | Guardrail receives canonicalized JSON; passes or fails correctly. |
| `TestWithGuardrails_Option` | `WithGuardrails(g1, g2)` functional option. | Both guardrails are added to the crew. |
| `TestWithGuardrail_Method` | `task.WithGuardrail(g)` fluent method. | Guardrail is set on the task. |

### 6.2 Example use-case tests

| Test | Scenario |
|---|---|
| `TestGuardrail_ProvenanceRule` | Guardrail that rejects any fact lacking a source URL. Mock returns output with and without URL. |
| `TestGuardrail_AntiHallucinationIDs` | Guardrail that rejects outputs referencing identifiers not present in source data. |
| `TestGuardrail_MinLength` | Guardrail that rejects empty or overly short final output. |

---

## 6.3 Security analysis

Guardrails are a security feature by design — they are the "anti-hallucination
barrier as a code guarantee" layer. The following security aspects must be
verified:

### 6.3.1 Threat model

| Threat | Mitigation | Test |
|---|---|---|
| **LLM hallucination**: model invents facts, numbers, or IDs not in source data | Guardrail inspects output against known-valid identifiers; blocks on mismatch | `TestGuardrail_AntiHallucinationIDs` |
| **Unprovenanced claims**: model states facts without sources | Guardrail checks for source URLs/references; blocks if absent | `TestGuardrail_ProvenanceRule` |
| **Truncated/empty output**: model returns empty or minimal output that passes schema but has no content | Guardrail checks minimum length or content presence | `TestGuardrail_MinLength` |
| **Guardrail bypass via mutation**: guardrail is tricked into accepting invalid output by mutating the `CrewOutput` | Documented MUST NOT mutate; test verifies no mutation occurs | `TestCrewGuardrail_DoesNotMutate` |
| **Denial of service via slow guardrail**: a guardrail that blocks forever | Context is passed to every guardrail; timeout via `context.WithTimeout` | `TestCrewGuardrail_ContextCancel` |
| **Partial output leak**: crew returns a partially validated output after a guardrail fails | `Kickoff` returns `nil, ErrBlockedByGuardrail` — output is never partially returned | `TestCrewGuardrail_Fail`, `TestTaskGuardrail_Fail` |
| **Guardrail injection**: attacker controls the guardrail function | Guardrails are set in Go code by the developer, not by user input or LLM output. No dynamic guardrail loading. | Review (static analysis) |
| **Order-dependent bypass**: guardrails run in wrong order, allowing a later guardrail to be skipped | Tests verify order and short-circuit behavior | `TestCrewGuardrail_Order`, `TestCrewGuardrail_ShortCircuit` |

### 6.3.2 Security tests

| Test | What it verifies |
|---|---|
| `TestGuardrail_BlocksHallucinatedIDs` | Output referencing ID "X-999" (not in source) is blocked; output with valid ID "X-001" passes. |
| `TestGuardrail_BlocksUnprovenancedClaim` | Output without any URL is blocked; output with "https://..." passes. |
| `TestGuardrail_BlocksEmptyOutput` | Empty string or <10 chars is blocked. |
| `TestGuardrail_NoPartialOutputLeak` | When guardrail fails, `Kickoff` returns nil output (not a partial CrewOutput). |
| `TestGuardrail_DoesNotMutate` | Guardrail that sets `out.Final = ""` — verify the returned output (on pass) is unchanged. |
| `TestGuardrail_ContextTimeout` | Guardrail that sleeps; context with 1ms timeout — `context.DeadlineExceeded` propagates. |
| `TestGuardrail_TaskHaltsCrew` | Task 1 guardrail fails — task 2 LLM is never called (call count = 1, not 2). |
| `TestGuardrail_ErrorsIs` | `errors.Is(err, crewai.ErrBlockedByGuardrail)` returns true for both crew and task guardrail failures. |
| `TestGuardrail_ErrorsUnwrapPreservesMessage` | `errors.Unwrap(err)` contains the original guardrail error message (e.g. "missing source URL"). |

### 6.3.3 gosec / govulncheck

- **gosec**: No new findings expected. Guardrails use only `context`, `fmt`,
  `strings` — no file I/O, no crypto, no exec. No `//nolint` directives.
- **govulncheck**: No new code paths to stdlib vulnerabilities (guardrails
  are pure Go logic, no network/crypto).
- **Secret scan**: No secrets in guardrail code or example. The example uses
  `openai.New` with `OPENAI_API_KEY` env var (existing pattern).

## 6.4 Code review checklist

Before merging, the following must be verified:

### 6.4.1 Architecture and design

- [ ] `Guardrail` type is a clean function signature, easy to implement.
- [ ] Task-level and crew-level guardrails use the same type (no type bloat).
- [ ] Execution order is documented and consistent: task guardrails at task
      completion, crew guardrails after all tasks.
- [ ] Short-circuit behavior is correct: first failure halts, no further
      guardrails run.
- [ ] No shared mutable state between guardrail calls.
- [ ] No new dependencies in `go.mod`.

### 6.4.2 Backward compatibility

- [ ] `Crew` without `Guardrails` field set behaves identically to before.
- [ ] `Task` without `Guardrail` field set behaves identically to before.
- [ ] `Kickoff` return type unchanged (`*CrewOutput, error`).
- [ ] `CrewOutput` and `TaskOutput` structs unchanged.
- [ ] No existing test modified or broken.

### 6.4.3 Error handling

- [ ] `ErrBlockedByGuardrail` is a sentinel usable with `errors.Is`.
- [ ] The wrapping preserves the original guardrail error message via
      `errors.Unwrap`.
- [ ] Task guardrail errors include the task name/label for debugging.
- [ ] No error swallowing (all guardrail errors propagate).

### 6.4.4 Concurrency safety

- [ ] Guardrails are pure functions (no mutation of `CrewOutput`).
- [ ] No new mutex needed (guardrails run synchronously).
- [ ] Context passed to every guardrail for cancellation.

### 6.4.5 Documentation and style

- [ ] All godoc and comments in English, no accents.
- [ ] `guardrail.go` has package-level documentation.
- [ ] `docs/crews.md` and `docs/pt-BR/crews.md` updated.
- [ ] `README.md` and `README.pt-BR.md` updated.
- [ ] `CHANGELOG.md` and `CHANGELOG.pt-BR.md` updated.
- [ ] Example in `examples/guardrails/main.go` is self-contained and builds.

## 6.5 Test coverage strategy (target: > 90%)

### 6.5.1 Coverage targets

| Scope | Target | Rationale |
|---|---|---|
| `guardrail.go` (new file) | **100%** | Small, pure-logic file; every branch testable. |
| `crew.go` (modified) | **> 92%** | Guardrail integration paths must be covered; existing paths already at 89%+. |
| `task.go` (modified) | **100%** | Only `WithGuardrail` added (one-liner). |
| `errors.go` (modified) | **100%** | Only a new var declaration. |
| Root package overall | **> 90%** | Currently 92.8%; guardrail additions must not lower this. |

### 6.5.2 Uncovered branches and how to cover them

| Branch | Location | Test to add |
|---|---|---|
| `runTaskGuardrail` nil guardrail | `guardrail.go` | `TestTaskGuardrail_NilGuardrail` — task with `Guardrail = nil`; verify no error. |
| `runTaskGuardrail` pass path | `guardrail.go` | `TestTaskGuardrail_Pass` (already planned). |
| `runTaskGuardrail` fail path | `guardrail.go` | `TestTaskGuardrail_Fail` (already planned). |
| `runCrewGuardrails` empty slice | `guardrail.go` | `TestNoGuardrails_Regression` (already planned). |
| `runCrewGuardrails` all pass | `guardrail.go` | `TestCrewGuardrail_Pass` (already planned). |
| `runCrewGuardrails` first fail | `guardrail.go` | `TestCrewGuardrail_ShortCircuit` (already planned). |
| `runCrewGuardrails` second fail | `guardrail.go` | `TestCrewGuardrail_Multiple` (already planned). |
| `execute()` task guardrail pass | `crew.go` | `TestTaskGuardrail_Pass` via `Kickoff`. |
| `execute()` task guardrail fail | `crew.go` | `TestTaskGuardrail_Fail` via `Kickoff`. |
| `Kickoff()` crew guardrails pass | `crew.go` | `TestCrewGuardrail_Pass` via `Kickoff`. |
| `Kickoff()` crew guardrails fail | `crew.go` | `TestCrewGuardrail_Fail` via `Kickoff`. |
| `WithGuardrails` option | `guardrail.go` | `TestWithGuardrails_Option` (already planned). |
| `WithGuardrail` method | `task.go` | `TestWithGuardrail_Method` (already planned). |

### 6.5.3 Coverage verification command

After implementation, run:

```bash
go test -coverprofile=/tmp/cov.out -coverpkg=. . && go tool cover -func=/tmp/cov.out | grep -E "guardrail\.go|crew\.go|task\.go|errors\.go"
```

Verify:
- `guardrail.go`: all functions at 100%.
- `crew.go`: `execute` and `Kickoff` at > 92%.
- Overall root package: > 90%.

### 6.5.4 Coverage test file

Add `guardrail_coverage_test.go` (or merge into `guardrail_test.go`) with
edge-case tests that exist solely to cover defensive branches:

| Test | Purpose |
|---|---|
| `TestTaskGuardrail_NilGuardrail` | Covers the `task.Guardrail == nil` early return. |
| `TestCrewGuardrail_EmptyGuardrails` | Covers the empty slice loop (no iterations). |
| `TestCrewGuardrail_AllPass` | Covers the all-pass return-nil path. |
| `TestCrewGuardrail_BothPassAndFail` | Covers pass-then-fail order. |

---

## 7. Example (`examples/guardrails/main.go`)

```go
// Guardrails example: a provenance guardrail that blocks outputs without
// source URLs.
//
// Run:
//
//	export OPENAI_API_KEY=sk-...
//	go run ./examples/guardrails
package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/rhgs/crewai-go"
	"github.com/rhgs/crewai-go/llm/openai"
)

// provenanceGuardrail rejects any output that does not contain at least one
// source URL (http:// or https://).
func provenanceGuardrail(_ context.Context, out *crewai.CrewOutput) error {
	if !strings.Contains(out.Final, "http://") && !strings.Contains(out.Final, "https://") {
		return fmt.Errorf("output lacks source URL; refusing to publish unprovenanced claim")
	}
	return nil
}

func main() {
	llm := openai.New("gpt-4o-mini")
	agent := crewai.NewAgent("Researcher", "Research with sources", "", llm)

	task := crewai.NewTask(
		"Summarize the key facts about the Go programming language with source URLs.",
		"A summary with at least one source URL.",
		agent,
	)

	crew := crewai.NewCrew([]*crewai.Agent{agent}, []*crewai.Task{task})
	crew.Guardrails = []crewai.Guardrail{provenanceGuardrail}

	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		log.Fatalf("blocked: %v", err)
	}

	fmt.Println("\n=== Result ===")
	fmt.Println(out.Final)
}
```

---

## 8. Documentation Updates

### 8.1 `docs/crews.md` — new "Guardrails" section

```markdown
## Guardrails

Guardrails are code-enforced post-output validation hooks. They run after a
crew (or task) produces output and BLOCK publication if a business invariant
is violated. Unlike prompt-level instructions, guardrails are a hard code
guarantee: the output is never returned if a guardrail fails.

### Crew-level guardrails

Set `Crew.Guardrails` to run validation against the full `CrewOutput` after
all tasks complete:

```go
crew.Guardrails = []crewai.Guardrail{
    func(_ context.Context, out *crewai.CrewOutput) error {
        if len(out.Final) < 50 {
            return fmt.Errorf("output too short: %d chars", len(out.Final))
        }
        return nil
    },
}
```

Or use the functional option:

```go
crew = crewai.NewCrew(agents, tasks)
// Apply guardrails via option (in a constructor pattern if desired).
```

### Task-level guardrails

Set `Task.Guardrail` to validate a single task's output as soon as it
completes (before the next task starts):

```go
task := crewai.NewTask("...", "...", agent).
    WithGuardrail(func(_ context.Context, out *crewai.CrewOutput) error {
        if !strings.Contains(out.Final, "http") {
            return fmt.Errorf("missing source URL")
        }
        return nil
    })
```

### Blocking semantics

- If any guardrail returns a non-nil error, `Kickoff` returns
  `crewai.ErrBlockedByGuardrail` wrapping the guardrail's error.
- The partially validated output is NOT returned.
- Task-level guardrails run at task completion; crew-level guardrails run
  after all tasks. First failure short-circuits.
- Guardrails MUST NOT mutate the output.

### Guardrails vs. structured output

| Feature | What it checks | When | Failure |
|---|---|---|---|
| Structured output | JSON shape (schema) | During LLM call | Repair loop, then `ErrRepairBudgetExceeded` |
| Guardrails | Business meaning (invariants) | After output | `ErrBlockedByGuardrail` (no retry) |

Schema validation checks the **shape**; guardrails check the **meaning**.
Use both for maximum safety.
```

### 8.2 `docs/pt-BR/crews.md`

Faithful Portuguese mirror of the above section.

### 8.3 `README.md` / `README.pt-BR.md`

- Add "Guardrails" bullet to the "Why crewai-go" feature list.
- Add `Guardrail` row to the Concepts table.

### 8.4 `CHANGELOG.md` / `CHANGELOG.pt-BR.md`

```markdown
## [Unreleased]

### Added

- **Guardrails**: crew-level (`Crew.Guardrails`) and task-level
  (`Task.Guardrail`) post-output validation hooks that block publication of
  outputs violating business invariants. New sentinel
  `ErrBlockedByGuardrail`. Functional options `WithGuardrails` (crew) and
  `WithGuardrail` (task). Complements structured-output schema validation
  (shape vs. meaning). No new dependencies; backward compatible.
```

### 8.5 `PLAN.md` / `PLAN.pt-BR.md`

- Update P2 Guardrails entry from partial to **done** (code-enforced, not
  just retry-based).
- Add guardrails to the completed features list.

---

## 9. Implementation Order

| Phase | Files | Description | Tests |
|---|---|---|---|
| **1. Errors + types** | `errors.go`, `guardrail.go`, `task.go` | `ErrBlockedByGuardrail`, `Guardrail` type, `Task.Guardrail` field, `WithGuardrail`, `WithGuardrails`. | `guardrail_test.go` (option/method tests) |
| **2. Crew integration** | `crew.go` | `Crew.Guardrails` field; task-level guardrail in `execute()`; crew-level guardrails in `Kickoff()`. | `guardrail_test.go` (pass/fail/order/short-circuit) |
| **3. Tests** | `guardrail_test.go` | All tests from section 6. | All pass |
| **4. Example** | `examples/guardrails/main.go` | Provenance guardrail example. | Build passes |
| **5. Documentation** | `docs/*.md`, `README*.md`, `CHANGELOG*.md`, `PLAN*.md` | Bilingual docs, changelog, README, PLAN. | Manual review |
| **6. Security tests** | `guardrail_test.go` | All security tests from section 6.3.2. | All pass |
| **7. Coverage tests** | `guardrail_test.go` or `guardrail_coverage_test.go` | Edge-case tests from section 6.5.4. | Coverage > 90% |
| **8. Code review** | — | Verify all items in section 6.4. | Manual checklist complete |

### Phase dependencies

```
Phase 1 (types + errors) ──> Phase 2 (crew integration) ──> Phase 3 (tests)
                                                                       │
                                                                       ├─> Phase 4 (example) ──> Phase 5 (docs)
                                                                       │
                                                                       └─> Phase 6 (security) ──> Phase 7 (coverage) ──> Phase 8 (code review)
```

---

## 10. Edge Cases and Decisions

| Case | Decision |
|---|---|
| Guardrail panics | Not caught — let it propagate (consistent with Go conventions). Document that guardrails should not panic. |
| Guardrail mutates output | Documented as MUST NOT; not enforced in code (no deep copy). Tests verify no mutation in practice. |
| Task guardrail fails on task 1 of 3 | Execution halts immediately; tasks 2 and 3 never run. |
| Crew guardrail fails | Output is fully produced but not returned; `Kickoff` returns `nil, ErrBlockedByGuardrail`. |
| Both task and crew guardrails set | Task guardrail runs first (per task); crew guardrail runs after all tasks. |
| Structured output + guardrail | Guardrail receives canonicalized JSON as `Final`; can parse it with `encoding/json` for deeper inspection. |
| Context cancelled during guardrail | `ctx.Err()` returned if guardrail respects context. |
| Empty `Guardrails` slice on crew | No guardrails run; existing behavior preserved. |
| `nil` Guardrail on task | `runTaskGuardrail` skips it (nil check). |
| Multiple crew guardrails | Run in order; first failure short-circuits. |

---

## 11. Acceptance Criteria Checklist

- [ ] `go build ./...` passes with no new dependencies in `go.mod`.
- [ ] `go test ./...` passes (all existing + new tests).
- [ ] `go vet ./...` clean.
- [ ] No change to existing Agent/Task/Crew behavior when no guardrails are set.
- [ ] `ErrBlockedByGuardrail` is a sentinel usable with `errors.Is`.
- [ ] Crew guardrails run in order, first failure short-circuits.
- [ ] Task guardrail runs at task completion, blocks crew on failure.
- [ ] Task guardrail failure halts execution (later tasks don't run).
- [ ] Guardrails receive fully populated `CrewOutput`.
- [ ] `examples/guardrails/main.go` builds.
- [ ] `docs/crews.md` and `docs/pt-BR/crews.md` have a "Guardrails" section.
- [ ] `CHANGELOG.md` and `CHANGELOG.pt-BR.md` updated.
- [ ] `README.md` and `README.pt-BR.md` updated.
- [ ] `PLAN.md` and `PLAN.pt-BR.md` updated.
- [ ] All comments and godoc in English, no accents.
- [ ] All tests hermetic (mock LLM, no real network).

### Security

- [ ] `gosec` reports no new findings (no `//nolint` added).
- [ ] `govulncheck` reports no new vulnerabilities from guardrail code.
- [ ] No secrets in guardrail code or example.
- [ ] `errors.Is(err, ErrBlockedByGuardrail)` works for both crew and task failures.
- [ ] `errors.Unwrap` preserves the original guardrail error message.
- [ ] No partial output leak: `Kickoff` returns nil output on guardrail failure.
- [ ] Task guardrail failure halts crew (later tasks don't run).
- [ ] Guardrail receives context for timeout/cancellation.

### Code review

- [ ] `Guardrail` type is clean and simple (function signature).
- [ ] Task and crew guardrails use the same type.
- [ ] Execution order documented and tested.
- [ ] Short-circuit behavior verified.
- [ ] No shared mutable state.
- [ ] No new dependencies.
- [ ] Backward compatible (no existing test modified or broken).

### Test coverage

- [ ] `guardrail.go`: 100% function coverage.
- [ ] `crew.go`: `execute` and `Kickoff` > 92%.
- [ ] Root package overall: > 90%.
- [ ] Coverage verified with `go tool cover -func`.