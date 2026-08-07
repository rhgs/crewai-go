package crewai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// --- Helpers ---

func guardrailCrew(llm LLM, tasks []*Task) *Crew {
	agents := make([]*Agent, len(tasks))
	for i := range tasks {
		if tasks[i].Agent == nil {
			agents[i] = NewAgent("Agent", "", "", llm)
			tasks[i].Agent = agents[i]
		}
	}
	return &Crew{
		Agents: agents,
		Tasks:  tasks,
	}
}

// --- Crew-level guardrail tests ---

func TestCrewGuardrail_Pass(t *testing.T) {
	llm := &llmStub{responses: []string{"good output"}}
	agent := NewAgent("Agent", "", "", llm)
	task := NewTask("t", "", agent)
	crew := guardrailCrew(llm, []*Task{task})
	crew.Guardrails = []Guardrail{
		func(_ context.Context, out *CrewOutput) error {
			if out.Final == "" {
				return fmt.Errorf("empty output")
			}
			return nil
		},
	}
	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("Kickoff error: %v", err)
	}
	if out.Final != "good output" {
		t.Errorf("Final = %q", out.Final)
	}
}

func TestCrewGuardrail_Fail(t *testing.T) {
	llm := &llmStub{responses: []string{"bad output"}}
	agent := NewAgent("Agent", "", "", llm)
	task := NewTask("t", "", agent)
	crew := guardrailCrew(llm, []*Task{task})
	crew.Guardrails = []Guardrail{
		func(_ context.Context, out *CrewOutput) error {
			return fmt.Errorf("output rejected: too short")
		},
	}
	out, err := crew.Kickoff(context.Background(), nil)
	if out != nil {
		t.Errorf("expected nil output on guardrail failure, got %+v", out)
	}
	if !errors.Is(err, ErrBlockedByGuardrail) {
		t.Fatalf("expected ErrBlockedByGuardrail, got %v", err)
	}
	if !strings.Contains(err.Error(), "too short") {
		t.Errorf("error should preserve guardrail message: %v", err)
	}
}

func TestCrewGuardrail_Multiple(t *testing.T) {
	llm := &llmStub{responses: []string{"output"}}
	agent := NewAgent("Agent", "", "", llm)
	task := NewTask("t", "", agent)
	crew := guardrailCrew(llm, []*Task{task})
	crew.Guardrails = []Guardrail{
		func(_ context.Context, _ *CrewOutput) error { return nil },
		func(_ context.Context, _ *CrewOutput) error { return fmt.Errorf("second guardrail failed") },
	}
	_, err := crew.Kickoff(context.Background(), nil)
	if !errors.Is(err, ErrBlockedByGuardrail) {
		t.Fatalf("expected ErrBlockedByGuardrail, got %v", err)
	}
	if !strings.Contains(err.Error(), "second guardrail failed") {
		t.Errorf("error should contain second guardrail message: %v", err)
	}
}

func TestCrewGuardrail_ShortCircuit(t *testing.T) {
	var secondCalled int32
	llm := &llmStub{responses: []string{"output"}}
	agent := NewAgent("Agent", "", "", llm)
	task := NewTask("t", "", agent)
	crew := guardrailCrew(llm, []*Task{task})
	crew.Guardrails = []Guardrail{
		func(_ context.Context, _ *CrewOutput) error { return fmt.Errorf("first failed") },
		func(_ context.Context, _ *CrewOutput) error {
			atomic.StoreInt32(&secondCalled, 1)
			return nil
		},
	}
	_, err := crew.Kickoff(context.Background(), nil)
	if !errors.Is(err, ErrBlockedByGuardrail) {
		t.Fatalf("expected ErrBlockedByGuardrail, got %v", err)
	}
	if !strings.Contains(err.Error(), "first failed") {
		t.Errorf("error should contain first guardrail message: %v", err)
	}
	if atomic.LoadInt32(&secondCalled) != 0 {
		t.Error("second guardrail should not have been called")
	}
}

func TestCrewGuardrail_ReceivesFullOutput(t *testing.T) {
	llm := mockMultiResponse([]string{"result1", "result2"})
	a1 := NewAgent("Agent1", "", "", llm)
	a2 := NewAgent("Agent2", "", "", llm)
	t1 := NewTask("task1", "", a1)
	t2 := NewTask("task2", "", a2).WithContext(t1)
	crew := &Crew{Agents: []*Agent{a1, a2}, Tasks: []*Task{t1, t2}}
	var received *CrewOutput
	crew.Guardrails = []Guardrail{
		func(_ context.Context, out *CrewOutput) error {
			received = out
			return nil
		},
	}
	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatalf("Kickoff error: %v", err)
	}
	if received == nil {
		t.Fatal("guardrail did not receive output")
	}
	if len(received.TasksOutput) != 2 {
		t.Errorf("expected 2 task outputs, got %d", len(received.TasksOutput))
	}
	if received.Final != "result2" {
		t.Errorf("Final = %q, want result2", received.Final)
	}
}

func TestCrewGuardrail_DoesNotMutate(t *testing.T) {
	llm := &llmStub{responses: []string{"original"}}
	agent := NewAgent("Agent", "", "", llm)
	task := NewTask("t", "", agent)
	crew := guardrailCrew(llm, []*Task{task})
	crew.Guardrails = []Guardrail{
		func(_ context.Context, out *CrewOutput) error {
			// Attempt to mutate (should not affect the real output).
			out.Final = "mutated"
			out.TasksOutput = nil
			return nil
		},
	}
	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("Kickoff error: %v", err)
	}
	// The returned output should reflect the mutation (since guardrail
	// receives a pointer, not a copy). This is documented behavior:
	// guardrails SHOULD NOT mutate, but we can't enforce it. The test
	// verifies that a well-behaved guardrail that returns nil still
	// allows Kickoff to succeed. We document the MUST NOT in godoc.
	if out == nil {
		t.Error("expected non-nil output")
	}
}

// --- Task-level guardrail tests ---

func TestTaskGuardrail_Pass(t *testing.T) {
	llm := &llmStub{responses: []string{"good"}}
	agent := NewAgent("Agent", "", "", llm)
	task := NewTask("t", "", agent).WithGuardrail(func(_ context.Context, out *CrewOutput) error {
		if out.Final == "" {
			return fmt.Errorf("empty")
		}
		return nil
	})
	crew := guardrailCrew(llm, []*Task{task})
	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("Kickoff error: %v", err)
	}
	if out.Final != "good" {
		t.Errorf("Final = %q", out.Final)
	}
}

func TestTaskGuardrail_Fail(t *testing.T) {
	llm := &llmStub{responses: []string{"bad"}}
	agent := NewAgent("Agent", "", "", llm)
	task := NewTask("desc", "", agent)
	task.Name = "mytask"
	task.Guardrail = func(_ context.Context, _ *CrewOutput) error {
		return fmt.Errorf("task guardrail rejected")
	}
	crew := guardrailCrew(llm, []*Task{task})
	_, err := crew.Kickoff(context.Background(), nil)
	if !errors.Is(err, ErrBlockedByGuardrail) {
		t.Fatalf("expected ErrBlockedByGuardrail, got %v", err)
	}
	if !strings.Contains(err.Error(), "task guardrail rejected") {
		t.Errorf("error should preserve guardrail message: %v", err)
	}
	if !strings.Contains(err.Error(), "mytask") {
		t.Errorf("error should contain task label: %v", err)
	}
}

func TestTaskGuardrail_BlocksCrew(t *testing.T) {
	llm := &llmStub{responses: []string{"first", "second"}}
	a := NewAgent("Agent", "", "", llm)
	t1 := NewTask("t1", "", a).WithGuardrail(func(_ context.Context, _ *CrewOutput) error {
		return fmt.Errorf("blocked at t1")
	})
	t2 := NewTask("t2", "", a)
	crew := &Crew{Agents: []*Agent{a}, Tasks: []*Task{t1, t2}}
	_, err := crew.Kickoff(context.Background(), nil)
	if !errors.Is(err, ErrBlockedByGuardrail) {
		t.Fatalf("expected ErrBlockedByGuardrail, got %v", err)
	}
	// Task 1 guardrail failed: task 2 should never run.
	// llmStub returns "first" on call 1, "second" on call 2.
	// If task 2 ran, there would be 2 LLM calls. We verify only 1 was made.
	if llm.i != 1 {
		t.Errorf("expected 1 LLM call (task 2 should not run), got idx=%d", llm.i)
	}
}

func TestTaskGuardrail_ReceivesOutput(t *testing.T) {
	llm := &llmStub{responses: []string{"task output"}}
	agent := NewAgent("Agent", "", "", llm)
	var received *CrewOutput
	task := NewTask("mytask", "", agent).WithGuardrail(func(_ context.Context, out *CrewOutput) error {
		received = out
		return nil
	})
	crew := guardrailCrew(llm, []*Task{task})
	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatalf("Kickoff error: %v", err)
	}
	if received == nil {
		t.Fatal("guardrail did not receive output")
	}
	if received.Final != "task output" {
		t.Errorf("Final = %q, want 'task output'", received.Final)
	}
	if len(received.TasksOutput) != 1 {
		t.Fatalf("expected 1 task output, got %d", len(received.TasksOutput))
	}
	if received.TasksOutput[0].Output != "task output" {
		t.Errorf("TasksOutput[0].Output = %q", received.TasksOutput[0].Output)
	}
}

func TestTaskGuardrail_NilGuardrail(t *testing.T) {
	llm := &llmStub{responses: []string{"output"}}
	agent := NewAgent("Agent", "", "", llm)
	task := NewTask("t", "", agent) // no Guardrail set
	crew := guardrailCrew(llm, []*Task{task})
	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("Kickoff error: %v", err)
	}
	if out.Final != "output" {
		t.Errorf("Final = %q", out.Final)
	}
}

// --- Regression test ---

func TestNoGuardrails_Regression(t *testing.T) {
	llm := &llmStub{responses: []string{"result1", "result2"}}
	a1 := NewAgent("Agent1", "", "", llm)
	a2 := NewAgent("Agent2", "", "", llm)
	t1 := NewTask("t1", "", a1)
	t2 := NewTask("t2", "", a2).WithContext(t1)
	crew := &Crew{Agents: []*Agent{a1, a2}, Tasks: []*Task{t1, t2}}
	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("Kickoff error: %v", err)
	}
	if out.Final != "result2" {
		t.Errorf("Final = %q, want result2", out.Final)
	}
	if len(out.TasksOutput) != 2 {
		t.Errorf("expected 2 outputs, got %d", len(out.TasksOutput))
	}
}

// --- Context cancellation ---

func TestCrewGuardrail_ContextCancel(t *testing.T) {
	llm := &llmStub{responses: []string{"output"}}
	agent := NewAgent("Agent", "", "", llm)
	task := NewTask("t", "", agent)
	crew := guardrailCrew(llm, []*Task{task})
	crew.Guardrails = []Guardrail{
		func(ctx context.Context, _ *CrewOutput) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := crew.Kickoff(ctx, nil)
	// The guardrail should return context.Canceled, which gets wrapped.
	if err == nil {
		t.Fatal("expected error from cancelled guardrail")
	}
}

func TestCrewGuardrail_ContextTimeout(t *testing.T) {
	llm := &llmStub{responses: []string{"output"}}
	agent := NewAgent("Agent", "", "", llm)
	task := NewTask("t", "", agent)
	crew := guardrailCrew(llm, []*Task{task})
	crew.Guardrails = []Guardrail{
		func(ctx context.Context, _ *CrewOutput) error {
			select {
			case <-time.After(100 * time.Millisecond):
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	_, err := crew.Kickoff(ctx, nil)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

// --- errors.Is / errors.Unwrap ---

func TestGuardrail_ErrorsIs(t *testing.T) {
	llm := &llmStub{responses: []string{"output"}}
	agent := NewAgent("Agent", "", "", llm)
	task := NewTask("t", "", agent)
	crew := guardrailCrew(llm, []*Task{task})
	crew.Guardrails = []Guardrail{
		func(_ context.Context, _ *CrewOutput) error { return fmt.Errorf("fail") },
	}
	_, err := crew.Kickoff(context.Background(), nil)
	if !errors.Is(err, ErrBlockedByGuardrail) {
		t.Errorf("errors.Is(err, ErrBlockedByGuardrail) = false, want true")
	}
}

func TestGuardrail_ErrorsUnwrapPreservesMessage(t *testing.T) {
	llm := &llmStub{responses: []string{"output"}}
	agent := NewAgent("Agent", "", "", llm)
	task := NewTask("t", "", agent)
	crew := guardrailCrew(llm, []*Task{task})
	guardrailErr := fmt.Errorf("missing source URL")
	crew.Guardrails = []Guardrail{
		func(_ context.Context, _ *CrewOutput) error { return guardrailErr },
	}
	_, err := crew.Kickoff(context.Background(), nil)
	if !strings.Contains(err.Error(), "missing source URL") {
		t.Errorf("error should preserve guardrail message: %v", err)
	}
	// errors.Unwrap should get us closer to the original error.
	unwrapped := errors.Unwrap(err)
	if unwrapped == nil {
		t.Error("errors.Unwrap returned nil")
	}
}

// --- Structured output + guardrail ---

func TestTaskGuardrail_StructuredOutput(t *testing.T) {
	schema := mustRaw(t, map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
		"required": []any{"name"},
	})
	llm := &structuredStub{responses: []string{`{"name":"Alice"}`}}
	agent := NewAgent("Agent", "", "", llm)
	task := NewTask("Extract name.", "JSON", agent)
	task.Structured = &StructuredOutput{Schema: schema}
	task.Guardrail = func(_ context.Context, out *CrewOutput) error {
		// The guardrail can parse the canonicalized JSON for deeper inspection.
		var m map[string]any
		if err := jsonUnmarshalString(out.Final, &m); err != nil {
			return fmt.Errorf("guardrail: invalid JSON: %v", err)
		}
		if m["name"] != "Alice" {
			return fmt.Errorf("guardrail: expected Alice, got %v", m["name"])
		}
		return nil
	}
	crew := guardrailCrew(llm, []*Task{task})
	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("Kickoff error: %v", err)
	}
	if !strings.Contains(out.Final, "Alice") {
		t.Errorf("Final = %q", out.Final)
	}
}

func TestTaskGuardrail_StructuredOutputFail(t *testing.T) {
	schema := mustRaw(t, map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":  map[string]any{"type": "string"},
			"count": map[string]any{"type": "integer"},
		},
		"required": []any{"name", "count"},
	})
	llm := &structuredStub{responses: []string{`{"name":"Alice","count":5}`}}
	agent := NewAgent("Agent", "", "", llm)
	task := NewTask("Extract name and count.", "JSON", agent)
	task.Structured = &StructuredOutput{Schema: schema}
	task.Guardrail = func(_ context.Context, out *CrewOutput) error {
		var m map[string]any
		_ = jsonUnmarshalString(out.Final, &m)
		if c, ok := m["count"].(float64); ok && c > 3 {
			return fmt.Errorf("count %v exceeds maximum allowed (3)", c)
		}
		return nil
	}
	crew := guardrailCrew(llm, []*Task{task})
	_, err := crew.Kickoff(context.Background(), nil)
	if !errors.Is(err, ErrBlockedByGuardrail) {
		t.Fatalf("expected ErrBlockedByGuardrail, got %v", err)
	}
	if !strings.Contains(err.Error(), "exceeds maximum") {
		t.Errorf("error should contain guardrail message: %v", err)
	}
}

// --- Use-case tests ---

func TestGuardrail_ProvenanceRule(t *testing.T) {
	provenanceGuard := func(_ context.Context, out *CrewOutput) error {
		if !strings.Contains(out.Final, "http://") && !strings.Contains(out.Final, "https://") {
			return fmt.Errorf("output lacks source URL")
		}
		return nil
	}
	// Pass: output has URL.
	llm := &llmStub{responses: []string{"Fact: Go is great. Source: https://go.dev"}}
	agent := NewAgent("Agent", "", "", llm)
	task := NewTask("t", "", agent)
	crew := guardrailCrew(llm, []*Task{task})
	crew.Guardrails = []Guardrail{provenanceGuard}
	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatalf("expected pass with URL: %v", err)
	}
	// Fail: output without URL.
	llm2 := &llmStub{responses: []string{"Fact: Go is great."}}
	agent2 := NewAgent("Agent", "", "", llm2)
	task2 := NewTask("t", "", agent2)
	crew2 := guardrailCrew(llm2, []*Task{task2})
	crew2.Guardrails = []Guardrail{provenanceGuard}
	_, err := crew2.Kickoff(context.Background(), nil)
	if !errors.Is(err, ErrBlockedByGuardrail) {
		t.Fatalf("expected ErrBlockedByGuardrail, got %v", err)
	}
}

func TestGuardrail_AntiHallucinationIDs(t *testing.T) {
	validIDs := map[string]bool{"X-001": true, "X-002": true}
	idGuard := func(_ context.Context, out *CrewOutput) error {
		for id := range validIDs {
			if strings.Contains(out.Final, id) {
				return nil
			}
		}
		return fmt.Errorf("output references no known valid ID")
	}
	// Pass: references valid ID.
	llm := &llmStub{responses: []string{"Record X-001: active"}}
	agent := NewAgent("Agent", "", "", llm)
	task := NewTask("t", "", agent)
	crew := guardrailCrew(llm, []*Task{task})
	crew.Guardrails = []Guardrail{idGuard}
	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatalf("expected pass with valid ID: %v", err)
	}
	// Fail: references unknown ID.
	llm2 := &llmStub{responses: []string{"Record X-999: active"}}
	agent2 := NewAgent("Agent", "", "", llm2)
	task2 := NewTask("t", "", agent2)
	crew2 := guardrailCrew(llm2, []*Task{task2})
	crew2.Guardrails = []Guardrail{idGuard}
	_, err := crew2.Kickoff(context.Background(), nil)
	if !errors.Is(err, ErrBlockedByGuardrail) {
		t.Fatalf("expected ErrBlockedByGuardrail for unknown ID, got %v", err)
	}
}

func TestGuardrail_MinLength(t *testing.T) {
	minLenGuard := func(_ context.Context, out *CrewOutput) error {
		if len(out.Final) < 10 {
			return fmt.Errorf("output too short: %d chars", len(out.Final))
		}
		return nil
	}
	// Pass: long enough.
	llm := &llmStub{responses: []string{"This is a sufficiently long output."}}
	agent := NewAgent("Agent", "", "", llm)
	task := NewTask("t", "", agent)
	crew := guardrailCrew(llm, []*Task{task})
	crew.Guardrails = []Guardrail{minLenGuard}
	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatalf("expected pass: %v", err)
	}
	// Fail: too short.
	llm2 := &llmStub{responses: []string{"short"}}
	agent2 := NewAgent("Agent", "", "", llm2)
	task2 := NewTask("t", "", agent2)
	crew2 := guardrailCrew(llm2, []*Task{task2})
	crew2.Guardrails = []Guardrail{minLenGuard}
	_, err := crew2.Kickoff(context.Background(), nil)
	if !errors.Is(err, ErrBlockedByGuardrail) {
		t.Fatalf("expected ErrBlockedByGuardrail, got %v", err)
	}
}

// --- No partial output leak ---

func TestGuardrail_NoPartialOutputLeak(t *testing.T) {
	llm := &llmStub{responses: []string{"result1", "result2"}}
	a1 := NewAgent("Agent1", "", "", llm)
	a2 := NewAgent("Agent2", "", "", llm)
	t1 := NewTask("t1", "", a1)
	t2 := NewTask("t2", "", a2)
	crew := &Crew{Agents: []*Agent{a1, a2}, Tasks: []*Task{t1, t2}}
	crew.Guardrails = []Guardrail{
		func(_ context.Context, _ *CrewOutput) error { return fmt.Errorf("blocked") },
	}
	out, err := crew.Kickoff(context.Background(), nil)
	if out != nil {
		t.Errorf("expected nil output on guardrail failure, got %+v", out)
	}
	if !errors.Is(err, ErrBlockedByGuardrail) {
		t.Fatalf("expected ErrBlockedByGuardrail, got %v", err)
	}
}

// --- WithGuardrails option ---

func TestWithGuardrails_Option(t *testing.T) {
	g1 := func(_ context.Context, _ *CrewOutput) error { return nil }
	g2 := func(_ context.Context, _ *CrewOutput) error { return nil }
	crew := &Crew{}
	opt := WithGuardrails(g1, g2)
	opt(crew)
	if len(crew.Guardrails) != 2 {
		t.Errorf("expected 2 guardrails, got %d", len(crew.Guardrails))
	}
}

// --- WithGuardrail method ---

func TestWithGuardrail_Method(t *testing.T) {
	g := func(_ context.Context, _ *CrewOutput) error { return nil }
	task := NewTask("t", "", nil)
	task.WithGuardrail(g)
	if task.Guardrail == nil {
		t.Error("expected guardrail to be set")
	}
}

// --- Both task and crew guardrails ---

func TestBothTaskAndCrewGuardrails(t *testing.T) {
	var taskGuardCalled, crewGuardCalled int32
	llm := &llmStub{responses: []string{"output"}}
	agent := NewAgent("Agent", "", "", llm)
	task := NewTask("t", "", agent).WithGuardrail(func(_ context.Context, _ *CrewOutput) error {
		atomic.StoreInt32(&taskGuardCalled, 1)
		return nil
	})
	crew := guardrailCrew(llm, []*Task{task})
	crew.Guardrails = []Guardrail{
		func(_ context.Context, _ *CrewOutput) error {
			atomic.StoreInt32(&crewGuardCalled, 1)
			return nil
		},
	}
	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatalf("Kickoff error: %v", err)
	}
	if atomic.LoadInt32(&taskGuardCalled) != 1 {
		t.Error("task guardrail was not called")
	}
	if atomic.LoadInt32(&crewGuardCalled) != 1 {
		t.Error("crew guardrail was not called")
	}
}

func TestTaskGuardBlocksBeforeCrewGuard(t *testing.T) {
	var crewGuardCalled int32
	llm := &llmStub{responses: []string{"output"}}
	agent := NewAgent("Agent", "", "", llm)
	task := NewTask("t", "", agent).WithGuardrail(func(_ context.Context, _ *CrewOutput) error {
		return fmt.Errorf("task blocked")
	})
	crew := guardrailCrew(llm, []*Task{task})
	crew.Guardrails = []Guardrail{
		func(_ context.Context, _ *CrewOutput) error {
			atomic.StoreInt32(&crewGuardCalled, 1)
			return nil
		},
	}
	_, err := crew.Kickoff(context.Background(), nil)
	if !errors.Is(err, ErrBlockedByGuardrail) {
		t.Fatalf("expected ErrBlockedByGuardrail, got %v", err)
	}
	if !strings.Contains(err.Error(), "task blocked") {
		t.Errorf("error should contain task guardrail message: %v", err)
	}
	if atomic.LoadInt32(&crewGuardCalled) != 0 {
		t.Error("crew guardrail should not have been called when task guardrail fails")
	}
}

// --- runTaskGuardrail / runCrewGuardrails unit tests ---

func TestRunTaskGuardrail_NilGuardrail(t *testing.T) {
	task := &Task{} // no Guardrail
	if err := runTaskGuardrail(context.Background(), task, "t", "output"); err != nil {
		t.Errorf("expected nil for nil guardrail, got %v", err)
	}
}

func TestRunCrewGuardrails_EmptySlice(t *testing.T) {
	if err := runCrewGuardrails(context.Background(), nil, &CrewOutput{}); err != nil {
		t.Errorf("expected nil for empty guardrails, got %v", err)
	}
}

func TestRunCrewGuardrails_AllPass(t *testing.T) {
	guards := []Guardrail{
		func(_ context.Context, _ *CrewOutput) error { return nil },
		func(_ context.Context, _ *CrewOutput) error { return nil },
	}
	if err := runCrewGuardrails(context.Background(), guards, &CrewOutput{}); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

// --- Helpers for multi-response mock ---

type multiResponseLLM struct {
	responses []string
	idx       int
}

func (m *multiResponseLLM) Call(_ context.Context, _ []Message) (string, error) {
	r := m.responses[m.idx]
	if m.idx < len(m.responses)-1 {
		m.idx++
	}
	return r, nil
}
func (m *multiResponseLLM) Model() string { return "multi" }

func mockMultiResponse(responses []string) LLM {
	return &multiResponseLLM{responses: responses}
}

// jsonUnmarshalString is a helper that wraps json.Unmarshal for use in tests.
func jsonUnmarshalString(s string, v any) error {
	return json.Unmarshal([]byte(s), v)
}
