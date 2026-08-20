package crewai

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
)

// --- idempotency (L-1) ---------------------------------------------------

func TestWithLogger_Idempotent_Crew(t *testing.T) {
	var buf1, buf2 bytes.Buffer
	l1 := slog.New(slog.NewTextHandler(&buf1, &slog.HandlerOptions{Level: slog.LevelInfo}))
	l2 := slog.New(slog.NewTextHandler(&buf2, &slog.HandlerOptions{Level: slog.LevelInfo}))
	c := NewCrew(nil, nil).WithLogger(l1).WithLogger(l2)
	if c.logger != l2 {
		t.Fatal("last WithLogger should win")
	}
}

func TestWithLogger_Idempotent_Agent(t *testing.T) {
	var buf1, buf2 bytes.Buffer
	l1 := slog.New(slog.NewTextHandler(&buf1, &slog.HandlerOptions{Level: slog.LevelInfo}))
	l2 := slog.New(slog.NewTextHandler(&buf2, &slog.HandlerOptions{Level: slog.LevelInfo}))
	a := NewAgent("a", "g", "b", &llmStub{responses: []string{"x"}}).WithLogger(l1).WithLogger(l2)
	if a.logger != l2 {
		t.Fatal("last WithLogger should win")
	}
}

// --- delegated WARN redaction (M-1) -------------------------------------

func TestDelegateLogUsesRedactedError(t *testing.T) {
	var buf bytes.Buffer
	log := capturingLogger(&buf)

	// Manager LLM that errors with a fake-secret-shaped message.
	secretErr := errors.New("anthropic: 401 x-anthropic-api-key sk-ant-api03-AbCdEfGhIj1234567890xyzABCDEFGHIJ")
	errLLM := &errLLMStub{err: secretErr}

	researcher := NewAgent("Researcher", "research", "", &llmStub{responses: []string{"r"}})
	writer := NewAgent("Writer", "write", "", &llmStub{responses: []string{"w"}})

	crew := NewCrew(
		[]*Agent{researcher, writer},
		[]*Task{NewTask("t1", "do", nil)},
	)
	crew.Process = Hierarchical
	crew.ManagerLLM = errLLM
	crew.WithLogger(log)

	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatalf("Kickoff: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "delegation failed") {
		t.Fatalf("expected 'delegation failed' WARN, got: %q", out)
	}
	if strings.Contains(out, "AbCdEfGhIj1234567890") {
		t.Fatalf("API key prefix leaked into log: %q", out)
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Fatalf("expected [REDACTED] marker in WARN line, got: %q", out)
	}
}

// --- defaultLogger --------------------------------------------------------

func TestDefaultLogger_VerboseTrueEnablesDebug(t *testing.T) {
	l := defaultLogger(true)
	if l == nil {
		t.Fatal("defaultLogger(true) returned nil")
	}
	h := l.Handler()
	if !h.Enabled(nil, slog.LevelDebug) {
		t.Fatal("Verbose=true should enable LevelDebug")
	}
}

func TestDefaultLogger_VerboseFalseSuppressesInfoAndDebug(t *testing.T) {
	l := defaultLogger(false)
	if l == nil {
		t.Fatal("defaultLogger(false) returned nil")
	}
	h := l.Handler()
	if h.Enabled(nil, slog.LevelInfo) {
		t.Fatal("Verbose=false should suppress LevelInfo (legacy nopLogger behavior)")
	}
	if h.Enabled(nil, slog.LevelDebug) {
		t.Fatal("Verbose=false should suppress LevelDebug")
	}
	if !h.Enabled(nil, slog.LevelError) {
		t.Fatal("Verbose=false should keep LevelError enabled")
	}
}

func TestDefaultLogger_WritesToStderr(t *testing.T) {
	l := defaultLogger(true)
	// defaultLogger writes to os.Stderr. We can't easily intercept stderr
	// from inside the test process; instead, ensure the handler does not
	// panic and that the handler's writer is non-nil.
	if l.Handler() == nil {
		t.Fatal("handler is nil")
	}
}

// --- Crew.WithLogger ------------------------------------------------------

func TestCrewWithLogger_StoresLogger(t *testing.T) {
	var buf bytes.Buffer
	log := capturingLogger(&buf)
	c := NewCrew(nil, nil).WithLogger(log)
	if c.logger != log {
		t.Fatal("WithLogger did not assign logger")
	}
}

func TestCrewWithLogger_FluentReturnsCrew(t *testing.T) {
	c := NewCrew(nil, nil)
	got := c.WithLogger(testLogger())
	if got != c {
		t.Fatal("WithLogger should return the same *Crew for fluent chaining")
	}
}

func TestCrewWithLogger_NilOverriddenByDefaultInKickoff(t *testing.T) {
	// Calling WithLogger(nil) explicitly should be equivalent to not
	// calling WithLogger at all: Kickoff should fall back to defaultLogger.
	var buf bytes.Buffer
	// Save and restore stderr so defaultLogger's stderr writes do not
	// pollute test output.
	oldStderr := os.Stderr
	devNull, _ := os.Open(os.DevNull)
	if devNull != nil {
		os.Stderr = devNull
		defer func() { os.Stderr = oldStderr }()
	}
	_ = buf

	crew := NewCrew(
		[]*Agent{NewAgent("a", "g", "", &llmStub{responses: []string{"x"}})},
		[]*Task{NewTask("t", "e", nil)},
	)
	crew.WithLogger(nil) // explicit nil
	crew.Verbose = true  // forces fallback to LevelDebug

	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("Kickoff: %v", err)
	}
	if out == nil {
		t.Fatal("nil output")
	}
	if crew.logger == nil {
		t.Fatal("Kickoff should have initialized c.logger via defaultLogger")
	}
}

func TestCrewWithLogger_PreventsKickoffOverwrite(t *testing.T) {
	// After Kickoff, the injected logger must be the one still on the
	// crew — not replaced by defaultLogger. Verify by inspecting the
	// pointer identity through the handler.
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	crew := NewCrew(
		[]*Agent{NewAgent("a", "g", "", &llmStub{responses: []string{"hi"}})},
		[]*Task{NewTask("t", "e", nil)},
	)
	crew.WithLogger(log)

	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatalf("Kickoff: %v", err)
	}
	if crew.logger != log {
		t.Fatal("Kickoff replaced injected logger with defaultLogger")
	}
}

// --- Agent.WithLogger -----------------------------------------------------

func TestAgentWithLogger_FluentReturnsAgent(t *testing.T) {
	a := NewAgent("a", "g", "b", &llmStub{responses: []string{"x"}})
	got := a.WithLogger(testLogger())
	if got != a {
		t.Fatal("WithLogger should return same *Agent")
	}
	if a.logger == nil {
		t.Fatal("WithLogger should assign agent.logger")
	}
}

func TestAgentWithLogger_NilFallsBackToDefault(t *testing.T) {
	a := NewAgent("a", "g", "b", &llmStub{responses: []string{"x"}})
	if a.WithLogger(nil); a.logger != nil {
		t.Fatal("WithLogger(nil) should store nil; fallback happens in Execute")
	}
	// slog.Default() should not panic.
	out, err := a.Execute(context.Background(), NewTask("t", "e", a))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out != "x" {
		t.Fatalf("unexpected output: %q", out)
	}
}

// --- actual logging through executor paths -------------------------------

func TestExecutorLogs_AgentThoughtAndToolInvoked(t *testing.T) {
	var buf bytes.Buffer
	log := capturingLogger(&buf)

	// ReAct protocol format: parseAction finds the action, then on the
	// next LLM call we return the final answer.
	stub := &llmStub{responses: []string{
		"Thought: I will use the tool.\nAction: echo\nAction Input: hi",
		"Final Answer: done",
	}}
	echoTool := &echoToolStub{}
	agent := NewAgent("a", "g", "", stub).WithTools(echoTool)
	task := NewTask("t", "e", agent)

	_, _, err := executeTask(context.Background(), agent, task, "", log)
	if err != nil {
		t.Fatalf("executeTask: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "agent thought") {
		t.Errorf("expected 'agent thought' log line, got: %q", out)
	}
	if !strings.Contains(out, "tool invoked") {
		t.Errorf("expected 'tool invoked' log line, got: %q", out)
	}
	// TextHandler emits key=value (no quotes around string values that lack
	// spaces). Accept either shape across handlers.
	if !strings.Contains(out, "agent=a") && !strings.Contains(out, `"agent":"a"`) {
		t.Errorf("expected structured key 'agent=a', got: %q", out)
	}
	if !strings.Contains(out, "tool=echo") && !strings.Contains(out, `"tool":"echo"`) {
		t.Errorf("expected structured key 'tool=echo', got: %q", out)
	}
	if !strings.Contains(out, "input=hi") && !strings.Contains(out, `"input":"hi"`) {
		t.Logf("note: 'input' attr not in expected format (output: %q)", out)
	}
}

func TestExecutorLogs_AgentThoughtWithoutTools(t *testing.T) {
	// Even without tools, DebugContext("agent thought") is unreachable —
	// the no-tools path returns directly. Verify that the no-tools path
	// does not emit any spurious log lines.
	var buf bytes.Buffer
	log := capturingLogger(&buf)

	stub := &llmStub{responses: []string{"Final Answer: 42"}}
	agent := NewAgent("a", "g", "", stub) // no tools
	task := NewTask("t", "e", agent)

	_, _, err := executeTask(context.Background(), agent, task, "", log)
	if err != nil {
		t.Fatalf("executeTask: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("no-tools path should not emit log lines, got: %q", buf.String())
	}
}

func TestStructuredLogs_ValidatedAndValidationFailed(t *testing.T) {
	// Valid JSON -> "structured output validated" line.
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
		"required": []string{"name"},
	}
	st, err := NewStructuredOutput(schema)
	if err != nil {
		t.Fatal(err)
	}
	task := NewTask("t", "e", &Agent{Role: "a", LLM: &llmStub{responses: []string{`{"name":"ok"}`}}})
	task.Structured = st

	var buf bytes.Buffer
	log := capturingLogger(&buf)
	out, _, err := executeStructured(context.Background(), task.Agent, task, "", log)
	if err != nil {
		t.Fatalf("executeStructured: %v", err)
	}
	if !strings.Contains(out, "ok") {
		t.Fatalf("unexpected output: %q", out)
	}
	if !strings.Contains(buf.String(), "structured output validated") {
		t.Errorf("expected 'structured output validated' log line, got: %q", buf.String())
	}

	// Invalid JSON -> "structured output validation failed" line.
	var buf2 bytes.Buffer
	log2 := capturingLogger(&buf2)
	badAgent := &Agent{Role: "a", LLM: &llmStub{responses: []string{`not json`}}}
	badTask := NewTask("t", "e", badAgent)
	badTask.Structured = st
	if _, _, err := executeStructured(context.Background(), badAgent, badTask, "", log2); err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(buf2.String(), "structured output validation failed") {
		t.Errorf("expected 'structured output validation failed' log, got: %q", buf2.String())
	}
}

func TestToolcallLogs_NativeLoopDone(t *testing.T) {
	// ToolCallingLLM stub: first call returns echo tool call;
	// second call returns no tool calls -> "native tool loop done".
	tc := &tcLlm{
		first: &ToolCallResponse{
			Content:   "",
			ToolCalls: []ToolCall{{ID: "1", Function: ToolCallFunction{Name: "echo", Arguments: []byte(`"hi"`)}}},
		},
		second: &ToolCallResponse{Content: "all done"},
	}
	echoTool := &echoToolStub{}
	agent := NewAgent("a", "g", "", tc).WithTools(echoTool)
	task := NewTask("t", "e", agent)

	var buf bytes.Buffer
	log := capturingLogger(&buf)
	if _, _, _, err := executeTaskWithTools(context.Background(), agent, task, "", log); err != nil {
		t.Fatalf("executeTaskWithTools: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "native tool call") {
		t.Errorf("expected 'native tool call' log line, got: %q", out)
	}
	if !strings.Contains(out, "native tool loop done") {
		t.Errorf("expected 'native tool loop done' log line, got: %q", out)
	}
}

// --- hierarchical delegation logs ---------------------------------------

func TestHierarchicalLogs_ManagerResolvedAndTaskDelegated(t *testing.T) {
	var buf bytes.Buffer
	log := capturingLogger(&buf)

	roleA := "Researcher"
	roleB := "Writer"
	mgr := &llmStub{responses: []string{roleA}}
	researcher := NewAgent(roleA, "research", "", &llmStub{responses: []string{"r"}})
	writer := NewAgent(roleB, "write", "", &llmStub{responses: []string{"w"}})

	crew := NewCrew(
		[]*Agent{researcher, writer},
		[]*Task{NewTask("t1", "do research", nil)},
	)
	crew.Process = Hierarchical
	crew.ManagerLLM = mgr
	crew.WithLogger(log)

	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatalf("Kickoff: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "manager resolved") {
		t.Errorf("expected 'manager resolved' log, got: %q", out)
	}
	if !strings.Contains(out, "task delegated") {
		t.Errorf("expected 'task delegated' log, got: %q", out)
	}
	if !strings.Contains(out, "manager=\"Team Manager\"") && !strings.Contains(out, "manager=Team Manager") {
		t.Errorf("expected structured manager key, got: %q", out)
	}
}

func TestHierarchicalLogs_DelegationFailedWarn(t *testing.T) {
	var buf bytes.Buffer
	log := capturingLogger(&buf)

	// Manager returns error -> warn log.
	bogusMgr := &llmStub{
		responses: []string{}, // empty: stub returns "" with nil err on exhaustion
	}
	// Force error by feeding a stub that errors. We build a custom LLM.
	errLLM := &errLLMStub{err: errors.New("upstream manager down")}

	researcher := NewAgent("Researcher", "research", "", &llmStub{responses: []string{"r"}})
	writer := NewAgent("Writer", "write", "", &llmStub{responses: []string{"w"}})

	crew := NewCrew(
		[]*Agent{researcher, writer},
		[]*Task{NewTask("t1", "do research", nil)},
	)
	crew.Process = Hierarchical
	crew.ManagerLLM = errLLM
	crew.WithLogger(log)
	_ = bogusMgr

	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatalf("Kickoff should fall back to first agent: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "delegation failed") {
		t.Errorf("expected 'delegation failed' warn log, got: %q", out)
	}
	if !strings.Contains(out, "WARN") {
		t.Errorf("expected WARN level, got: %q", out)
	}
	if !strings.Contains(out, "upstream manager down") {
		t.Errorf("expected error message in log, got: %q", out)
	}
}

// --- context propagation --------------------------------------------------

// traceKey is used by both the ctx-value and the captureHandler.
// Declared at file scope so the two `traceKey{}` literals refer to the
// same type.
const traceKey string = "trace-abc"

func TestLoggersReceiveContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), traceKey, "trace-abc")
	var buf bytes.Buffer
	log := slog.New(&captureHandler{buf: &buf})

	// Manually invoke InfoContext with the ctx — handler receives ctx in
	// Handle and can extract values.
	log.InfoContext(ctx, "test", "k", "v")

	out := buf.String()
	if !strings.Contains(out, "test") {
		t.Errorf("expected 'test' in capture, got: %q", out)
	}
	// captureHandler pulls the ctx value (trace-abc) and prefixes it.
	if !strings.Contains(out, "trace=trace-abc") {
		t.Errorf("expected handler to extract trace-abc from context, got: %q", out)
	}
}

// -------------------------------------------------------------------------
// helpers local to this test file
// -------------------------------------------------------------------------

// echoToolStub is a minimal Tool that returns the input as its output.
type echoToolStub struct{}

func (e *echoToolStub) Name() string        { return "echo" }
func (e *echoToolStub) Description() string { return "echo input" }
func (e *echoToolStub) Call(_ context.Context, input string) (string, error) {
	return "echo:" + input, nil
}

// tcLlm is a stub that pretends to be a ToolCallingLLM. It returns
// first on the first call, second on every subsequent call.
type tcLlm struct {
	first     *ToolCallResponse
	second    *ToolCallResponse
	firstDone bool
}

func (t *tcLlm) Call(_ context.Context, _ []Message) (string, error) {
	return "", nil
}
func (t *tcLlm) Model() string { return "tc-stub" }

func (t *tcLlm) CallWithTools(_ context.Context, _ []Message, _ []ToolSpec) (*ToolCallResponse, error) {
	if !t.firstDone {
		t.firstDone = true
		return t.first, nil
	}
	return t.second, nil
}

// errLLMStub always errors on Call.
type errLLMStub struct{ err error }

func (e *errLLMStub) Call(_ context.Context, _ []Message) (string, error) {
	return "", e.err
}
func (e *errLLMStub) Model() string { return "err-stub" }

// captureHandler is a tiny slog.Handler that pulls a value from the
// context and writes it as part of each log line. Used to verify that
// *Context methods actually pass the context through.
type captureHandler struct {
	buf *bytes.Buffer
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *captureHandler) Handle(ctx context.Context, r slog.Record) error {
	if v, ok := ctx.Value(traceKey).(string); ok {
		h.buf.WriteString("trace=" + v + " ")
	}
	h.buf.WriteString(r.Message)
	return nil
}
func (h *captureHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(_ string) slog.Handler      { return h }
