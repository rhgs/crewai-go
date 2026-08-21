package crewai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
)

// Loop is an optional execution strategy for an agent. When set on an Agent
// or Task, it replaces the default single-pass ReAct executor with a
// multi-phase Plan-Execute-Evaluate-Refine cycle.
type Loop interface {
	// Run executes the task using the loop strategy and returns the final
	// answer, collected facts, and an error.
	Run(ctx context.Context, a *Agent, t *Task, contextText string, log *slog.Logger) (string, []Fact, error)
}

// AgenticLoop implements the Plan-Execute-Evaluate-Refine pattern.
type AgenticLoop struct {
	// MaxRefinements is the maximum number of evaluate→refine cycles after
	// the initial execution. If <= 0, defaults to 2.
	MaxRefinements int

	// Evaluator is an optional separate agent that evaluates the output. If
	// nil, the same agent evaluates itself with a dedicated prompt.
	Evaluator *Agent

	// EvaluationPrompt is an optional custom prompt template for the
	// evaluation phase. If empty, a default template is used. It receives
	// {task}, {expected_output}, {actual_output} as variables.
	EvaluationPrompt string

	// PassThreshold is the minimum evaluation score (0-100) to consider the
	// output acceptable. If <= 0, defaults to 70.
	PassThreshold int

	// SkipPlan, when true, skips the planning phase and goes straight to
	// execution. Useful for simple tasks with tools.
	SkipPlan bool

	// RefineRewriteOnly, when true, makes the refine phase rewrite the
	// previous output using the feedback WITHOUT re-running tools. When
	// false (the default), the refine phase re-executes the full task
	// (tools included).
	RefineRewriteOnly bool
}

// NewAgenticLoop creates an AgenticLoop with the given options.
func NewAgenticLoop(opts ...func(*AgenticLoop)) *AgenticLoop {
	l := &AgenticLoop{}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// WithMaxRefinements sets the maximum number of refinement cycles.
func WithMaxRefinements(n int) func(*AgenticLoop) {
	return func(l *AgenticLoop) { l.MaxRefinements = n }
}

// WithEvaluator sets a separate evaluator agent.
func WithEvaluator(a *Agent) func(*AgenticLoop) {
	return func(l *AgenticLoop) { l.Evaluator = a }
}

// WithEvaluationPrompt sets a custom evaluation prompt template.
func WithEvaluationPrompt(template string) func(*AgenticLoop) {
	return func(l *AgenticLoop) { l.EvaluationPrompt = template }
}

// WithPassThreshold sets the minimum passing score (0-100).
func WithPassThreshold(score int) func(*AgenticLoop) {
	return func(l *AgenticLoop) { l.PassThreshold = score }
}

// WithSkipPlan skips the planning phase.
func WithSkipPlan() func(*AgenticLoop) {
	return func(l *AgenticLoop) { l.SkipPlan = true }
}

// WithRefineRewriteOnly makes the refine phase rewrite-only (no tools).
func WithRefineRewriteOnly() func(*AgenticLoop) {
	return func(l *AgenticLoop) { l.RefineRewriteOnly = true }
}

// Run executes the task using the Plan-Execute-Evaluate-Refine cycle.
func (l *AgenticLoop) Run(ctx context.Context, a *Agent, t *Task, contextText string, log *slog.Logger) (string, []Fact, error) {
	maxRefinements := l.MaxRefinements
	if maxRefinements <= 0 {
		maxRefinements = defaultMaxRefinements
	}
	threshold := l.PassThreshold
	if threshold <= 0 {
		threshold = defaultPassThreshold
	}

	// Plan phase: skipped when SkipPlan is set or the agent has no tools.
	if !l.SkipPlan && len(effectiveTools(a, t)) > 0 {
		emitEvent(ctx, CrewEvent{Type: EventLoopPhase, Task: taskLabel(t, 0), Agent: a.Role, Phase: "plan"})
		plan, err := l.plan(ctx, a, t, contextText)
		if err != nil {
			return "", nil, err
		}
		if strings.TrimSpace(plan) != "" {
			if contextText != "" {
				contextText = plan + "\n\n" + contextText
			} else {
				contextText = plan
			}
		}
	}

	var allFacts []Fact

	// Initial execute phase.
	emitEvent(ctx, CrewEvent{Type: EventLoopPhase, Task: taskLabel(t, 0), Agent: a.Role, Phase: "execute"})
	output, facts, err := executeTaskDefault(ctx, a, t, contextText, log)
	if err != nil {
		return "", allFacts, err
	}
	allFacts = dedupFacts(allFacts, facts)

	for round := 0; ; round++ {
		select {
		case <-ctx.Done():
			return "", allFacts, ctx.Err()
		default:
		}

		// Evaluate phase.
		emitEvent(ctx, CrewEvent{Type: EventLoopPhase, Task: taskLabel(t, 0), Agent: a.Role, Phase: "evaluate", Iteration: round})
		score, feedback, err := l.evaluate(ctx, a, t, output)
		if err != nil {
			return "", allFacts, err
		}

		if score >= threshold {
			return output, allFacts, nil
		}

		// Refine phase.
		if round >= maxRefinements {
			return "", allFacts, fmt.Errorf("%w: %s", ErrEvaluationFailed, feedback)
		}

		refine := buildRefinePrompt(feedback, score, threshold)
		if l.RefineRewriteOnly {
			// Rewrite-only: revise the previous output directly, without
			// re-running tools. The rewritten output is re-evaluated on the
			// next iteration.
			emitEvent(ctx, CrewEvent{Type: EventLoopPhase, Task: taskLabel(t, 0), Agent: a.Role, Phase: "rewrite", Iteration: round})
			output, err = l.rewrite(ctx, a, t, output, refine)
			if err != nil {
				return "", allFacts, err
			}
			continue
		}

		// Re-execute with feedback injected as context.
		emitEvent(ctx, CrewEvent{Type: EventLoopPhase, Task: taskLabel(t, 0), Agent: a.Role, Phase: "refine", Iteration: round})
		if contextText != "" {
			contextText = contextText + "\n\n" + refine
		} else {
			contextText = refine
		}
		output, facts, err = executeTaskDefault(ctx, a, t, contextText, log)
		if err != nil {
			return "", allFacts, err
		}
		allFacts = dedupFacts(allFacts, facts)
	}
}

// plan asks the agent to produce a numbered plan before acting.
func (l *AgenticLoop) plan(ctx context.Context, a *Agent, t *Task, contextText string) (string, error) {
	messages := []Message{
		SystemMessage(buildSystemPrompt(a)),
		UserMessage(buildPlanPrompt(t, contextText)),
	}
	out, err := a.LLM.Call(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("agent %q: plan: %w", a.Role, err)
	}
	return strings.TrimSpace(out), nil
}

// evaluate scores the output against the expected output. It uses the
// evaluator agent's LLM if set, otherwise the executing agent's LLM. The
// evaluation is a pure LLM call — it never goes through executeTask or any
// loop.
func (l *AgenticLoop) evaluate(ctx context.Context, a *Agent, t *Task, actualOutput string) (int, string, error) {
	evaluator := l.Evaluator
	if evaluator == nil {
		evaluator = a
	}

	prompt := l.EvaluationPrompt
	if prompt == "" {
		prompt = buildEvaluationPrompt(t, actualOutput)
	} else {
		prompt = interpolateEvaluationPrompt(prompt, t, actualOutput)
	}

	messages := []Message{
		SystemMessage(buildSystemPrompt(evaluator)),
		UserMessage(prompt),
	}

	raw, err := evaluator.LLM.Call(ctx, messages)
	if err != nil {
		return 0, "", fmt.Errorf("evaluator %q: %w", evaluator.Role, err)
	}

	result, err := parseEvaluation(raw)
	if err != nil {
		// Retry once with a repair prompt.
		repair := []Message{
			SystemMessage(buildSystemPrompt(evaluator)),
			UserMessage(buildEvalRepairPrompt(raw)),
		}
		raw2, err2 := evaluator.LLM.Call(ctx, repair)
		if err2 != nil {
			return 0, "", fmt.Errorf("evaluator %q: %w", evaluator.Role, err2)
		}
		result, err = parseEvaluation(raw2)
		if err != nil {
			return 0, "", err
		}
	}

	return *result.Score, result.Feedback, nil
}

// rewrite asks the model to revise the previous output using the feedback,
// without re-running tools.
func (l *AgenticLoop) rewrite(ctx context.Context, a *Agent, t *Task, previous, refine string) (string, error) {
	messages := []Message{
		SystemMessage(buildSystemPrompt(a)),
		UserMessage(buildTaskPrompt(t, "")),
		AssistantMessage(previous),
		UserMessage(refine),
	}
	out, err := a.LLM.Call(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("agent %q: refine: %w", a.Role, err)
	}
	return strings.TrimSpace(stripFinalAnswer(out)), nil
}

// evaluationResult is the parsed output of the evaluation phase.
type evaluationResult struct {
	Score    *int   `json:"score"`
	Feedback string `json:"feedback"`
}

// parseEvaluation parses the evaluator's JSON response. A missing "score"
// field is rejected (not treated as 0).
func parseEvaluation(raw string) (evaluationResult, error) {
	cleaned := extractJSON(raw)
	var result evaluationResult
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return evaluationResult{}, fmt.Errorf("%w: %v", ErrInvalidEvaluation, err)
	}
	if result.Score == nil {
		return evaluationResult{}, fmt.Errorf("%w: missing score field", ErrInvalidEvaluation)
	}
	if *result.Score < 0 || *result.Score > 100 {
		return evaluationResult{}, fmt.Errorf("%w: score out of range (0-100)", ErrInvalidEvaluation)
	}
	return result, nil
}

// interpolateEvaluationPrompt substitutes {task}, {expected_output}, and
// {actual_output} in a custom evaluation prompt template.
func interpolateEvaluationPrompt(template string, t *Task, actualOutput string) string {
	repl := map[string]string{
		"{task}":            t.Description,
		"{expected_output}": t.ExpectedOutput,
		"{actual_output}":   actualOutput,
	}
	for k, v := range repl {
		template = strings.ReplaceAll(template, k, v)
	}
	return template
}

// defaultMaxRefinements is the refinement limit used when MaxRefinements <= 0.
const defaultMaxRefinements = 2

// defaultPassThreshold is the passing score used when PassThreshold <= 0.
const defaultPassThreshold = 70
