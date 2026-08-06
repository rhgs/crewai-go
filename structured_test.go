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

	out, err := executeStructured(context.Background(), agent, task, "", nopLogger{})
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
		`{"name":"Alice","age":30}`,                       // valid JSON
	}}
	agent := NewAgent("Extractor", "extract", "", llm)
	task := NewTask("Extract name and age.", "JSON", agent)
	task.Structured = &StructuredOutput{Schema: personSchema(t), RepairMax: 2}

	out, err := executeStructured(context.Background(), agent, task, "", nopLogger{})
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

	_, err := executeStructured(context.Background(), agent, task, "", nopLogger{})
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
		`{"name":"Alice"}`,    // missing age
		`{"name":"Alice","age":30}`, // valid
	}}
	agent := NewAgent("Extractor", "extract", "", llm)
	task := NewTask("Extract name and age.", "JSON", agent)
	task.Structured = &StructuredOutput{Schema: personSchema(t), RepairMax: 1}

	out, err := executeStructured(context.Background(), agent, task, "", nopLogger{})
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

	_, err := executeStructured(context.Background(), agent, task, "", nopLogger{})
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

	out, err := executeStructured(context.Background(), agent, task, "", nopLogger{})
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

	out, err := executeStructured(context.Background(), agent, task, "", nopLogger{})
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

	out, err := executeStructured(context.Background(), agent, task, "", nopLogger{})
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

	if _, err := executeStructured(context.Background(), agent, task, "", nopLogger{}); err != ErrNoLLM {
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

	if _, err := executeStructured(ctx, agent, task, "", nopLogger{}); err != context.Canceled {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}

func TestExecuteStructured_InvalidSchema(t *testing.T) {
	llm := &structuredStub{responses: []string{`{"name":"Alice","age":30}`}}
	agent := NewAgent("Extractor", "extract", "", llm)
	task := NewTask("Extract name and age.", "JSON", agent)
	task.Structured = &StructuredOutput{Schema: json.RawMessage(`not valid json`)}

	_, err := executeStructured(context.Background(), agent, task, "", nopLogger{})
	if !errors.Is(err, ErrInvalidOutput) {
		t.Errorf("expected ErrInvalidOutput, got %v", err)
	}
}

func TestExecuteStructured_EmptySchema(t *testing.T) {
	llm := &structuredStub{responses: []string{`{"name":"Alice","age":30}`}}
	agent := NewAgent("Extractor", "extract", "", llm)
	task := NewTask("Extract name and age.", "JSON", agent)
	task.Structured = &StructuredOutput{}

	_, err := executeStructured(context.Background(), agent, task, "", nopLogger{})
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

	_, err := executeStructured(context.Background(), agent, task, "", nopLogger{})
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

	out, _, err := executeTask(context.Background(), agent, task, "", nopLogger{})
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

	out, _, err := executeTask(context.Background(), agent, task, "", nopLogger{})
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

	if _, _, err := executeTask(context.Background(), agent, task, "", nopLogger{}); err != ErrNoLLM {
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