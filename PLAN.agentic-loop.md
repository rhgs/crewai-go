# Plan — Agentic Loop: Plan-Execute-Evaluate-Refine

> **Languages:** **English** (current) · [Português](PLAN.agentic-loop.pt-BR.md)

> **Status:** Draft for implementation
> **Scope:** Add an optional, more sophisticated agent execution loop that
> goes beyond the simple ReAct cycle. The AgenticLoop follows a
> Plan-Execute-Evaluate-Refine pattern, giving agents the ability to
> self-assess their output and iteratively improve it before returning a
> final answer. Zero new dependencies, backward compatible.

---

## 1. Goals and Constraints

### 1.1 Goals

1. Provide an alternative execution loop (`AgenticLoop`) that replaces the
   single-pass ReAct loop with a multi-phase cycle:
   - **Plan**: the agent decomposes the task into actionable steps.
   - **Execute**: the agent executes each step, using tools as needed.
   - **Evaluate**: the agent (or an evaluator) assesses the output against
     the task's expected output and quality criteria.
   - **Refine**: if evaluation fails, the agent revises and re-executes,
     up to a configurable maximum number of refinement rounds.
2. Allow the evaluate step to use a separate evaluator LLM/agent (or the
   same agent with a different prompt) for independent quality assessment.
3. Support configurable stop conditions: max refinement rounds, evaluation
   threshold (pass/fail), or a custom evaluator function.
4. Integrate with existing features: structured output (the final answer
   can go through schema validation), guardrails (the loop output is
   subject to crew/task guardrails), and facts (collected during execution).
5. Work with or without tools — the plan step is skipped when the agent has
   no tools (equivalent to a direct call with self-evaluation).

### 1.2 Constraints (must be preserved)

| Constraint | Enforcement |
|---|---|
| Zero external dependencies | `go.mod` unchanged; the loop uses only stdlib (`context`, `fmt`, `strings`, `encoding/json`). |
| LLM-agnostic, non-breaking `LLM.Call` | The `Call(ctx, []Message) (string, error)` signature is never modified. The AgenticLoop works by prompt engineering + Go-side orchestration. |
| English comments, no accents | All godoc, error messages, prompts, and identifiers follow this rule. |
| Hermetic tests | `llm/mock` + spy tools; no real network. |
| Sentinel errors, context on all I/O, functional options | New sentinels in `errors.go`; `ctx` propagated through every phase; functional options for `AgenticLoop`. |
| Concurrency safety | The loop is stateless per execution; no shared mutable state across goroutines. |
| Backward compatibility | The default executor remains the ReAct loop. AgenticLoop is opt-in via `Agent.Loop` or `Task.Loop`. Existing behavior is unchanged when no loop is configured. |

---

## 2. Background — Why an Agentic Loop?

### 2.1 Current ReAct loop limitations

The current executor (`executor.go`) runs a single ReAct loop:

```
Thought → Action → Observation → ... → Final Answer
```

This works well for simple tasks but has limitations:

1. **No planning phase** — the agent reacts step-by-step but never explicitly
   plans its approach, which can lead to unfocused exploration.
2. **No self-evaluation** — once the agent produces a `Final Answer`, the loop
   ends. There is no quality check; a mediocre answer is returned as-is.
3. **No iterative refinement** — if the answer is incomplete or wrong, there is
   no mechanism to retry with feedback.
4. **No independent evaluation** — the same agent that produced the output
   evaluates it (if at all), creating a blind spot.

### 2.2 What the AgenticLoop adds

The AgenticLoop introduces a **meta-level** above the ReAct loop:

```
┌─────────────────────────────────────────────────┐
│                  AgenticLoop                     │
│                                                  │
│  ┌─────────┐    ┌──────────┐    ┌──────────┐   │
│  │  Plan   │───▶│ Execute  │───▶│ Evaluate │   │
│  └─────────┘    └──────────┘    └────┬─────┘   │
│                      ▲                │         │
│                      │           pass? │         │
│                      │                ▼         │
│                  ┌───┴────┐      ┌─────────┐   │
│                  │ Refine │◀────│  Fail   │   │
│                  └────────┘      └─────────┘   │
│                      │                          │
│                      ▼                          │
│               (max rounds?)                     │
│                      │                          │
│              ┌───────────────┐                 │
│              │  Final Answer │                 │
│              └───────────────┘                 │
└─────────────────────────────────────────────────┘
```

- **Plan**: the agent produces a numbered list of steps before acting. This
  is injected as context for the execution phase, keeping the agent focused.
- **Execute**: the agent runs the ReAct loop (existing executor) with the plan
  as additional context.
- **Evaluate**: an evaluator (same agent with an evaluation prompt, or a
  separate evaluator agent) scores the output against the expected output
  and quality criteria. Returns PASS or FAIL + feedback.
- **Refine**: on FAIL, the feedback is injected as context and the agent
  re-executes (or just rewrites, depending on configuration). The loop
  repeats up to `MaxRefinements` times.

---

## 3. New API Surface

### 3.1 `Loop` type (root package)

```go
// Loop is an optional execution strategy for an agent. When set on an
// Agent or Task, it replaces the default single-pass ReAct executor with
// a multi-phase Plan-Execute-Evaluate-Refine cycle.
type Loop interface {
    // Run executes the task using the loop strategy and returns the final
    // answer, collected facts, and an error.
    Run(ctx context.Context, a *Agent, t *Task, contextText string, log Logger) (string, []Fact, error)
}
```

### 3.2 `AgenticLoop` struct

```go
// AgenticLoop implements the Plan-Execute-Evaluate-Refine pattern.
type AgenticLoop struct {
    // MaxRefinements is the maximum number of evaluate→refine cycles
    // after the initial execution. If <= 0, defaults to 2.
    MaxRefinements int

    // Evaluator is an optional separate agent that evaluates the output.
    // If nil, the same agent evaluates itself with a dedicated prompt.
    Evaluator *Agent

    // EvaluationPrompt is an optional custom prompt template for the
    // evaluation phase. If empty, a default template is used.
    // It receives {task}, {expected_output}, {actual_output} as variables.
    EvaluationPrompt string

    // PassThreshold is the minimum evaluation score (0-100) to consider
    // the output acceptable. If <= 0, defaults to 70.
    // The evaluator must return a JSON object with a "score" field (0-100)
    // and an optional "feedback" field.
    PassThreshold int

    // SkipPlan, when true, skips the planning phase and goes straight to
    // execution. Useful for simple tasks with tools.
    SkipPlan bool
}
```

### 3.3 Functional options

```go
// WithMaxRefinements sets the maximum number of refinement cycles.
func WithMaxRefinements(n int) func(*AgenticLoop)

// WithEvaluator sets a separate evaluator agent.
func WithEvaluator(a *Agent) func(*AgenticLoop)

// WithEvaluationPrompt sets a custom evaluation prompt template.
func WithEvaluationPrompt(template string) func(*AgenticLoop)

// WithPassThreshold sets the minimum passing score (0-100).
func WithPassThreshold(score int) func(*AgenticLoop)

// WithSkipPlan skips the planning phase.
func WithSkipPlan() func(*AgenticLoop)
```

### 3.4 New `Agent` and `Task` fields

```go
type Agent struct {
    // ... existing fields ...

    // Loop, when set, replaces the default ReAct executor for all tasks
    // executed by this agent (unless the task overrides it).
    Loop Loop
}

type Task struct {
    // ... existing fields ...

    // Loop, when set, overrides the agent's loop for this specific task.
    Loop Loop
}
```

### 3.5 New sentinel errors

```go
var (
    // ErrEvaluationFailed is returned when the evaluator scores the output
    // below the pass threshold and all refinement attempts are exhausted.
    ErrEvaluationFailed = errors.New("crewai: output did not pass evaluation after all refinements")

    // ErrInvalidEvaluation is returned when the evaluator produces a response
    // that cannot be parsed as a valid evaluation result.
    ErrInvalidEvaluation = errors.New("crewai: evaluator returned an invalid response")
)
```

### 3.6 Constructor

```go
// NewAgenticLoop creates an AgenticLoop with the given options.
func NewAgenticLoop(opts ...func(*AgenticLoop)) *AgenticLoop
```

---

## 4. Execution Flow

### 4.1 `AgenticLoop.Run`

```
1. If not SkipPlan and agent has tools:
   a. Call LLM with planPrompt(task, contextText) → plan string
   b. Prepend plan to contextText

2. Execute phase:
   a. Call executeTask(ctx, agent, task, contextText, log)
      - This reuses the existing ReAct executor.
      - If Task.Structured is set, executeStructured runs as usual.
   b. Collect output and facts.

3. Evaluate phase:
   a. Build evaluationPrompt(task, expectedOutput, actualOutput)
   b. If Evaluator is set, call Evaluator.LLM; else call Agent.LLM
      with an evaluation-specific system prompt.
   c. Parse evaluator response as JSON: {"score": int, "feedback": string}
   d. If parse fails → return ErrInvalidEvaluation (or retry once)

4. Decision:
   a. If score >= PassThreshold → return output, facts, nil
   b. If score < PassThreshold and refinements < MaxRefinements:
      - Append feedback to contextText
      - Go to step 2 (refine)
   c. If refinements exhausted → return ErrEvaluationFailed (wrapping last feedback)

5. Structured output integration:
   - If Task.Structured is set, the structured output path runs inside
     executeTask as usual. The evaluation checks the canonicalized JSON
     against the expected output, not the raw model response.
```

### 4.2 Planning prompt

```
You are about to work on the following task:

{task description}
{expected output}
{context from previous tasks}

Before acting, create a concise, numbered plan of the steps you will take.
Consider which tools you have available and how to use them.

Respond ONLY with the plan, numbered 1-N. No other text.
```

### 4.3 Evaluation prompt (default)

```
You are an evaluator. Your job is to assess whether the output meets the
expected quality and completeness for the task.

Task:
{task description}

Expected output:
{expected output}

Actual output:
{actual output}

Evaluate the actual output against the expected output. Respond ONLY with a
JSON object:
{
  "score": <integer 0-100>,
  "feedback": "<brief explanation of issues or confirmation of quality>"
}

Scoring guide:
- 90-100: excellent, fully meets expectations
- 70-89: good, minor issues
- 50-69: partial, significant gaps
- 0-49: poor, major problems
```

### 4.4 Refine prompt

```
The evaluator provided the following feedback on your previous output:

{feedback}

Score: {score}/{threshold}

Please revise your output to address the feedback. Re-execute the task with
the improvements in mind.
```

---

## 5. Interaction with Existing Features

### 5.1 Structured output

When `Task.Structured` is set, the AgenticLoop's execute phase calls
`executeStructured` (via `executeTask`) as usual. The structured output's own
repair loop runs first; then the AgenticLoop's evaluation phase evaluates
the canonicalized JSON against the expected output. This creates a two-layer
validation:

1. **Schema validation** (structured output repair loop): checks JSON shape.
2. **Quality evaluation** (AgenticLoop): checks content quality.

If the structured output repair loop fails (`ErrRepairBudgetExceeded`), the
AgenticLoop does NOT retry — the error propagates immediately. The
evaluation phase only runs on successfully validated JSON.

### 5.2 Guardrails

Guardrails run AFTER the AgenticLoop completes, in the same place they run
today (in `Crew.execute` after `executeTask` returns). The AgenticLoop is
transparent to guardrails — it returns a `(string, []Fact, error)` tuple
just like the ReAct executor.

### 5.3 Facts

Facts collected during the execute phase (from `FactSource` tools) are
returned alongside the final output. In refinement rounds, facts from all
rounds are accumulated and deduplicated by `PayloadHash` (same `dedupFacts`
mechanism).

### 5.4 Hierarchical process

The AgenticLoop works with the hierarchical process — the manager delegates
to an agent, and that agent uses its configured loop. The manager itself
does not use a loop (delegation is a single LLM call).

### 5.5 Memory

Memory is handled at the `Crew.execute` level, not in the executor. The
AgenticLoop's output is stored in memory just like the ReAct executor's
output. No changes needed.

---

## 6. File Changes

| File | Change | Description |
|------|--------|-------------|
| `loop.go` | **New** | `Loop` interface, `AgenticLoop` struct, `Run` method, planning/evaluation/refine prompts. |
| `loop_test.go` | **New** | Hermetic tests using `llm/mock`: plan→execute→evaluate→pass, plan→execute→evaluate→fail→refine→pass, max refinements exhausted, evaluator parse error, skip plan, no tools, structured output integration, facts collection. |
| `errors.go` | **Modified** | Add `ErrEvaluationFailed` and `ErrInvalidEvaluation`. |
| `agent.go` | **Modified** | Add `Loop Loop` field. |
| `task.go` | **Modified** | Add `Loop Loop` field. |
| `executor.go` | **Modified** | In `executeTask`, check `t.Loop` then `a.Loop` before defaulting to ReAct. |
| `prompts.go` | **Modified** | Add `buildPlanPrompt`, `buildEvaluationPrompt`, `buildRefinePrompt`. |
| `doc.go` | **Modified** | Document the AgenticLoop in the package overview. |
| `examples/agentic_loop/main.go` | **New** | Runnable example with mock LLM. |
| `docs/agents.md` | **Modified** | Add AgenticLoop section. |
| `docs/pt-BR/agents.md` | **Modified** | Portuguese mirror. |
| `PLAN.md` | **Modified** | Update roadmap: mark AgenticLoop as in-progress/done. |
| `PLAN.pt-BR.md` | **Modified** | Portuguese mirror. |
| `README.md` | **Modified** | Add AgenticLoop to "What's new" / feature list. |
| `README.pt-BR.md` | **Modified** | Portuguese mirror. |
| `CHANGELOG.md` | **Modified** | Add entry under `[Unreleased]`. |
| `CHANGELOG.pt-BR.md` | **Modified** | Portuguese mirror. |

---

## 7. `executeTask` modification

The current `executeTask` function is the entry point for task execution.
It will be modified to check for a configured loop:

```go
func executeTask(ctx context.Context, a *Agent, t *Task, contextText string, log Logger) (string, []Fact, error) {
    if a.LLM == nil {
        return "", nil, ErrNoLLM
    }

    // Check for a loop: task-level takes precedence over agent-level.
    if t != nil && t.Loop != nil {
        return t.Loop.Run(ctx, a, t, contextText, log)
    }
    if a.Loop != nil {
        return a.Loop.Run(ctx, a, t, contextText, log)
    }

    // Default: structured output or ReAct loop (existing behavior).
    if t != nil && t.Structured != nil {
        out, err := executeStructured(ctx, a, t, contextText, log)
        return out, nil, err
    }

    // ... existing ReAct loop code unchanged ...
}
```

---

## 8. Evaluation Response Parsing

The evaluator must return JSON: `{"score": int, "feedback": string}`.
Parsing uses the same `extractJSON` + `encoding/json` approach as the
structured output feature. If the evaluator returns non-JSON, the loop
retries the evaluation once with a repair prompt (similar to the structured
output repair). If parsing still fails, `ErrInvalidEvaluation` is returned.

```go
type evaluationResult struct {
    Score    int    `json:"score"`
    Feedback string `json:"feedback"`
}

func parseEvaluation(raw string) (evaluationResult, error) {
    cleaned := extractJSON(raw)
    var result evaluationResult
    if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
        return evaluationResult{}, fmt.Errorf("%w: %v", ErrInvalidEvaluation, err)
    }
    if result.Score < 0 || result.Score > 100 {
        return evaluationResult{}, fmt.Errorf("%w: score out of range (0-100)", ErrInvalidEvaluation)
    }
    return result, nil
}
```

---

## 9. Test Plan

All tests use `llm/mock` — no real network.

### 9.1 Unit tests (`loop_test.go`)

| Test | Description |
|------|-------------|
| `TestAgenticLoop_PassOnFirstEvaluation` | Plan → execute → evaluate (score 95) → pass. Verify output and facts. |
| `TestAgenticLoop_RefineThenPass` | Execute → evaluate (score 50) → refine → evaluate (score 85) → pass. Verify feedback was injected. |
| `TestAgenticLoop_MaxRefinementsExhausted` | Execute → evaluate (score 40) → refine → evaluate (score 40) → refine → evaluate (score 40) → `ErrEvaluationFailed`. MaxRefinements=2. |
| `TestAgenticLoop_EvaluatorParseError` | Evaluator returns non-JSON → repair → still non-JSON → `ErrInvalidEvaluation`. |
| `TestAgenticLoop_SkipPlan` | SkipPlan=true → no plan call, direct execute → evaluate → pass. Verify only 2 LLM calls (execute + evaluate). |
| `TestAgenticLoop_NoTools` | Agent without tools → plan is skipped → direct call → evaluate → pass. |
| `TestAgenticLoop_WithStructuredOutput` | Task with Structured set → execute runs structured path → evaluate checks JSON → pass. |
| `TestAgenticLoop_FactsCollection` | Agent with FactSource tool → facts collected across refinement rounds → deduplicated. |
| `TestAgenticLoop_SeparateEvaluator` | Evaluator agent set → evaluation uses evaluator's LLM, not the executor's. |
| `TestAgenticLoop_CustomEvaluationPrompt` | Custom prompt template → verify it appears in the evaluation message. |
| `TestAgenticLoop_TaskLoopOverridesAgentLoop` | Agent has Loop A, Task has Loop B → Task.Loop is used. |
| `TestAgenticLoop_DefaultReActUnchanged` | Agent/Task without Loop → existing ReAct behavior, no AgenticLoop code path. |

### 9.2 Example

`examples/agentic_loop/main.go` — a self-contained example using `llm/mock`
with scripted responses showing the full Plan-Execute-Evaluate-Refine cycle.

---

## 10. Implementation Order

1. **`errors.go`** — add `ErrEvaluationFailed`, `ErrInvalidEvaluation`.
2. **`prompts.go`** — add `buildPlanPrompt`, `buildEvaluationPrompt`,
   `buildRefinePrompt`, `buildEvalRepairPrompt`.
3. **`loop.go`** — `Loop` interface, `AgenticLoop` struct, `NewAgenticLoop`,
   `Run` method, `parseEvaluation` helper.
4. **`agent.go`** — add `Loop` field.
5. **`task.go`** — add `Loop` field.
6. **`executor.go`** — modify `executeTask` to check for `Loop`.
7. **`loop_test.go`** — all unit tests.
8. **`examples/agentic_loop/main.go`** — runnable example.
9. **`docs/agents.md`** + **`docs/pt-BR/agents.md`** — documentation.
10. **`doc.go`** — update package overview.
11. **`PLAN.md`** + **`PLAN.pt-BR.md`** — update roadmap status.
12. **`README.md`** + **`README.pt-BR.md`** — feature list + "What's new".
13. **`CHANGELOG.md`** + **`CHANGELOG.pt-BR.md`** — add entry.

---

## 11. Open Decisions

| Decision | Options | Recommendation |
|----------|---------|----------------|
| Should the evaluator use a separate `LLM` (not a full `Agent`)? | `Evaluator LLM` vs `Evaluator *Agent` | Use `*Agent` for flexibility (separate persona/backstory for evaluation). A bare `LLM` can be wrapped in an agent easily. |
| Should the plan be a string or a structured JSON array of steps? | String (simple) vs JSON (parseable) | String — keeps it simple and avoids a parsing failure mode. The plan is guidance, not a program. |
| Should refinement re-execute (with tools) or just rewrite? | Re-execute vs rewrite-only | Re-execute by default (the agent can use tools again), with an option `RefineRewriteOnly bool` to skip tool use in refinement. |
| Should the evaluation score be required as JSON, or accept free text? | JSON only vs free text with regex | JSON only — consistent with structured output, parseable, and the repair prompt handles malformed responses. |
| Should `AgenticLoop` be nestable (a loop inside a loop)? | Yes vs no | No — one loop per task. Nesting adds complexity without clear benefit. |