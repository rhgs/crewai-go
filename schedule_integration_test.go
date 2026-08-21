package crewai_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rhgs/crewai-go"
	"github.com/rhgs/crewai-go/llm/mock"
)

// TestAsyncSequentialOverlap is the integration proof that two independent
// Async tasks run with overlapping time (rendezvous via a channel, not a
// wall-clock sleep).
func TestAsyncSequentialOverlap(t *testing.T) {
	gate := make(chan struct{})
	var calls atomic.Int32
	llm := &mock.LLM{Handler: func(ctx context.Context, _ []crewai.Message) (string, error) {
		n := calls.Add(1)
		if n == 2 {
			close(gate) // both workers are inside; release them together
			return "ok", nil
		}
		select {
		case <-gate:
			return "ok", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}}
	a := crewai.NewAgent("A", "", "", llm)
	t1 := crewai.NewTask("t1", "", a).WithAsync()
	t2 := crewai.NewTask("t2", "", a).WithAsync()

	crew := crewai.NewCrew([]*crewai.Agent{a}, []*crewai.Task{t1, t2})
	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if calls.Load() != 2 {
		t.Errorf("calls = %d, want 2 concurrent", calls.Load())
	}
	if len(out.TasksOutput) != 2 {
		t.Errorf("outputs = %d, want 2", len(out.TasksOutput))
	}
}

func TestAsyncDependencyWaitsForUpstream(t *testing.T) {
	var secondPrompt string
	var startedSecond atomic.Bool
	llm := &mock.LLM{Handler: func(_ context.Context, msgs []crewai.Message) (string, error) {
		var all string
		for _, m := range msgs {
			all += m.Content
		}
		if strings.Contains(all, "SLOW") {
			time.Sleep(50 * time.Millisecond)
			return "OUTPUT_ONE", nil
		}
		// The task asking for the slow task output.
		startedSecond.Store(true)
		secondPrompt = all
		return "OUTPUT_TWO", nil
	}}
	a := crewai.NewAgent("A", "", "", llm)
	t1 := crewai.NewTask("SLOW task", "", a)
	t2 := crewai.NewTask("use t1", "", a).WithContext(t1).WithAsync()

	crew := crewai.NewCrew([]*crewai.Agent{a}, []*crewai.Task{t1, t2})
	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatalf("error: %v", err)
	}
	if !startedSecond.Load() {
		t.Fatal("dependent async task never ran")
	}
	if !strings.Contains(secondPrompt, "OUTPUT_ONE") {
		t.Errorf("dependent started without seeing upstream output; prompt = %q", secondPrompt)
	}
}

func TestAsyncFailFastCancel(t *testing.T) {
	release := make(chan struct{})
	var started atomic.Int32
	llm := &mock.LLM{Handler: func(ctx context.Context, msgs []crewai.Message) (string, error) {
		started.Add(1)
		var all string
		for _, m := range msgs {
			all += m.Content
		}
		if strings.Contains(all, "FAIL") {
			close(release)
			return "", errors.New("boom")
		}
		// sibling blocks until canceled by fail-fast
		select {
		case <-release:
			<-ctx.Done()
			return "", ctx.Err()
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}}
	a := crewai.NewAgent("A", "", "", llm)
	t1 := crewai.NewTask("FAIL", "", a).WithAsync()
	t2 := crewai.NewTask("slow sibling", "", a).WithAsync()

	crew := crewai.NewCrew([]*crewai.Agent{a}, []*crewai.Task{t1, t2})
	crew.AsyncFailFast = true
	_, err := crew.Kickoff(context.Background(), nil)
	if err == nil {
		t.Fatal("expected fail-fast error")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("error should surface the real cause, got %v", err)
	}
}

func TestAsyncFailFastFalseKeepsGoing(t *testing.T) {
	llm := &mock.LLM{Handler: func(_ context.Context, msgs []crewai.Message) (string, error) {
		var all string
		for _, m := range msgs {
			all += m.Content
		}
		if strings.Contains(all, "FAIL") {
			return "", errors.New("boom")
		}
		return "ok", nil
	}}
	a := crewai.NewAgent("A", "", "", llm)
	t1 := crewai.NewTask("FAIL", "", a).WithAsync()
	t2 := crewai.NewTask("independent", "", a).WithAsync()

	crew := crewai.NewCrew([]*crewai.Agent{a}, []*crewai.Task{t1, t2})
	crew.AsyncFailFast = false
	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("FailFast=false should not abort Kickoff: %v", err)
	}
	if out.Final != "ok" {
		t.Errorf("Final = %q, want %q (surviving task output)", out.Final, "ok")
	}
}

func TestAsyncPanicRecoveredKickoffErrors(t *testing.T) {
	llm := &mock.LLM{Handler: func(_ context.Context, _ []crewai.Message) (string, error) {
		panic("boom")
	}}
	a := crewai.NewAgent("A", "", "", llm)
	t1 := crewai.NewTask("panic task", "", a).WithAsync()

	crew := crewai.NewCrew([]*crewai.Agent{a}, []*crewai.Task{t1})
	_, err := crew.Kickoff(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "panic") {
		t.Errorf("expected panic error, got %v", err)
	}
}

func TestAsyncSequentialAggregationDeclarationOrder(t *testing.T) {
	llm := &mock.LLM{Handler: func(_ context.Context, msgs []crewai.Message) (string, error) {
		for _, m := range msgs {
			if strings.Contains(m.Content, "SLOW") {
				time.Sleep(50 * time.Millisecond)
				return "slow", nil
			}
			if strings.Contains(m.Content, "FAST") {
				return "fast", nil
			}
		}
		return "?", nil
	}}
	a := crewai.NewAgent("A", "", "", llm)
	t1 := crewai.NewTask("SLOW", "", a).WithAsync()
	t2 := crewai.NewTask("FAST", "", a).WithAsync()

	crew := crewai.NewCrew([]*crewai.Agent{a}, []*crewai.Task{t1, t2})
	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(out.TasksOutput) != 2 {
		t.Fatalf("want 2 outputs, got %d", len(out.TasksOutput))
	}
	if out.TasksOutput[0].Output != "slow" || out.TasksOutput[1].Output != "fast" {
		t.Errorf("declaration-order fold violated: %q then %q",
			out.TasksOutput[0].Output, out.TasksOutput[1].Output)
	}
	if out.Final != "fast" {
		t.Errorf("Final = %q, want last declared output %q", out.Final, "fast")
	}
}

func TestNewCrewAsyncDefaults(t *testing.T) {
	c := crewai.NewCrew(nil, nil)
	if c.AsyncMaxWorkers != crewai.DefaultAsyncMaxWorkers {
		t.Errorf("AsyncMaxWorkers = %d, want %d", c.AsyncMaxWorkers, crewai.DefaultAsyncMaxWorkers)
	}
	if !c.AsyncFailFast {
		t.Error("AsyncFailFast should default to true")
	}
}

func TestAsyncMaxWorkersZeroMeansUnlimited(t *testing.T) {
	// 12 Async tasks: with the default cap of 8 they cannot all overlap, but
	// explicit 0 must allow every ready task in flight at once.
	const n = 12
	gate := make(chan struct{})
	var inside atomic.Int32
	llm := &mock.LLM{Handler: func(ctx context.Context, _ []crewai.Message) (string, error) {
		if inside.Add(1) == n {
			close(gate)
		}
		select {
		case <-gate:
			return "ok", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}}
	a := crewai.NewAgent("A", "", "", llm)
	tasks := make([]*crewai.Task, n)
	for i := range tasks {
		tasks[i] = crewai.NewTask("t", "", a).WithAsync()
	}
	crew := crewai.NewCrew([]*crewai.Agent{a}, tasks)
	crew.AsyncMaxWorkers = 0 // unlimited
	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatalf("error: %v", err)
	}
	if inside.Load() != n {
		t.Errorf("unlimited wave saw %d concurrent, want %d", inside.Load(), n)
	}
}

func TestAsyncCycleFailsAtKickoffStart(t *testing.T) {
	llm := &mock.LLM{Handler: func(_ context.Context, _ []crewai.Message) (string, error) {
		return "x", nil
	}}
	a := crewai.NewAgent("A", "", "", llm)
	t1 := crewai.NewTask("t1", "", a).WithAsync()
	t2 := crewai.NewTask("t2", "", a).WithAsync()
	t1.WithContext(t2)
	t2.WithContext(t1)

	crew := crewai.NewCrew([]*crewai.Agent{a}, []*crewai.Task{t1, t2})
	_, err := crew.Kickoff(context.Background(), nil)
	if !errors.Is(err, crewai.ErrTaskDependencyCycle) {
		t.Fatalf("cycle must fail fast at Kickoff start, got %v", err)
	}
}

func TestAsyncContextAlwaysEarlierWave(t *testing.T) {
	// G12 structural guarantee: adding a Context edge to a co-ready Async
	// task moves it to a strictly later wave (the two never share a wave).
	// Only cycles/self/duplicates are hard errors.
	a := crewai.NewAgent("A", "", "", mock.New("one", "two"))
	t1 := crewai.NewTask("t1", "", a).WithAsync()
	t2 := crewai.NewTask("t2", "", a).WithAsync().WithContext(t1)

	crew := crewai.NewCrew([]*crewai.Agent{a}, []*crewai.Task{t1, t2})
	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("same-ready dependency must schedule as a later wave, got %v", err)
	}
	if len(out.TasksOutput) != 2 {
		t.Fatalf("outputs = %d, want 2", len(out.TasksOutput))
	}
	if out.TasksOutput[0].Output != "one" || out.TasksOutput[1].Output != "two" {
		t.Errorf("declaration order = [%q %q], want [one two]",
			out.TasksOutput[0].Output, out.TasksOutput[1].Output)
	}
}

func TestAsyncHierarchicalPreResolvesAgentThenWaves(t *testing.T) {
	// D-A2: the manager picks agents serially, then the async waves run.
	worker := &mock.LLM{Handler: func(_ context.Context, _ []crewai.Message) (string, error) {
		return "work done", nil
	}}
	manager := &mock.LLM{Handler: func(_ context.Context, _ []crewai.Message) (string, error) {
		return "Specialist", nil
	}}
	generalist := crewai.NewAgent("Generalist", "general", "", worker)
	specialist := crewai.NewAgent("Specialist", "specific", "", worker)

	t1 := crewai.NewTask("job 1", "", nil).WithAsync()
	t2 := crewai.NewTask("job 2", "", nil).WithAsync()

	crew := crewai.NewCrew([]*crewai.Agent{generalist, specialist}, []*crewai.Task{t1, t2})
	crew.Process = crewai.Hierarchical
	crew.ManagerLLM = manager

	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("hierarchical async: %v", err)
	}
	if len(out.TasksOutput) != 2 {
		t.Fatalf("outputs = %d, want 2", len(out.TasksOutput))
	}
	for _, o := range out.TasksOutput {
		if o.Agent != "Specialist" {
			t.Errorf("task agent = %q, want Specialist (manager pre-resolved)", o.Agent)
		}
	}
}

func TestAsyncWithAsyncMaxWorkersMethodChains(t *testing.T) {
	llm := mock.New("a", "b")
	a := crewai.NewAgent("A", "", "", llm)
	t1 := crewai.NewTask("t1", "", a).WithAsync()
	t2 := crewai.NewTask("t2", "", a).WithAsync()
	crew := crewai.NewCrew([]*crewai.Agent{a}, []*crewai.Task{t1, t2}).
		WithAsyncMaxWorkers(2)
	if crew.AsyncMaxWorkers != 2 {
		t.Fatalf("WithAsyncMaxWorkers(2) set %d", crew.AsyncMaxWorkers)
	}
	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatalf("error: %v", err)
	}
}

func TestStagedIgnoresTaskAsync(t *testing.T) {
	// G5 / D-A5: the flag is ignored; stages still own their own batches.
	llm := mock.New("ok", "ok")
	a := crewai.NewAgent("A", "", "", llm)
	t1 := crewai.NewTask("t1", "", a).WithAsync()
	crew := crewai.NewCrew([]*crewai.Agent{a}, nil)
	crew.Process = crewai.Staged
	crew.Stages = []crewai.Stage{{Name: "s1", Tasks: []*crewai.Task{t1}}}
	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("staged with Async=true must still run: %v", err)
	}
	if out.Final != "ok" {
		t.Errorf("Final = %q, want %q", out.Final, "ok")
	}
}
