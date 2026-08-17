package crewai_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/rhgs/crewai-go"
	"github.com/rhgs/crewai-go/llm/mock"
)

// testLogger returns a logger that discards output, for use in loop tests.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// scriptedLLM returns a mock LLM whose Handler dispatches on message content.
// It is a convenience for driving the multi-phase AgenticLoop deterministically.
func scriptedLLM(fn func(ctx context.Context, msgs []crewai.Message) (string, error)) *mock.LLM {
	return &mock.LLM{Handler: fn}
}

// lastUserContent returns the content of the last user-role message.
func lastUserContent(msgs []crewai.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == crewai.RoleUser {
			return msgs[i].Content
		}
	}
	return ""
}

func TestAgenticLoop_PassOnFirstEvaluation(t *testing.T) {
	// Plan → execute → evaluate (score 95) → pass.
	llm := scriptedLLM(func(_ context.Context, msgs []crewai.Message) (string, error) {
		content := lastUserContent(msgs)
		switch {
		case strings.Contains(content, "create a concise, numbered plan"):
			return "1. Do the thing", nil
		case strings.Contains(content, "You are an evaluator"):
			return `{"score": 95, "feedback": "great"}`, nil
		default:
			return "Final Answer: the result", nil
		}
	})

	a := crewai.NewAgent("A", "", "", llm)
	a.WithTools(crewai.NewTool("t", "a tool", func(_ context.Context, _ string) (string, error) {
		return "tool result", nil
	}))
	task := crewai.NewTask("do the thing", "a result", a)

	loop := crewai.NewAgenticLoop()
	out, facts, err := loop.Run(context.Background(), a, task, "", testLogger())
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if out != "the result" {
		t.Errorf("output = %q, want %q", out, "the result")
	}
	if len(facts) != 0 {
		t.Errorf("unexpected facts: %v", facts)
	}
}

func TestAgenticLoop_RefineThenPass(t *testing.T) {
	// Execute → evaluate (score 50) → refine → evaluate (score 85) → pass.
	var evalCalls int
	llm := scriptedLLM(func(_ context.Context, msgs []crewai.Message) (string, error) {
		content := lastUserContent(msgs)
		switch {
		case strings.Contains(content, "You are an evaluator"):
			evalCalls++
			if evalCalls == 1 {
				return `{"score": 50, "feedback": "too short"}`, nil
			}
			return `{"score": 85, "feedback": "ok now"}`, nil
		default:
			return "Final Answer: the result", nil
		}
	})

	a := crewai.NewAgent("A", "", "", llm)
	task := crewai.NewTask("do the thing", "a result", a)

	loop := crewai.NewAgenticLoop(crewai.WithMaxRefinements(2))
	out, _, err := loop.Run(context.Background(), a, task, "", testLogger())
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if out != "the result" {
		t.Errorf("output = %q", out)
	}
	if evalCalls != 2 {
		t.Errorf("expected 2 evaluation calls, got %d", evalCalls)
	}
}

func TestAgenticLoop_MaxRefinementsExhausted(t *testing.T) {
	// Always score 40 → exhaust refinements → ErrEvaluationFailed.
	llm := scriptedLLM(func(_ context.Context, msgs []crewai.Message) (string, error) {
		content := lastUserContent(msgs)
		if strings.Contains(content, "You are an evaluator") {
			return `{"score": 40, "feedback": "bad"}`, nil
		}
		return "Final Answer: the result", nil
	})

	a := crewai.NewAgent("A", "", "", llm)
	task := crewai.NewTask("do the thing", "a result", a)

	loop := crewai.NewAgenticLoop(crewai.WithMaxRefinements(2))
	_, _, err := loop.Run(context.Background(), a, task, "", testLogger())
	if !errors.Is(err, crewai.ErrEvaluationFailed) {
		t.Fatalf("error = %v, want ErrEvaluationFailed", err)
	}
}

func TestAgenticLoop_EvaluatorParseError(t *testing.T) {
	// Evaluator returns non-JSON → repair → still non-JSON → ErrInvalidEvaluation.
	llm := scriptedLLM(func(_ context.Context, msgs []crewai.Message) (string, error) {
		content := lastUserContent(msgs)
		if strings.Contains(content, "You are an evaluator") || strings.Contains(content, "not valid JSON") {
			return "this is not json at all", nil
		}
		return "Final Answer: the result", nil
	})

	a := crewai.NewAgent("A", "", "", llm)
	task := crewai.NewTask("do the thing", "a result", a)

	loop := crewai.NewAgenticLoop()
	_, _, err := loop.Run(context.Background(), a, task, "", testLogger())
	if !errors.Is(err, crewai.ErrInvalidEvaluation) {
		t.Fatalf("error = %v, want ErrInvalidEvaluation", err)
	}
}

func TestAgenticLoop_MissingScoreField(t *testing.T) {
	// Evaluator returns JSON without "score" → ErrInvalidEvaluation.
	llm := scriptedLLM(func(_ context.Context, msgs []crewai.Message) (string, error) {
		content := lastUserContent(msgs)
		if strings.Contains(content, "You are an evaluator") || strings.Contains(content, "not valid JSON") {
			return `{"feedback": "no score here"}`, nil
		}
		return "Final Answer: the result", nil
	})

	a := crewai.NewAgent("A", "", "", llm)
	task := crewai.NewTask("do the thing", "a result", a)

	loop := crewai.NewAgenticLoop()
	_, _, err := loop.Run(context.Background(), a, task, "", testLogger())
	if !errors.Is(err, crewai.ErrInvalidEvaluation) {
		t.Fatalf("error = %v, want ErrInvalidEvaluation", err)
	}
}

func TestAgenticLoop_SkipPlan(t *testing.T) {
	// SkipPlan=true → no plan call, direct execute → evaluate → pass.
	var planCalls int
	llm := scriptedLLM(func(_ context.Context, msgs []crewai.Message) (string, error) {
		content := lastUserContent(msgs)
		switch {
		case strings.Contains(content, "create a concise, numbered plan"):
			planCalls++
			return "1. plan", nil
		case strings.Contains(content, "You are an evaluator"):
			return `{"score": 90, "feedback": "ok"}`, nil
		default:
			return "Final Answer: the result", nil
		}
	})

	a := crewai.NewAgent("A", "", "", llm)
	a.WithTools(crewai.NewTool("t", "a tool", func(_ context.Context, _ string) (string, error) {
		return "tool result", nil
	}))
	task := crewai.NewTask("do the thing", "a result", a)

	loop := crewai.NewAgenticLoop(crewai.WithSkipPlan())
	if _, _, err := loop.Run(context.Background(), a, task, "", testLogger()); err != nil {
		t.Fatalf("error: %v", err)
	}
	if planCalls != 0 {
		t.Errorf("plan was called %d times, want 0", planCalls)
	}
}

func TestAgenticLoop_NoTools(t *testing.T) {
	// Agent without tools → plan skipped → direct call → evaluate → pass.
	var planCalls int
	llm := scriptedLLM(func(_ context.Context, msgs []crewai.Message) (string, error) {
		content := lastUserContent(msgs)
		switch {
		case strings.Contains(content, "create a concise, numbered plan"):
			planCalls++
			return "1. plan", nil
		case strings.Contains(content, "You are an evaluator"):
			return `{"score": 90, "feedback": "ok"}`, nil
		default:
			return "Final Answer: the result", nil
		}
	})

	a := crewai.NewAgent("A", "", "", llm) // no tools
	task := crewai.NewTask("do the thing", "a result", a)

	loop := crewai.NewAgenticLoop()
	if _, _, err := loop.Run(context.Background(), a, task, "", testLogger()); err != nil {
		t.Fatalf("error: %v", err)
	}
	if planCalls != 0 {
		t.Errorf("plan was called %d times, want 0 (no tools)", planCalls)
	}
}

func TestAgenticLoop_SeparateEvaluator(t *testing.T) {
	// Evaluator agent set → evaluation uses evaluator's LLM, not executor's.
	var executorEvalCalls int
	executorLLM := scriptedLLM(func(_ context.Context, msgs []crewai.Message) (string, error) {
		content := lastUserContent(msgs)
		if strings.Contains(content, "You are an evaluator") {
			executorEvalCalls++
		}
		return "Final Answer: the result", nil
	})
	evaluatorLLM := scriptedLLM(func(_ context.Context, _ []crewai.Message) (string, error) {
		return `{"score": 95, "feedback": "great"}`, nil
	})

	a := crewai.NewAgent("A", "", "", executorLLM)
	evaluator := crewai.NewAgent("Evaluator", "", "", evaluatorLLM)
	task := crewai.NewTask("do the thing", "a result", a)

	loop := crewai.NewAgenticLoop(crewai.WithEvaluator(evaluator))
	if _, _, err := loop.Run(context.Background(), a, task, "", testLogger()); err != nil {
		t.Fatalf("error: %v", err)
	}
	if executorEvalCalls != 0 {
		t.Errorf("executor LLM was used for evaluation %d times, want 0", executorEvalCalls)
	}
}

func TestAgenticLoop_CustomEvaluationPrompt(t *testing.T) {
	// Custom prompt template → verify it appears in the evaluation message.
	var sawCustom bool
	llm := scriptedLLM(func(_ context.Context, msgs []crewai.Message) (string, error) {
		content := lastUserContent(msgs)
		if strings.Contains(content, "CUSTOM EVAL MARKER") {
			sawCustom = true
			return `{"score": 90, "feedback": "ok"}`, nil
		}
		return "Final Answer: the result", nil
	})

	a := crewai.NewAgent("A", "", "", llm)
	task := crewai.NewTask("do the thing", "a result", a)

	loop := crewai.NewAgenticLoop(crewai.WithEvaluationPrompt("CUSTOM EVAL MARKER {task} {expected_output} {actual_output}"))
	if _, _, err := loop.Run(context.Background(), a, task, "", testLogger()); err != nil {
		t.Fatalf("error: %v", err)
	}
	if !sawCustom {
		t.Error("custom evaluation prompt was not used")
	}
}

func TestAgenticLoop_RefineRewriteOnly(t *testing.T) {
	// RefineRewriteOnly=true → refine phase does not re-run tools.
	var toolCalls int
	tool := crewai.NewTool("t", "a tool", func(_ context.Context, _ string) (string, error) {
		toolCalls++
		return "tool result", nil
	})

	var evalCalls int
	var execCalls int
	llm := scriptedLLM(func(_ context.Context, msgs []crewai.Message) (string, error) {
		content := lastUserContent(msgs)
		switch {
		case strings.Contains(content, "create a concise, numbered plan"):
			// Plan phase: return a plan, not a tool action.
			return "1. Use the tool", nil
		case strings.Contains(content, "You are an evaluator"):
			evalCalls++
			if evalCalls == 1 {
				return `{"score": 50, "feedback": "too short"}`, nil
			}
			return `{"score": 90, "feedback": "ok"}`, nil
		case strings.Contains(content, "revise your output"):
			// Rewrite phase: return a revised answer directly.
			return "Final Answer: revised result", nil
		default:
			// Execute phase: invoke the tool once, then answer.
			execCalls++
			if execCalls == 1 {
				return "Action: t\nAction Input: x\n", nil
			}
			return "Final Answer: initial result", nil
		}
	})

	a := crewai.NewAgent("A", "", "", llm)
	a.WithTools(tool)
	task := crewai.NewTask("do the thing", "a result", a)

	loop := crewai.NewAgenticLoop(crewai.WithRefineRewriteOnly(), crewai.WithMaxRefinements(2))
	out, _, err := loop.Run(context.Background(), a, task, "", testLogger())
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if out != "revised result" {
		t.Errorf("output = %q, want %q", out, "revised result")
	}
	// The tool should be called exactly once (initial execute), not again
	// during the rewrite-only refine.
	if toolCalls != 1 {
		t.Errorf("tool called %d times, want 1 (rewrite-only refine)", toolCalls)
	}
}

func TestAgenticLoop_TaskLoopOverridesAgentLoop(t *testing.T) {
	// Agent has Loop A, Task has Loop B → Task.Loop is used.
	// We verify via executeTask dispatch: a task-level loop that returns a
	// sentinel output proves it took precedence.
	agentLoop := &stubLoop{result: "agent-loop"}
	taskLoop := &stubLoop{result: "task-loop"}

	a := crewai.NewAgent("A", "", "", mock.New("x"))
	a.Loop = agentLoop
	task := crewai.NewTask("do the thing", "a result", a)
	task.Loop = taskLoop

	crew := crewai.NewCrew([]*crewai.Agent{a}, []*crewai.Task{task})
	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if out.Final != "task-loop" {
		t.Errorf("Final = %q, want %q (task loop should override agent loop)", out.Final, "task-loop")
	}
}

func TestAgenticLoop_DefaultReActUnchanged(t *testing.T) {
	// Agent/Task without Loop → existing ReAct behavior.
	llm := mock.New("Final Answer: plain result")
	a := crewai.NewAgent("A", "", "", llm)
	task := crewai.NewTask("do the thing", "a result", a)

	crew := crewai.NewCrew([]*crewai.Agent{a}, []*crewai.Task{task})
	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if out.Final != "plain result" {
		t.Errorf("Final = %q, want %q", out.Final, "plain result")
	}
}

func TestAgenticLoop_NoInfiniteRecursion(t *testing.T) {
	// A loop configured on the agent completes in bounded LLM calls.
	var calls int
	llm := scriptedLLM(func(_ context.Context, msgs []crewai.Message) (string, error) {
		calls++
		content := lastUserContent(msgs)
		if strings.Contains(content, "You are an evaluator") {
			return `{"score": 95, "feedback": "ok"}`, nil
		}
		return "Final Answer: done", nil
	})

	a := crewai.NewAgent("A", "", "", llm)
	a.Loop = crewai.NewAgenticLoop()
	task := crewai.NewTask("do the thing", "a result", a)

	crew := crewai.NewCrew([]*crewai.Agent{a}, []*crewai.Task{task})
	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if out.Final != "done" {
		t.Errorf("Final = %q, want %q", out.Final, "done")
	}
	// execute (1) + evaluate (1) = 2 calls. No recursion.
	if calls != 2 {
		t.Errorf("LLM called %d times, want 2 (no recursion)", calls)
	}
}

func TestAgenticLoop_WithStructuredOutput(t *testing.T) {
	// Task with Structured set → execute runs structured path → evaluate
	// checks the canonicalized JSON → pass.
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
		"required": []string{"name"},
	}
	structured, err := crewai.NewStructuredOutput(schema)
	if err != nil {
		t.Fatalf("NewStructuredOutput: %v", err)
	}

	llm := scriptedLLM(func(_ context.Context, msgs []crewai.Message) (string, error) {
		content := lastUserContent(msgs)
		if strings.Contains(content, "You are an evaluator") {
			return `{"score": 95, "feedback": "ok"}`, nil
		}
		// Structured execute phase: return valid JSON.
		return `{"name": "Go"}`, nil
	})

	a := crewai.NewAgent("A", "", "", llm)
	task := crewai.NewTask("produce a name", "a JSON object", a)
	task.Structured = structured

	loop := crewai.NewAgenticLoop()
	out, _, err := loop.Run(context.Background(), a, task, "", testLogger())
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !strings.Contains(out, `"name"`) {
		t.Errorf("output = %q, want canonicalized JSON with name", out)
	}
}

func TestAgenticLoop_FactsCollection(t *testing.T) {
	// Agent with FactSource tool → facts collected across refinement rounds
	// → deduplicated.
	fact := crewai.NewFact("claim", "org", "https://example.com", []byte("payload"))
	tool := crewai.NewFactSourceTool("f", "", func(_ context.Context, _ string) (string, error) {
		return "x", nil
	}, func(_ context.Context, _ string) []crewai.Fact { return []crewai.Fact{fact} })

	var evalCalls int
	var execCalls int
	llm := scriptedLLM(func(_ context.Context, msgs []crewai.Message) (string, error) {
		content := lastUserContent(msgs)
		switch {
		case strings.Contains(content, "create a concise, numbered plan"):
			return "1. Use the tool", nil
		case strings.Contains(content, "You are an evaluator"):
			evalCalls++
			if evalCalls == 1 {
				return `{"score": 50, "feedback": "redo"}`, nil
			}
			return `{"score": 90, "feedback": "ok"}`, nil
		default:
			// Execute phase: invoke the tool once, then answer.
			execCalls++
			if execCalls%2 == 1 {
				return "Action: f\nAction Input: x\n", nil
			}
			return "Final Answer: done", nil
		}
	})

	a := crewai.NewAgent("A", "", "", llm)
	a.WithTools(tool)
	task := crewai.NewTask("do the thing", "a result", a)

	loop := crewai.NewAgenticLoop(crewai.WithMaxRefinements(2))
	_, facts, err := loop.Run(context.Background(), a, task, "", testLogger())
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	// The same fact is produced in both execute rounds but deduplicated.
	if len(facts) != 1 {
		t.Fatalf("expected 1 deduplicated fact, got %d", len(facts))
	}
}

func TestAgenticLoop_WithPassThreshold(t *testing.T) {
	// WithPassThreshold sets a custom threshold; a score below it fails.
	llm := scriptedLLM(func(_ context.Context, msgs []crewai.Message) (string, error) {
		content := lastUserContent(msgs)
		if strings.Contains(content, "You are an evaluator") {
			return `{"score": 80, "feedback": "ok"}`, nil
		}
		return "Final Answer: the result", nil
	})

	a := crewai.NewAgent("A", "", "", llm)
	task := crewai.NewTask("do the thing", "a result", a)

	// Threshold 90 > score 80 → should fail after refinements.
	loop := crewai.NewAgenticLoop(crewai.WithPassThreshold(90), crewai.WithMaxRefinements(0))
	_, _, err := loop.Run(context.Background(), a, task, "", testLogger())
	if !errors.Is(err, crewai.ErrEvaluationFailed) {
		t.Fatalf("error = %v, want ErrEvaluationFailed", err)
	}
}

func TestAgenticLoop_PlanError(t *testing.T) {
	// LLM returns an error during the plan phase → error propagates.
	llm := scriptedLLM(func(_ context.Context, _ []crewai.Message) (string, error) {
		return "", errors.New("plan failed")
	})

	a := crewai.NewAgent("A", "", "", llm)
	a.WithTools(crewai.NewTool("t", "a tool", func(_ context.Context, _ string) (string, error) {
		return "x", nil
	}))
	task := crewai.NewTask("do the thing", "a result", a)

	loop := crewai.NewAgenticLoop()
	_, _, err := loop.Run(context.Background(), a, task, "", testLogger())
	if err == nil || !strings.Contains(err.Error(), "plan") {
		t.Fatalf("error = %v, want plan error", err)
	}
}

func TestAgenticLoop_RewriteError(t *testing.T) {
	// LLM returns an error during the rewrite phase → error propagates.
	var evalCalls int
	llm := scriptedLLM(func(_ context.Context, msgs []crewai.Message) (string, error) {
		content := lastUserContent(msgs)
		switch {
		case strings.Contains(content, "You are an evaluator"):
			evalCalls++
			if evalCalls == 1 {
				return `{"score": 50, "feedback": "redo"}`, nil
			}
			return `{"score": 90, "feedback": "ok"}`, nil
		case strings.Contains(content, "revise your output"):
			return "", errors.New("rewrite failed")
		default:
			return "Final Answer: the result", nil
		}
	})

	a := crewai.NewAgent("A", "", "", llm)
	task := crewai.NewTask("do the thing", "a result", a)

	loop := crewai.NewAgenticLoop(crewai.WithRefineRewriteOnly())
	_, _, err := loop.Run(context.Background(), a, task, "", testLogger())
	if err == nil || !strings.Contains(err.Error(), "refine") {
		t.Fatalf("error = %v, want refine error", err)
	}
}

func TestAgenticLoop_ScoreOutOfRange(t *testing.T) {
	// Evaluator returns score > 100 → ErrInvalidEvaluation.
	llm := scriptedLLM(func(_ context.Context, msgs []crewai.Message) (string, error) {
		content := lastUserContent(msgs)
		if strings.Contains(content, "You are an evaluator") || strings.Contains(content, "not valid JSON") {
			return `{"score": 150, "feedback": "x"}`, nil
		}
		return "Final Answer: the result", nil
	})

	a := crewai.NewAgent("A", "", "", llm)
	task := crewai.NewTask("do the thing", "a result", a)

	loop := crewai.NewAgenticLoop()
	_, _, err := loop.Run(context.Background(), a, task, "", testLogger())
	if !errors.Is(err, crewai.ErrInvalidEvaluation) {
		t.Fatalf("error = %v, want ErrInvalidEvaluation", err)
	}
}

func TestAgenticLoop_EvaluatorLLMError(t *testing.T) {
	// Evaluator LLM returns an error → error propagates.
	llm := scriptedLLM(func(_ context.Context, msgs []crewai.Message) (string, error) {
		content := lastUserContent(msgs)
		if strings.Contains(content, "You are an evaluator") {
			return "", errors.New("evaluator down")
		}
		return "Final Answer: the result", nil
	})

	a := crewai.NewAgent("A", "", "", llm)
	task := crewai.NewTask("do the thing", "a result", a)

	loop := crewai.NewAgenticLoop()
	_, _, err := loop.Run(context.Background(), a, task, "", testLogger())
	if err == nil || !strings.Contains(err.Error(), "evaluator") {
		t.Fatalf("error = %v, want evaluator error", err)
	}
}

// stubLoop is a minimal Loop implementation for dispatch tests.
type stubLoop struct {
	result string
}

func (s *stubLoop) Run(_ context.Context, _ *crewai.Agent, _ *crewai.Task, _ string, _ *slog.Logger) (string, []crewai.Fact, error) {
	return s.result, nil, nil
}
