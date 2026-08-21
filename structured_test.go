package crewai

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// structuredStub is a minimal LLM stub for structured-output tests that
// records the number of calls and returns predefined responses in order.
type structuredStub struct {
	responses []string
	idx       int
	calls     int
}

func (s *structuredStub) Call(_ context.Context, _ []Message) (string, error) {
	s.calls++
	i := s.idx
	if s.idx < len(s.responses)-1 {
		s.idx++
	}
	return s.responses[i], nil
}
func (s *structuredStub) Model() string { return "stub" }

// dynamicStub delegates each call to a handler function, allowing the
// test to inspect the messages and return a response dynamically.
type dynamicStub struct {
	handler func(ctx context.Context, messages []Message) (string, error)
	calls   int
}

func (d *dynamicStub) Call(ctx context.Context, messages []Message) (string, error) {
	d.calls++
	return d.handler(ctx, messages)
}
func (d *dynamicStub) Model() string { return "dynamic-stub" }

func personSchema(t *testing.T) json.RawMessage {
	t.Helper()
	return mustRaw(t, map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
			"age":  map[string]any{"type": "integer"},
		},
		"required": []any{"name", "age"},
	})
}

func TestExecuteStructured_ValidJSON(t *testing.T) {
	llm := &structuredStub{responses: []string{`{"name":"Alice","age":30}`}}
	agent := NewAgent("Extractor", "extract", "", llm)
	task := NewTask("Extract name and age.", "JSON", agent)
	task.Structured = &StructuredOutput{Schema: personSchema(t)}

	out, _, err := executeStructured(context.Background(), agent, task, "", testLogger())
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if llm.calls != 1 {
		t.Errorf("expected 1 call, got %d", llm.calls)
	}
	// Verify canonicalization: keys are sorted, compact.
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if m["name"] != "Alice" {
		t.Errorf("name = %v, want Alice", m["name"])
	}
	if m["age"] != float64(30) {
		t.Errorf("age = %v, want 30", m["age"])
	}
	if strings.Contains(out, "  ") {
		t.Errorf("output should be compact, got: %q", out)
	}
}

func TestExecuteStructured_RepairConverges(t *testing.T) {
	llm := &structuredStub{responses: []string{
		"Sorry, here is the answer: Alice is 30 years old.", // non-JSON
		`{"name":"Alice","age":30}`,                         // valid JSON
	}}
	agent := NewAgent("Extractor", "extract", "", llm)
	task := NewTask("Extract name and age.", "JSON", agent)
	task.Structured = &StructuredOutput{Schema: personSchema(t), RepairMax: 2}

	out, _, err := executeStructured(context.Background(), agent, task, "", testLogger())
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if llm.calls != 2 {
		t.Errorf("expected 2 calls, got %d", llm.calls)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if m["name"] != "Alice" {
		t.Errorf("name = %v, want Alice", m["name"])
	}
}

func TestExecuteStructured_RepairBudgetExceeded(t *testing.T) {
	llm := &structuredStub{responses: []string{
		`{"name":"Alice"}`, // missing age
		`{"name":"Alice"}`, // still missing age
		`{"name":"Alice"}`, // still missing age
	}}
	agent := NewAgent("Extractor", "extract", "", llm)
	task := NewTask("Extract name and age.", "JSON", agent)
	task.Structured = &StructuredOutput{Schema: personSchema(t), RepairMax: 2}

	_, _, err := executeStructured(context.Background(), agent, task, "", testLogger())
	if !errors.Is(err, ErrRepairBudgetExceeded) {
		t.Fatalf("expected ErrRepairBudgetExceeded, got %v", err)
	}
	// 1 initial + 2 repairs = 3 calls.
	if llm.calls != 3 {
		t.Errorf("expected 3 calls, got %d", llm.calls)
	}
}

func TestExecuteStructured_RepairBudgetCustom1(t *testing.T) {
	llm := &structuredStub{responses: []string{
		`{"name":"Alice"}`,          // missing age
		`{"name":"Alice","age":30}`, // valid
	}}
	agent := NewAgent("Extractor", "extract", "", llm)
	task := NewTask("Extract name and age.", "JSON", agent)
	task.Structured = &StructuredOutput{Schema: personSchema(t), RepairMax: 1}

	out, _, err := executeStructured(context.Background(), agent, task, "", testLogger())
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if llm.calls != 2 {
		t.Errorf("expected 2 calls, got %d", llm.calls)
	}
	if !strings.Contains(out, "Alice") {
		t.Errorf("output = %q", out)
	}
}

func TestExecuteStructured_RepairBudgetExceededCustom1(t *testing.T) {
	llm := &structuredStub{responses: []string{
		`{"name":"Alice"}`,
		`{"name":"Alice"}`,
	}}
	agent := NewAgent("Extractor", "extract", "", llm)
	task := NewTask("Extract name and age.", "JSON", agent)
	task.Structured = &StructuredOutput{Schema: personSchema(t), RepairMax: 1}

	_, _, err := executeStructured(context.Background(), agent, task, "", testLogger())
	if !errors.Is(err, ErrRepairBudgetExceeded) {
		t.Fatalf("expected ErrRepairBudgetExceeded, got %v", err)
	}
	if llm.calls != 2 {
		t.Errorf("expected 2 calls (1 initial + 1 repair), got %d", llm.calls)
	}
}

func TestExecuteStructured_MissingRequiredRepair(t *testing.T) {
	var lastRepairPrompt string
	llm := &dynamicStub{
		handler: func(_ context.Context, msgs []Message) (string, error) {
			if len(msgs) == 2 {
				// Initial call.
				return `{"name":"Alice"}`, nil // missing age
			}
			// Repair call: the last user message is the repair prompt.
			lastRepairPrompt = msgs[len(msgs)-1].Content
			return `{"name":"Alice","age":30}`, nil
		},
	}
	agent := NewAgent("Extractor", "extract", "", llm)
	task := NewTask("Extract name and age.", "JSON", agent)
	task.Structured = &StructuredOutput{Schema: personSchema(t), RepairMax: 2}

	out, _, err := executeStructured(context.Background(), agent, task, "", testLogger())
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if llm.calls != 2 {
		t.Errorf("expected 2 calls, got %d", llm.calls)
	}
	// Verify the repair prompt mentions the missing field "age".
	if !strings.Contains(lastRepairPrompt, "age") {
		t.Errorf("repair prompt should mention 'age': %q", lastRepairPrompt)
	}
	// Verify the repair prompt mentions the previous output.
	if !strings.Contains(lastRepairPrompt, "Alice") {
		t.Errorf("repair prompt should mention previous output: %q", lastRepairPrompt)
	}
	// Verify the repair prompt includes the schema.
	if !strings.Contains(lastRepairPrompt, "name") || !strings.Contains(lastRepairPrompt, "required") {
		t.Errorf("repair prompt should include schema keywords: %q", lastRepairPrompt)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("output invalid: %v", err)
	}
	if m["age"] != float64(30) {
		t.Errorf("age = %v, want 30", m["age"])
	}
}

func TestExecuteStructured_Canonicalization(t *testing.T) {
	// Provide JSON with extra whitespace and different key order.
	llm := &structuredStub{responses: []string{`{  "age" : 30, "name" : "Alice"  }`}}
	agent := NewAgent("Extractor", "extract", "", llm)
	task := NewTask("Extract name and age.", "JSON", agent)
	task.Structured = &StructuredOutput{Schema: personSchema(t)}

	out, _, err := executeStructured(context.Background(), agent, task, "", testLogger())
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	// Canonical output: compact, keys sorted alphabetically.
	want := `{"age":30,"name":"Alice"}`
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestExecuteStructured_CodeFenceStripped(t *testing.T) {
	llm := &structuredStub{responses: []string{"```json\n{\"name\":\"Alice\",\"age\":30}\n```"}}
	agent := NewAgent("Extractor", "extract", "", llm)
	task := NewTask("Extract name and age.", "JSON", agent)
	task.Structured = &StructuredOutput{Schema: personSchema(t)}

	out, _, err := executeStructured(context.Background(), agent, task, "", testLogger())
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if llm.calls != 1 {
		t.Errorf("expected 1 call, got %d", llm.calls)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if m["name"] != "Alice" {
		t.Errorf("name = %v", m["name"])
	}
}

func TestExecuteStructured_NoLLM(t *testing.T) {
	agent := &Agent{Role: "X"}
	task := NewTask("t", "", agent)
	task.Structured = &StructuredOutput{Schema: personSchema(t)}

	if _, _, err := executeStructured(context.Background(), agent, task, "", testLogger()); err != ErrNoLLM {
		t.Errorf("error = %v, want %v", err, ErrNoLLM)
	}
}

func TestExecuteStructured_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	llm := &structuredStub{responses: []string{`{"name":"Alice","age":30}`}}
	agent := NewAgent("Extractor", "extract", "", llm)
	task := NewTask("Extract name and age.", "JSON", agent)
	task.Structured = &StructuredOutput{Schema: personSchema(t)}

	if _, _, err := executeStructured(ctx, agent, task, "", testLogger()); err != context.Canceled {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}

func TestExecuteStructured_InvalidSchema(t *testing.T) {
	llm := &structuredStub{responses: []string{`{"name":"Alice","age":30}`}}
	agent := NewAgent("Extractor", "extract", "", llm)
	task := NewTask("Extract name and age.", "JSON", agent)
	task.Structured = &StructuredOutput{Schema: json.RawMessage(`not valid json`)}

	_, _, err := executeStructured(context.Background(), agent, task, "", testLogger())
	if !errors.Is(err, ErrInvalidOutput) {
		t.Errorf("expected ErrInvalidOutput, got %v", err)
	}
}

func TestExecuteStructured_EmptySchema(t *testing.T) {
	llm := &structuredStub{responses: []string{`{"name":"Alice","age":30}`}}
	agent := NewAgent("Extractor", "extract", "", llm)
	task := NewTask("Extract name and age.", "JSON", agent)
	task.Structured = &StructuredOutput{}

	_, _, err := executeStructured(context.Background(), agent, task, "", testLogger())
	if !errors.Is(err, ErrInvalidOutput) {
		t.Errorf("expected ErrInvalidOutput, got %v", err)
	}
}

func TestExecuteStructured_DefaultRepairMax(t *testing.T) {
	// Always invalid; with no RepairMax set, default should be 2.
	llm := &structuredStub{responses: []string{
		`{"name":"Alice"}`,
		`{"name":"Alice"}`,
		`{"name":"Alice"}`,
	}}
	agent := NewAgent("Extractor", "extract", "", llm)
	task := NewTask("Extract name and age.", "JSON", agent)
	task.Structured = &StructuredOutput{Schema: personSchema(t)} // RepairMax=0 -> default 2

	_, _, err := executeStructured(context.Background(), agent, task, "", testLogger())
	if !errors.Is(err, ErrRepairBudgetExceeded) {
		t.Fatalf("expected ErrRepairBudgetExceeded, got %v", err)
	}
	if llm.calls != 3 {
		t.Errorf("expected 3 calls (1 + 2 default repairs), got %d", llm.calls)
	}
}

func TestNewStructuredOutput(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
		"required": []any{"name"},
	}
	s, err := NewStructuredOutput(schema, WithRepairMax(5))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if s.RepairMax != 5 {
		t.Errorf("RepairMax = %d, want 5", s.RepairMax)
	}
	var parsed map[string]any
	if err := json.Unmarshal(s.Schema, &parsed); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	if parsed["type"] != "object" {
		t.Errorf("schema type = %v, want object", parsed["type"])
	}
}

func TestNewStructuredOutput_MarshalError(t *testing.T) {
	// A channel cannot be marshaled to JSON.
	_, err := NewStructuredOutput(make(chan int))
	if err == nil {
		t.Fatal("expected error for unmarshalable schema")
	}
}

// --- Agent.Execute integration ---

func TestAgentExecute_Structured(t *testing.T) {
	llm := &structuredStub{responses: []string{`{"name":"Alice","age":30}`}}
	agent := NewAgent("Extractor", "extract", "", llm)
	task := NewTask("Extract name and age.", "JSON", agent)
	task.Structured = &StructuredOutput{Schema: personSchema(t)}

	out, err := agent.Execute(context.Background(), task)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	// agent.Execute returns the raw result; it does not call setOutput.
	if err := task.setOutput(out); err != nil {
		t.Fatalf("setOutput error: %v", err)
	}
	if task.Output() != out {
		t.Errorf("Task.Output() = %q, agent output = %q", task.Output(), out)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(task.Output()), &m); err != nil {
		t.Fatalf("Task.Output() is not valid JSON: %v", err)
	}
}

// --- Regression: structured dispatch in executeTask ---

func TestExecuteTaskStructuredDispatch(t *testing.T) {
	llm := &structuredStub{responses: []string{`{"name":"Alice","age":30}`}}
	agent := NewAgent("Extractor", "extract", "", llm)
	task := NewTask("Extract name and age.", "JSON", agent)
	task.Structured = &StructuredOutput{Schema: personSchema(t)}

	out, _, err := executeTask(context.Background(), agent, task, "", testLogger())
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if llm.calls != 1 {
		t.Errorf("expected 1 call (structured), got %d", llm.calls)
	}
	if !strings.Contains(out, "Alice") {
		t.Errorf("output = %q", out)
	}
}

func TestExecuteTaskStructuredDispatchWithTools(t *testing.T) {
	// Even with tools configured, structured mode should bypass ReAct.
	llm := &structuredStub{responses: []string{`{"name":"Alice","age":30}`}}
	agent := NewAgent("Extractor", "extract", "", llm)
	agent.WithTools(NewTool("calc", "calculator", func(_ context.Context, _ string) (string, error) {
		return "should not be called", nil
	}))
	task := NewTask("Extract name and age.", "JSON", agent)
	task.Structured = &StructuredOutput{Schema: personSchema(t)}

	out, _, err := executeTask(context.Background(), agent, task, "", testLogger())
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if llm.calls != 1 {
		t.Errorf("expected 1 call (structured bypasses tools), got %d", llm.calls)
	}
	if !strings.Contains(out, "Alice") {
		t.Errorf("output = %q", out)
	}
}

func TestExecuteTaskStructuredNoLLM(t *testing.T) {
	agent := &Agent{Role: "X"}
	task := NewTask("t", "", agent)
	task.Structured = &StructuredOutput{Schema: personSchema(t)}

	if _, _, err := executeTask(context.Background(), agent, task, "", testLogger()); err != ErrNoLLM {
		t.Errorf("error = %v, want %v", err, ErrNoLLM)
	}
}

// --- extractJSON unit tests ---

func TestExtractJSON(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`{"a":1}`, `{"a":1}`},
		{"  {\"a\":1}  ", `{"a":1}`},
		{"```json\n{\"a\":1}\n```", `{"a":1}`},
		{"```\n{\"a\":1}\n```", `{"a":1}`},
	}
	for _, c := range cases {
		got := extractJSON(c.in)
		if got != c.want {
			t.Errorf("extractJSON(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNewStructuredOutput_StrictSchema(t *testing.T) {
	_, err := NewStructuredOutput(map[string]any{"unevaluatedProperties": false}, WithStrictSchema())
	if err == nil {
		t.Fatal("expected unsupported keyword error")
	}
	// $ref is supported under StrictSchema
	soRef, err := NewStructuredOutput(map[string]any{
		"$defs": map[string]any{"x": map[string]any{"type": "string"}},
		"$ref":  "#/$defs/x",
	}, WithStrictSchema())
	if err != nil || soRef == nil {
		t.Fatalf("$ref schema: %v", err)
	}
	so, err := NewStructuredOutput(map[string]any{"type": "string"}, WithStrictSchema())
	if err != nil || so == nil {
		t.Fatalf("ok schema: %v", err)
	}
}

func TestWithAllowTools_Option(t *testing.T) {
	so, err := NewStructuredOutput(map[string]any{"type": "object"}, WithAllowTools(), WithToolCall())
	if err != nil {
		t.Fatal(err)
	}
	if !so.AllowTools || !so.ToolCall {
		t.Fatal("options not applied")
	}
}

// allowToolsStub sequences gather (ReAct tool use) then JSON capture.
type allowToolsSeq struct {
	responses []string
	idx       int
	calls     int
}

func (s *allowToolsSeq) Call(_ context.Context, _ []Message) (string, error) {
	s.calls++
	i := s.idx
	if s.idx < len(s.responses)-1 {
		s.idx++
	}
	return s.responses[i], nil
}
func (s *allowToolsSeq) Model() string { return "allow-tools-stub" }

func TestExecuteStructured_AllowToolsGatherThenJSON(t *testing.T) {
	ft := NewFactSourceTool(
		"lookup",
		"lookup tool",
		func(context.Context, string) (string, error) { return "value=42", nil },
		func(_ context.Context, output string) []Fact {
			return []Fact{NewFact("value is 42", "test", "http://example.test", []byte(output))}
		},
	)
	// Call 1: ReAct tool; Call 2: Final Answer summary; Call 3: JSON capture
	llm := &allowToolsSeq{responses: []string{
		"Thought: need data\nAction: lookup\nAction Input: q",
		"Final Answer: found 42",
		`{"name":"Alice","age":30}`,
	}}
	agent := NewAgent("Extractor", "extract", "", llm)
	agent.Tools = []Tool{ft}
	task := NewTask("Extract name and age using tools if needed.", "JSON", agent)
	task.Structured = &StructuredOutput{Schema: personSchema(t), AllowTools: true}

	out, facts, err := executeStructured(context.Background(), agent, task, "", testLogger())
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if llm.calls < 3 {
		t.Fatalf("expected gather+capture calls, got %d", llm.calls)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatal(err)
	}
	if m["name"] != "Alice" {
		t.Fatalf("name=%v", m["name"])
	}
	if len(facts) != 1 {
		t.Fatalf("facts=%d want 1", len(facts))
	}
}

func TestExecuteStructured_AllowToolsFalseSkipsTools(t *testing.T) {
	called := false
	ft := NewTool("lookup", "t", func(context.Context, string) (string, error) {
		called = true
		return "x", nil
	})
	llm := &structuredStub{responses: []string{`{"name":"Bob","age":20}`}}
	agent := NewAgent("E", "e", "", llm)
	agent.Tools = []Tool{ft}
	task := NewTask("x", "JSON", agent)
	task.Structured = &StructuredOutput{Schema: personSchema(t), AllowTools: false}
	if _, _, err := executeStructured(context.Background(), agent, task, "", testLogger()); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("tool must not be called when AllowTools is false")
	}
}

func TestExecuteStructured_AllowToolsGatherExhaustWarning(t *testing.T) {
	// Always request a tool — exhaust MaxIterations, then still capture JSON.
	llm := &allowToolsSeq{responses: []string{
		"Action: lookup\nAction Input: a",
		"Action: lookup\nAction Input: b",
		"Action: lookup\nAction Input: c",
		// after exhaust, capture phase uses Call again
		`{"name":"Zed","age":1}`,
	}}
	ft := NewTool("lookup", "t", func(context.Context, string) (string, error) {
		return "ok", nil
	})
	agent := NewAgent("E", "e", "", llm)
	agent.MaxIterations = 2
	agent.Tools = []Tool{ft}
	task := NewTask("x", "JSON", agent)
	task.Structured = &StructuredOutput{Schema: personSchema(t), AllowTools: true, RepairMax: 0}
	out, _, err := executeStructured(context.Background(), agent, task, "", testLogger())
	if err != nil {
		t.Fatalf("should capture after exhaust: %v", err)
	}
	warns := task.Warnings()
	found := false
	for _, w := range warns {
		if strings.Contains(w, "gather budget exhausted") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected gather warning, got %v", warns)
	}
	if !strings.Contains(out, "Zed") {
		t.Fatalf("out=%s", out)
	}
}
