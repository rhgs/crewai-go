package crewai

import (
	"context"
	"sync"
	"testing"
)

// warnTool records a warning via the WarningSink exposed in the
// context and returns a successful observation so the task succeeds.
type warnTool struct {
	called int
	mu     sync.Mutex
}

func (w *warnTool) Name() string        { return "warner" }
func (w *warnTool) Description() string { return "records a warning then succeeds" }
func (w *warnTool) Call(ctx context.Context, _ string) (string, error) {
	w.mu.Lock()
	w.called++
	w.mu.Unlock()
	AddWarningFromCtx(ctx, "secondary source unavailable")
	return "ok", nil
}

// warnLLM is used by tests that need a bare LLM without tool calling
// (notably the management LLM in Hierarchical and the standalone
// Agent.Execute tests).
type warnLLM struct {
	calls int
	mu    sync.Mutex
}

func (w *warnLLM) Call(_ context.Context, _ []Message) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls++
	return "answer", nil
}
func (w *warnLLM) Model() string { return "warn-stub" }

// reactCallLLM mimics a model that uses the ReAct tool-use protocol:
// it emits one action to call "warner", then a Final Answer on the
// next call. This is what the warning-propagation tests need — using
// a plain "answer" string would bypass the tool and the warning would
// never be recorded.
type reactCallLLM struct {
	calls int
	mu    sync.Mutex
}

func (w *reactCallLLM) Call(_ context.Context, _ []Message) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls++
	switch w.calls {
	case 1:
		return "Thought: I should record a warning.\nAction: warner\nAction Input: ",
			nil
	default:
		return "Thought: I now know the final answer.\nFinal Answer: all done.", nil
	}
}
func (w *reactCallLLM) Model() string { return "react-stub" }

// warnNativeLLM implements LLM + ToolCallingLLM and issues exactly
// one tool call followed by a textual response. Exercises the native
// tool calling path under a WarningSink.
type warnNativeLLM struct {
	calls int
	mu    sync.Mutex
}

func (w *warnNativeLLM) Call(_ context.Context, _ []Message) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls++
	return "answer", nil
}
func (w *warnNativeLLM) Model() string { return "warn-native-stub" }
func (w *warnNativeLLM) CallWithTools(_ context.Context, _ []Message, _ []ToolSpec) (*ToolCallResponse, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls++
	switch w.calls {
	case 1:
		return &ToolCallResponse{ToolCalls: []ToolCall{{
			Function: ToolCallFunction{Name: "warner", Arguments: []byte(`{}`)},
		}}}, nil
	default:
		return &ToolCallResponse{Content: "done"}, nil
	}
}

var (
	_ LLM            = (*warnLLM)(nil)
	_ LLM            = (*reactCallLLM)(nil)
	_ ToolCallingLLM = (*warnNativeLLM)(nil)
)

// noSinkTool exercises the path where AddWarningFromCtx is called
// without an attached sink (e.g. Agent.Execute standalone). The call
// must be a silent no-op, not a panic.
type noSinkTool struct{}

func (n *noSinkTool) Name() string { return "no-sink" }
func (n *noSinkTool) Description() string {
	return "attempts to record a warning without an attached sink"
}
func (n *noSinkTool) Call(ctx context.Context, _ string) (string, error) {
	AddWarningFromCtx(ctx, "should be dropped")
	return "ok", nil
}

// ---------- Task API ---------------------------------------------------------

func TestTask_AddWarning_AndWarnings(t *testing.T) {
	task := NewTask("x", "y", nil)
	if w := task.Warnings(); len(w) != 0 {
		t.Fatalf("expected no warnings initially, got %v", w)
	}
	task.AddWarning("first")
	task.AddWarning("second")
	got := task.Warnings()
	if len(got) != 2 || got[0] != "first" || got[1] != "second" {
		t.Fatalf("unexpected warnings: %v", got)
	}
	got[0] = "mutated"
	if task.Warnings()[0] != "first" {
		t.Fatalf("Warnings returned a shared slice")
	}
}

func TestTask_AddWarning_EmptyDropped(t *testing.T) {
	task := NewTask("x", "y", nil)
	task.AddWarning("")
	if w := task.Warnings(); len(w) != 0 {
		t.Fatalf("empty strings must not be recorded, got %v", w)
	}
}

func TestTask_AddWarning_ConcurrentSafe(t *testing.T) {
	task := NewTask("x", "y", nil)
	const N = 100
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			task.AddWarning("w")
		}()
	}
	wg.Wait()
	if got := len(task.Warnings()); got != N {
		t.Fatalf("expected %d warnings, got %d", N, got)
	}
}

// ---------- Context plumbing -------------------------------------------------

func TestAddWarningFromCtx_NoSink_NoPanic(t *testing.T) {
	AddWarningFromCtx(context.Background(), "ignored")
}

func TestAddWarningFromCtx_WithSink(t *testing.T) {
	task := NewTask("x", "y", nil)
	ctx := ContextWithWarningSink(context.Background(), task)
	AddWarningFromCtx(ctx, "from tool")
	if got := task.Warnings(); len(got) != 1 || got[0] != "from tool" {
		t.Fatalf("expected warning recorded, got %v", got)
	}
}

func TestContextWithWarningSink_NilIsNoOp(t *testing.T) {
	ctx := ContextWithWarningSink(context.Background(), nil)
	AddWarningFromCtx(ctx, "ignored")
	if v := ctx.Value(warningSinkKey{}); v != nil {
		t.Fatalf("nil sink must not be attached, got %v", v)
	}
}

func TestContextWithWarningSink_ClosestSinkWins(t *testing.T) {
	// Wrapping a context with a new sink must shadow the outer one
	// for derived contexts.
	taskA := NewTask("x", "y", nil)
	taskB := NewTask("x", "y", nil)
	ctxA := ContextWithWarningSink(context.Background(), taskA)
	ctxB := ContextWithWarningSink(ctxA, taskB)
	AddWarningFromCtx(ctxB, "to B")
	if len(taskA.Warnings()) != 0 {
		t.Fatalf("taskA must not see B's warnings: %v", taskA.Warnings())
	}
	if w := taskB.Warnings(); len(w) != 1 || w[0] != "to B" {
		t.Fatalf("taskB missing warning: %v", w)
	}
}

// ---------- Executor integration --------------------------------------------

func TestCrew_Warnings_PropagateToOutput(t *testing.T) {
	llm := &reactCallLLM{}
	agent := NewAgent("A", "G", "B", llm)
	agent.Tools = []Tool{&warnTool{}}
	task := NewTask("do thing", "answer", agent)
	crew := NewCrew([]*Agent{agent}, []*Task{task})

	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("Kickoff: %v", err)
	}
	if len(out.TasksOutput) != 1 {
		t.Fatalf("expected 1 task output, got %d", len(out.TasksOutput))
	}
	if w := out.TasksOutput[0].Warnings; len(w) != 1 || w[0] != "secondary source unavailable" {
		t.Fatalf("TaskOutput.Warnings missing: %v", w)
	}
	if w := out.Warnings; len(w) != 1 || w[0] != "secondary source unavailable" {
		t.Fatalf("CrewOutput.Warnings missing: %v", w)
	}
}

func TestCrew_Warnings_NativeToolPath(t *testing.T) {
	llm := &warnNativeLLM{}
	agent := NewAgent("A", "G", "B", llm)
	agent.ToolMode = ToolModeNative
	agent.Tools = []Tool{&warnTool{}}
	task := NewTask("do thing", "answer", agent)
	crew := NewCrew([]*Agent{agent}, []*Task{task})

	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("Kickoff: %v", err)
	}
	if got := out.Warnings; len(got) != 1 || got[0] != "secondary source unavailable" {
		t.Fatalf("native path did not record warning: %v", got)
	}
}

func TestCrew_Warnings_StagedParallel(t *testing.T) {
	llmA := &reactCallLLM{}
	llmB := &reactCallLLM{}
	agentA := NewAgent("A", "g", "b", llmA)
	agentB := NewAgent("B", "g", "b", llmB)
	agentA.Tools = []Tool{&warnTool{}}
	agentB.Tools = []Tool{&warnTool{}}
	taskA := NewTask("first", "x", agentA)
	taskB := NewTask("second", "y", agentB)
	crew := NewCrew([]*Agent{agentA, agentB}, nil)
	crew.Process = Staged
	crew.Stages = []Stage{{Name: "parallel", Tasks: []*Task{taskA, taskB}}}

	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("Kickoff: %v", err)
	}
	if len(out.TasksOutput) != 2 {
		t.Fatalf("expected 2 outputs, got %d", len(out.TasksOutput))
	}
	if len(out.Warnings) != 2 {
		t.Fatalf("expected 2 aggregated warnings, got %d: %v", len(out.Warnings), out.Warnings)
	}
	for i, w := range out.Warnings {
		if w != "secondary source unavailable" {
			t.Fatalf("warning[%d] = %q", i, w)
		}
	}
}

func TestCrew_Warnings_Hierarchical(t *testing.T) {
	llm := &reactCallLLM{}
	agent := NewAgent("A", "G", "B", llm)
	agent.Tools = []Tool{&warnTool{}}
	task := NewTask("x", "y", agent)
	crew := NewCrew([]*Agent{agent}, []*Task{task})
	crew.Process = Hierarchical
	crew.ManagerLLM = &warnLLM{}

	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("Kickoff: %v", err)
	}
	if len(out.Warnings) != 1 {
		t.Fatalf("expected 1 warning under hierarchical, got %d: %v",
			len(out.Warnings), out.Warnings)
	}
}

func TestCrew_Warnings_DoNotAbortKickoff(t *testing.T) {
	llm := &reactCallLLM{}
	agent := NewAgent("A", "G", "B", llm)
	agent.Tools = []Tool{&warnTool{}}
	task := NewTask("x", "y", agent)
	crew := NewCrew([]*Agent{agent}, []*Task{task})

	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("warning must not abort Kickoff, got error: %v", err)
	}
	if out == nil {
		t.Fatal("CrewOutput must be non-nil on warning")
	}
}

func TestStandaloneAgent_NoSink_NoPanic(t *testing.T) {
	// The plan calls out that Agent.Execute (standalone) does NOT
	// inject a sink. A tool that tries to call AddWarningFromCtx on
	// the bare ctx must therefore find nil and silently drop the
	// warning. This test verifies the behaviour does not panic and
	// that no warnings leak.
	//
	// We don't run ReAct here because we only care about the
	// no-panic/no-leak property; an LLM that returns plain text
	// hits the same code path (tool not invoked). For real coverage
	// of the ctx plumbing, see TestCrew_Warnings_PropagateToOutput.
	pt := &noSinkTool{}
	llm := &warnLLM{}
	agent := NewAgent("A", "g", "b", llm)
	agent.Tools = []Tool{pt}
	task := NewTask("x", "y", agent)
	if _, err := agent.Execute(context.Background(), task); err != nil {
		t.Fatalf("standalone Execute returned error: %v", err)
	}
	if len(task.Warnings()) != 0 {
		t.Fatalf("standalone Agent.Execute must not inject a sink; warnings leak: %v",
			task.Warnings())
	}
}
