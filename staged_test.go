package crewai_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rhgs/crewai-go"
	"github.com/rhgs/crewai-go/llm/mock"
)

func TestStagedParallelism(t *testing.T) {
	// Two tasks that each block for 150ms. If they run in parallel the total
	// time is ~150ms; if serial, ~300ms.
	const per = 150 * time.Millisecond
	llm := &mock.LLM{Handler: func(_ context.Context, _ []crewai.Message) (string, error) {
		time.Sleep(per)
		return "ok", nil
	}}
	a := crewai.NewAgent("A", "", "", llm)
	t1 := crewai.NewTask("t1", "", a)
	t2 := crewai.NewTask("t2", "", a)

	crew := crewai.NewCrew([]*crewai.Agent{a}, nil)
	crew.Process = crewai.Staged
	crew.Stages = []crewai.Stage{{Name: "s1", Tasks: []*crewai.Task{t1, t2}}}

	start := time.Now()
	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatalf("error: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed >= 2*per {
		t.Errorf("tasks ran serially: elapsed %v, want < %v", elapsed, 2*per)
	}
}

func TestStagedDeterministicOrder(t *testing.T) {
	// t1 is slow, t2 is fast; output must still be in declaration order.
	llm := &mock.LLM{Handler: func(_ context.Context, msgs []crewai.Message) (string, error) {
		for _, m := range msgs {
			if strings.Contains(m.Content, "SLOW") {
				time.Sleep(100 * time.Millisecond)
				return "slow", nil
			}
			if strings.Contains(m.Content, "FAST") {
				return "fast", nil
			}
		}
		return "?", nil
	}}
	a := crewai.NewAgent("A", "", "", llm)
	t1 := crewai.NewTask("SLOW", "", a)
	t2 := crewai.NewTask("FAST", "", a)

	crew := crewai.NewCrew([]*crewai.Agent{a}, nil)
	crew.Process = crewai.Staged
	crew.Stages = []crewai.Stage{
		{Name: "s1", Tasks: []*crewai.Task{t1, t2}},
	}

	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(out.TasksOutput) != 2 {
		t.Fatalf("expected 2 outputs, got %d", len(out.TasksOutput))
	}
	if out.TasksOutput[0].Output != "slow" {
		t.Errorf("task 0 output = %q, want %q", out.TasksOutput[0].Output, "slow")
	}
	if out.TasksOutput[1].Output != "fast" {
		t.Errorf("task 1 output = %q, want %q", out.TasksOutput[1].Output, "fast")
	}
}

func TestStagedOptionalFailure(t *testing.T) {
	llm := &mock.LLM{Handler: func(_ context.Context, msgs []crewai.Message) (string, error) {
		for _, m := range msgs {
			if strings.Contains(m.Content, "FAIL") {
				return "", errors.New("boom")
			}
		}
		return "ok", nil
	}}
	a := crewai.NewAgent("A", "", "", llm)
	t1 := crewai.NewTask("FAIL", "", a)
	t2 := crewai.NewTask("ok", "", a)

	crew := crewai.NewCrew([]*crewai.Agent{a}, nil)
	crew.Process = crewai.Staged
	crew.Stages = []crewai.Stage{
		{Name: "s1", Tasks: []*crewai.Task{t1, t2}, Optional: true},
	}

	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("optional stage should not abort: %v", err)
	}
	if len(out.TasksOutput) != 1 {
		t.Fatalf("expected 1 output (failed task skipped), got %d", len(out.TasksOutput))
	}
	if out.TasksOutput[0].Output != "ok" {
		t.Errorf("output = %q, want %q", out.TasksOutput[0].Output, "ok")
	}
}

func TestStagedRequiredFailure(t *testing.T) {
	llm := &mock.LLM{Handler: func(_ context.Context, msgs []crewai.Message) (string, error) {
		for _, m := range msgs {
			if strings.Contains(m.Content, "FAIL") {
				return "", errors.New("boom")
			}
		}
		return "ok", nil
	}}
	a := crewai.NewAgent("A", "", "", llm)
	t1 := crewai.NewTask("ok", "", a)
	t2 := crewai.NewTask("FAIL", "", a)

	crew := crewai.NewCrew([]*crewai.Agent{a}, nil)
	crew.Process = crewai.Staged
	crew.Stages = []crewai.Stage{
		{Name: "coleta", Tasks: []*crewai.Task{t1, t2}},
	}

	_, err := crew.Kickoff(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error from non-optional stage")
	}
	if !strings.Contains(err.Error(), `stage "coleta"`) {
		t.Errorf("error should mention stage name: %v", err)
	}
	if !strings.Contains(err.Error(), "task 2") {
		t.Errorf("error should mention task index: %v", err)
	}
}

func TestStagedCancellation(t *testing.T) {
	started := make(chan struct{}, 2)
	llm := &mock.LLM{Handler: func(ctx context.Context, _ []crewai.Message) (string, error) {
		started <- struct{}{}
		<-ctx.Done()
		return "", ctx.Err()
	}}
	a := crewai.NewAgent("A", "", "", llm)
	t1 := crewai.NewTask("t1", "", a)
	t2 := crewai.NewTask("t2", "", a)

	crew := crewai.NewCrew([]*crewai.Agent{a}, nil)
	crew.Process = crewai.Staged
	crew.Stages = []crewai.Stage{{Name: "s1", Tasks: []*crewai.Task{t1, t2}}}

	ctx, cancel := context.WithCancel(context.Background())
	// Wait for both goroutines to start, then cancel.
	go func() {
		<-started
		<-started
		cancel()
	}()

	_, err := crew.Kickoff(ctx, nil)
	if err == nil {
		t.Fatal("expected cancellation error")
	}
}

func TestStagedContextBetweenStages(t *testing.T) {
	var secondPrompt string
	llm := &mock.LLM{Handler: func(_ context.Context, msgs []crewai.Message) (string, error) {
		for _, m := range msgs {
			if strings.Contains(m.Content, "stage2") {
				secondPrompt += m.Content
				return "OUTPUT_TWO", nil
			}
		}
		return "OUTPUT_ONE", nil
	}}
	a := crewai.NewAgent("A", "", "", llm)
	t1 := crewai.NewTask("stage1", "", a)
	t2 := crewai.NewTask("stage2", "", a).WithContext(t1)

	crew := crewai.NewCrew([]*crewai.Agent{a}, nil)
	crew.Process = crewai.Staged
	crew.Stages = []crewai.Stage{
		{Name: "s1", Tasks: []*crewai.Task{t1}},
		{Name: "s2", Tasks: []*crewai.Task{t2}},
	}

	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatalf("error: %v", err)
	}
	if !strings.Contains(secondPrompt, "OUTPUT_ONE") {
		t.Errorf("stage 2 did not receive stage 1 output; prompt = %q", secondPrompt)
	}
}

func TestStagedFactsAggregated(t *testing.T) {
	// Two tasks in different stages each produce the same fact via a
	// FactSource tool; the crew output must deduplicate it.
	fact := crewai.NewFact("claim", "org", "https://example.com", []byte("payload"))

	newTool := func(name string) crewai.Tool {
		return crewai.NewFactSourceTool(name, "", func(_ context.Context, _ string) (string, error) {
			return "x", nil
		}, func(_ context.Context, _ string) []crewai.Fact { return []crewai.Fact{fact} })
	}

	// Each task's LLM invokes its tool once, then answers.
	newLLM := func(toolName string) *mock.LLM {
		calls := 0
		return &mock.LLM{Handler: func(_ context.Context, _ []crewai.Message) (string, error) {
			calls++
			if calls == 1 {
				return "Action: " + toolName + "\nAction Input: x\n", nil
			}
			return "Final Answer: done", nil
		}}
	}

	a1 := crewai.NewAgent("A1", "", "", newLLM("f1")).WithTools(newTool("f1"))
	a2 := crewai.NewAgent("A2", "", "", newLLM("f2")).WithTools(newTool("f2"))
	t1 := crewai.NewTask("t1", "", a1)
	t2 := crewai.NewTask("t2", "", a2)

	crew := crewai.NewCrew([]*crewai.Agent{a1, a2}, nil)
	crew.Process = crewai.Staged
	crew.Stages = []crewai.Stage{
		{Name: "s1", Tasks: []*crewai.Task{t1}},
		{Name: "s2", Tasks: []*crewai.Task{t2}},
	}

	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(out.Facts) != 1 {
		t.Fatalf("expected 1 deduplicated fact, got %d", len(out.Facts))
	}
}

func TestStagedPanicRecovered(t *testing.T) {
	llm := &mock.LLM{Handler: func(_ context.Context, _ []crewai.Message) (string, error) {
		panic("boom")
	}}
	a := crewai.NewAgent("A", "", "", llm)
	t1 := crewai.NewTask("t1", "", a)

	crew := crewai.NewCrew([]*crewai.Agent{a}, nil)
	crew.Process = crewai.Staged
	crew.Stages = []crewai.Stage{{Name: "s1", Tasks: []*crewai.Task{t1}}}

	_, err := crew.Kickoff(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error from panic")
	}
	if !strings.Contains(err.Error(), "panic") {
		t.Errorf("error should mention panic: %v", err)
	}
}

func TestStagedNoStages(t *testing.T) {
	crew := crewai.NewCrew(nil, nil)
	crew.Process = crewai.Staged
	if _, err := crew.Kickoff(context.Background(), nil); err != crewai.ErrNoStages {
		t.Errorf("error = %v, want %v", err, crewai.ErrNoStages)
	}
}

func TestStagedEmptyTasksButStages(t *testing.T) {
	llm := mock.New("ok")
	a := crewai.NewAgent("A", "", "", llm)
	t1 := crewai.NewTask("t1", "", a)

	crew := crewai.NewCrew([]*crewai.Agent{a}, nil) // Tasks empty
	crew.Process = crewai.Staged
	crew.Stages = []crewai.Stage{{Name: "s1", Tasks: []*crewai.Task{t1}}}

	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("staged with empty Tasks should not return ErrNoTasks: %v", err)
	}
	if out.Final != "ok" {
		t.Errorf("Final = %q, want %q", out.Final, "ok")
	}
}

func TestStagedInterpolation(t *testing.T) {
	var prompt string
	llm := &mock.LLM{Handler: func(_ context.Context, msgs []crewai.Message) (string, error) {
		for _, m := range msgs {
			prompt += m.Content
		}
		return "ok", nil
	}}
	a := crewai.NewAgent("A", "", "", llm)
	t1 := crewai.NewTask("Analyze {company}", "", a)

	crew := crewai.NewCrew([]*crewai.Agent{a}, nil)
	crew.Process = crewai.Staged
	crew.Stages = []crewai.Stage{{Name: "s1", Tasks: []*crewai.Task{t1}}}

	if _, err := crew.Kickoff(context.Background(), map[string]string{"company": "Acme"}); err != nil {
		t.Fatalf("error: %v", err)
	}
	if !strings.Contains(prompt, "Acme") {
		t.Errorf("interpolation failed; prompt = %q", prompt)
	}
}

func TestStagedFinalIsLastTask(t *testing.T) {
	llm := mock.New("first", "second")
	a := crewai.NewAgent("A", "", "", llm)
	t1 := crewai.NewTask("t1", "", a)
	t2 := crewai.NewTask("t2", "", a)

	crew := crewai.NewCrew([]*crewai.Agent{a}, nil)
	crew.Process = crewai.Staged
	crew.Stages = []crewai.Stage{
		{Name: "s1", Tasks: []*crewai.Task{t1}},
		{Name: "s2", Tasks: []*crewai.Task{t2}},
	}

	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if out.Final != "second" {
		t.Errorf("Final = %q, want %q", out.Final, "second")
	}
}
