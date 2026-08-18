package crewai

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// progressCollector records every Progress it sees, in order, with a
// mutex protecting the slice so a thread-safe callback is not
// required for the test itself (the callback IS still expected to be
// thread-safe in real usage; we just use a lock to be strict).
type progressCollector struct {
	mu      sync.Mutex
	events  []Progress
	panics  int
	panicAt string
}

func (c *progressCollector) Collect(p Progress) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.panicAt != "" && p.Event == c.panicAt {
		defer func() {
			if r := recover(); r != nil {
				c.panics++
			}
		}()
		panic("callback boom")
	}
	c.events = append(c.events, p)
}

func (c *progressCollector) Snapshot() []Progress {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Progress, len(c.events))
	copy(out, c.events)
	return out
}

// progressTool returns a fixed observation, used to exercise the
// tool_invoked ReAct event without re-running the full tool protocol
// multiple times.
type progressTool struct{}

func (p *progressTool) Name() string        { return "ptool" }
func (p *progressTool) Description() string { return "records progress" }
func (p *progressTool) Call(_ context.Context, _ string) (string, error) {
	return "ok", nil
}

// reactProgressLLM calls "ptool" once and then returns a Final Answer,
// triggering exactly one tool_invoked event.
type reactProgressLLM struct {
	calls int
	mu    sync.Mutex
}

func (r *reactProgressLLM) Call(_ context.Context, _ []Message) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	switch r.calls {
	case 1:
		return "Thought: call the tool.\nAction: ptool\nAction Input: ", nil
	}
	return "Thought: done.\nFinal Answer: finished.", nil
}
func (r *reactProgressLLM) Model() string { return "react-progress" }

func TestProgress_Sequential_Events(t *testing.T) {
	coll := &progressCollector{}
	llm := &progressLLMStub{out: "ok"}
	agent := NewAgent("A", "g", "b", llm)
	task := NewTask("x", "y", agent)
	crew := NewCrew([]*Agent{agent}, []*Task{task}).WithProgress(coll.Collect)

	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	events := coll.Snapshot()
	if len(events) < 2 {
		t.Fatalf("expected >= 2 events, got %d: %+v", len(events), events)
	}
	if events[0].Event != "task_started" {
		t.Fatalf("first event must be task_started, got %q", events[0].Event)
	}
	if events[len(events)-1].Event != "task_completed" {
		t.Fatalf("last event must be task_completed, got %q", events[len(events)-1].Event)
	}
	for _, e := range events {
		if e.Event == "tool_invoked" {
			if e.Tool == "" {
				t.Fatalf("tool_invoked must carry a Tool name")
			}
			if e.Duration <= 0 {
				t.Fatalf("tool_invoked must carry a non-zero Duration")
			}
		}
	}
}

func TestProgress_Staged_Order(t *testing.T) {
	coll := &progressCollector{}
	a1 := NewAgent("a1", "g", "b", &progressLLMStub{out: "a1"})
	a2 := NewAgent("a2", "g", "b", &progressLLMStub{out: "a2"})
	b := NewAgent("b", "g", "b", &progressLLMStub{out: "b"})
	t1 := NewTask("t1", "x", a1)
	t2 := NewTask("t2", "x", a2)
	t3 := NewTask("t3", "x", b)
	crew := NewCrew([]*Agent{a1, a2, b}, nil).
		WithProgress(coll.Collect)
	crew.Process = Staged
	crew.Stages = []Stage{
		{Name: "stage1", Tasks: []*Task{t1, t2}},
		{Name: "stage2", Tasks: []*Task{t3}},
	}

	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	events := coll.Snapshot()
	got := make([]string, len(events))
	for i, e := range events {
		got[i] = e.Event + "|" + e.Stage + "|" + e.Task
	}

	// Stage labels are fixed. Task labels fall back to "Task N"
	// because the tasks have no Name. Within a parallel stage the
	// order of completion across goroutines is non-deterministic, so
	// we assert via multiset counts keyed by (event, stage, task).
	type key struct {
		event, stage, task string
	}
	want := map[key]int{
		{"stage_started", "stage1", ""}:        1,
		{"stage_completed", "stage1", ""}:      1,
		{"task_started", "stage1", "Task 1"}:   1,
		{"task_started", "stage1", "Task 2"}:   1,
		{"task_completed", "stage1", "Task 1"}: 1,
		{"task_completed", "stage1", "Task 2"}: 1,
		{"stage_started", "stage2", ""}:        1,
		{"stage_completed", "stage2", ""}:      1,
		{"task_started", "stage2", "Task 1"}:   1,
		{"task_completed", "stage2", "Task 1"}: 1,
	}
	got2 := map[key]int{}
	for _, e := range events {
		got2[key{e.Event, e.Stage, e.Task}]++
	}
	for k, v := range want {
		if got2[k] != v {
			t.Fatalf("event %v: got %d, want %d (full got: %v)", k, got2[k], v, got2)
		}
	}
	for k, v := range got2 {
		if _, ok := want[k]; !ok && v > 0 {
			t.Fatalf("unexpected event %v count=%d (full: %v)", k, v, got2)
		}
	}
}

func TestProgress_ToolInvoked_NativeAndReact(t *testing.T) {
	t.Run("react", func(t *testing.T) {
		coll := &progressCollector{}
		llm := &reactProgressLLM{}
		agent := NewAgent("A", "g", "b", llm)
		agent.Tools = []Tool{&progressTool{}}
		task := NewTask("x", "y", agent)
		crew := NewCrew([]*Agent{agent}, []*Task{task}).WithProgress(coll.Collect)

		if _, err := crew.Kickoff(context.Background(), nil); err != nil {
			t.Fatal(err)
		}
		assertOneToolInvoked(t, coll.Snapshot(), "ptool")
	})

	t.Run("native", func(t *testing.T) {
		coll := &progressCollector{}
		llm := &nativeProgressLLM{}
		agent := NewAgent("A", "g", "b", llm)
		agent.ToolMode = ToolModeNative
		agent.Tools = []Tool{&progressTool{}}
		task := NewTask("x", "y", agent)
		crew := NewCrew([]*Agent{agent}, []*Task{task}).WithProgress(coll.Collect)

		if _, err := crew.Kickoff(context.Background(), nil); err != nil {
			t.Fatal(err)
		}
		assertOneToolInvoked(t, coll.Snapshot(), "ptool")
	})
}

func assertOneToolInvoked(t *testing.T, events []Progress, wantTool string) {
	t.Helper()
	got := 0
	for _, e := range events {
		if e.Event != "tool_invoked" {
			continue
		}
		if e.Tool != wantTool {
			t.Fatalf("tool_invoked Tool=%q, want %s", e.Tool, wantTool)
		}
		if e.Duration <= 0 {
			t.Fatalf("Duration must be > 0")
		}
		got++
	}
	if got != 1 {
		t.Fatalf("expected 1 tool_invoked, got %d", got)
	}
}

// nativeProgressLLM implements ToolCallingLLM and issues exactly one
// call to "ptool" so the native executeTaskWithTools path emits
// tool_invoked.
type nativeProgressLLM struct {
	calls int
	mu    sync.Mutex
}

func (n *nativeProgressLLM) Call(_ context.Context, _ []Message) (string, error) {
	return "unused", nil
}
func (n *nativeProgressLLM) Model() string { return "native-progress" }
func (n *nativeProgressLLM) CallWithTools(_ context.Context, _ []Message, _ []ToolSpec) (*ToolCallResponse, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.calls++
	if n.calls == 1 {
		return &ToolCallResponse{ToolCalls: []ToolCall{{
			Function: ToolCallFunction{Name: "ptool", Arguments: []byte(`{}`)},
		}}}, nil
	}
	return &ToolCallResponse{Content: "done"}, nil
}

func TestProgress_CallbackPanic_DoesNotAbort(t *testing.T) {
	coll := &progressCollector{panicAt: "task_started"}
	llm := &progressLLMStub{out: "ok"}
	agent := NewAgent("A", "g", "b", llm)
	task := NewTask("x", "y", agent)
	crew := NewCrew([]*Agent{agent}, []*Task{task}).WithProgress(coll.Collect)

	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("Kickoff must not be aborted by callback panic: %v", err)
	}
	if out == nil {
		t.Fatal("Kickoff returned nil output")
	}
	if coll.panics < 1 {
		t.Fatalf("expected at least 1 panic, got %d", coll.panics)
	}
}

func TestProgress_NeverIncludesPromptOrOutput(t *testing.T) {
	// The collector records full Progress values for later inspection.
	coll := &progressCollector{}
	llm := &reactProgressLLM{}
	agent := NewAgent("A", "g", "b", llm)
	agent.Tools = []Tool{&progressTool{}}
	task := NewTask("top-secret CNPJ", "y", agent)
	crew := NewCrew([]*Agent{agent}, []*Task{task}).WithProgress(coll.Collect)
	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	for _, e := range coll.Snapshot() {
		// The malicious-looking tokens must NOT appear anywhere in the
		// Progress payload. Tool name is the only metadata we expose.
		if strings.Contains(e.Task, "top-secret") {
			t.Fatalf("Progress leaked task description: %q", e.Task)
		}
	}
}

func TestProgress_TaskCompleted_ErrRedacted(t *testing.T) {
	coll := &progressCollector{}
	llm := &progressLLMStub{err: errors.New("upstream: Bearer SECRET-TOKEN-VALUE")}
	agent := NewAgent("A", "g", "b", llm)
	task := NewTask("x", "y", agent)
	crew := NewCrew([]*Agent{agent}, []*Task{task}).WithProgress(coll.Collect)
	_, _ = crew.Kickoff(context.Background(), nil)

	for _, e := range coll.Snapshot() {
		if e.Event == "task_completed" && e.Err != nil {
			if strings.Contains(e.Err.Error(), "SECRET-TOKEN-VALUE") {
				t.Fatalf("redaction failed: %v", e.Err)
			}
		}
	}
}

func TestProgress_EmitterNilCtx(t *testing.T) {
	// emitProgress with no callback attached is a silent no-op.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("emitProgress panicked on nil callback: %v", r)
		}
	}()
	emitProgress(context.Background(), Progress{Event: "x"})
}

func TestContextWithProgress_NilNoOp(t *testing.T) {
	ctx := ContextWithProgress(context.Background(), nil)
	if v := ctx.Value(progressKey{}); v != nil {
		t.Fatalf("nil fn must not be attached")
	}
	emitProgress(ctx, Progress{Event: "x"})
}

// progressLLMStub is a plain LLM stub that returns a fixed string.
// Imported tests do not need tool calling.
type progressLLMStub struct {
	out string
	err error
}

func (p *progressLLMStub) Call(_ context.Context, _ []Message) (string, error) {
	if p.err != nil {
		return "", p.err
	}
	return p.out, nil
}
func (p *progressLLMStub) Model() string { return "progress-stub" }

// guard against unused-time import in future refactors.
var _ = time.Nanosecond
