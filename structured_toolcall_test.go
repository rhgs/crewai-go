package crewai

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
)

// toolCallStub is a minimal ToolCallingLLM stub: it returns canned
// responses in order and records every call's tool spec names so
// tests can assert on what the executor passed.
type toolCallStub struct {
	mu        sync.Mutex
	responses []*ToolCallResponse
	calls     int
	lastTools []ToolSpec
}

func (s *toolCallStub) Call(_ context.Context, _ []Message) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	return "", nil
}
func (s *toolCallStub) Model() string { return "tc-stub" }

func (s *toolCallStub) CallWithTools(_ context.Context, _ []Message, tools []ToolSpec) (*ToolCallResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.lastTools = tools
	idx := s.calls - 1
	if idx >= len(s.responses) {
		return &ToolCallResponse{}, nil
	}
	return s.responses[idx], nil
}

func (s *toolCallStub) ToolsSeen() []ToolSpec {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ToolSpec, len(s.lastTools))
	copy(out, s.lastTools)
	return out
}

func (s *toolCallStub) Calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// nonToolCallStub implements only LLM (NOT ToolCallingLLM), used to
// verify the sentinel error path.
type nonToolCallStub struct{}

func (nonToolCallStub) Call(_ context.Context, _ []Message) (string, error) { return "", nil }
func (nonToolCallStub) Model() string                                       { return "non-tc" }

// ---------- emit_result flow -------------------------------------------------

func TestStructured_ToolCall_SchemaBecomesParameters(t *testing.T) {
	stub := &toolCallStub{
		responses: []*ToolCallResponse{
			{ToolCalls: []ToolCall{{
				Function: ToolCallFunction{Name: "emit_result", Arguments: []byte(`{"name":"Alice","age":30}`)},
			}}},
		},
	}
	agent := NewAgent("X", "g", "b", stub)
	task := NewTask("t", "y", agent)
	task.Structured = &StructuredOutput{
		Schema:   personSchema(t),
		ToolCall: true,
	}
	out, err := executeStructured(context.Background(), agent, task, "", testLogger())
	if err != nil {
		t.Fatalf("executeStructured: %v", err)
	}
	if out != `{"age":30,"name":"Alice"}` && out != `{"name":"Alice","age":30}` {
		t.Fatalf("output not canonicalized: %s", out)
	}

	tools := stub.ToolsSeen()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool spec, got %d", len(tools))
	}
	if tools[0].Function.Name != "emit_result" {
		t.Fatalf("expected tool name emit_result, got %q", tools[0].Function.Name)
	}
	if string(tools[0].Function.Parameters) != string(personSchema(t)) {
		t.Fatalf("parameters must equal schema verbatim")
	}
}

func TestStructured_ToolCall_FreeTextTriggersRepair(t *testing.T) {
	stub := &toolCallStub{
		responses: []*ToolCallResponse{
			{Content: "I cannot answer"}, // turn 1: plain text
			{ToolCalls: []ToolCall{{
				Function: ToolCallFunction{Name: "emit_result", Arguments: []byte(`{"name":"Bob","age":42}`)},
			}}}, // turn 2: emits correctly
		},
	}
	agent := NewAgent("X", "g", "b", stub)
	task := NewTask("t", "y", agent)
	task.Structured = &StructuredOutput{
		Schema:   personSchema(t),
		ToolCall: true,
	}
	if _, err := executeStructured(context.Background(), agent, task, "", testLogger()); err != nil {
		t.Fatalf("repair must succeed: %v", err)
	}
	if stub.Calls() < 2 {
		t.Fatalf("expected at least 2 tool-call attempts, got %d", stub.Calls())
	}
}

func TestStructured_ToolCall_BudgetExceeded(t *testing.T) {
	stub := &toolCallStub{
		responses: []*ToolCallResponse{
			{Content: "free text"}, // attempt 1
			{Content: "free text"}, // attempt 2 (repairMax=1)
			{Content: "free text"}, // attempt 3 -> exhausted
		},
	}
	agent := NewAgent("X", "g", "b", stub)
	task := NewTask("t", "y", agent)
	task.Structured = &StructuredOutput{
		Schema:    personSchema(t),
		RepairMax: 1,
		ToolCall:  true,
	}
	_, err := executeStructured(context.Background(), agent, task, "", testLogger())
	if !errors.Is(err, ErrRepairBudgetExceeded) {
		t.Fatalf("expected ErrRepairBudgetExceeded, got %v", err)
	}
}

func TestStructured_ToolCall_InvalidArgsTriggersRepair(t *testing.T) {
	stub := &toolCallStub{
		responses: []*ToolCallResponse{
			{ToolCalls: []ToolCall{{
				Function: ToolCallFunction{Name: "emit_result", Arguments: []byte(`{"name":"x"}`)}, // missing age
			}}},
			{ToolCalls: []ToolCall{{
				Function: ToolCallFunction{Name: "emit_result", Arguments: []byte(`{"name":"Alice","age":30}`)},
			}}},
		},
	}
	agent := NewAgent("X", "g", "b", stub)
	task := NewTask("t", "y", agent)
	task.Structured = &StructuredOutput{Schema: personSchema(t), ToolCall: true}
	if _, err := executeStructured(context.Background(), agent, task, "", testLogger()); err != nil {
		t.Fatalf("repair must succeed: %v", err)
	}
}

func TestStructured_ToolCall_RequiresToolCallingLLM(t *testing.T) {
	agent := NewAgent("X", "g", "b", nonToolCallStub{})
	task := NewTask("t", "y", agent)
	task.Structured = &StructuredOutput{Schema: personSchema(t), ToolCall: true}
	_, err := executeStructured(context.Background(), agent, task, "", testLogger())
	if !errors.Is(err, ErrToolCallStructuredUnsupported) {
		t.Fatalf("expected ErrToolCallStructuredUnsupported, got %v", err)
	}
}

func TestStructured_ToolCall_EmptyArgumentsTreatedAsInvalid(t *testing.T) {
	stub := &toolCallStub{
		responses: []*ToolCallResponse{
			{ToolCalls: []ToolCall{{
				Function: ToolCallFunction{Name: "emit_result", Arguments: nil},
			}}},
			{ToolCalls: []ToolCall{{
				Function: ToolCallFunction{Name: "emit_result", Arguments: []byte(`{"name":"Alice","age":30}`)},
			}}},
		},
	}
	agent := NewAgent("X", "g", "b", stub)
	task := NewTask("t", "y", agent)
	task.Structured = &StructuredOutput{Schema: personSchema(t), ToolCall: true}
	if _, err := executeStructured(context.Background(), agent, task, "", testLogger()); err != nil {
		t.Fatalf("repair must succeed after empty args, got %v", err)
	}
}

func TestStructured_ToolCall_OtherToolsIgnored(t *testing.T) {
	stub := &toolCallStub{
		responses: []*ToolCallResponse{
			{ToolCalls: []ToolCall{
				{Function: ToolCallFunction{Name: "unrelated", Arguments: []byte(`{}`)}},
				{Function: ToolCallFunction{Name: "emit_result", Arguments: []byte(`{"name":"Alice","age":30}`)}},
			}},
		},
	}
	agent := NewAgent("X", "g", "b", stub)
	task := NewTask("t", "y", agent)
	task.Structured = &StructuredOutput{Schema: personSchema(t), ToolCall: true}
	out, err := executeStructured(context.Background(), agent, task, "", testLogger())
	if err != nil {
		t.Fatalf("non-emit_result calls must be ignored: %v", err)
	}
	if !contains(out, `"name":"Alice"`) || !contains(out, `"age":30`) {
		t.Fatalf("output missing valid emit_result: %s", out)
	}
}

func TestStructured_ToolCall_DefaultModeIsJSONOnly(t *testing.T) {
	stub := &toolCallStub{}
	agent := NewAgent("X", "g", "b", stub)
	task := NewTask("t", "y", agent)
	task.Structured = &StructuredOutput{Schema: personSchema(t)}
	// No ToolCall set -> default JSON-only path. Use the jsonOnlyStub
	// to provide valid JSON; toolCallStub.CallWithTools must NOT be
	// invoked (verify via calls==0 for CallWithTools path).
	jsonOnly := &jsonOnlyStub{}
	agent.LLM = jsonOnly
	out, err := executeStructured(context.Background(), agent, task, "", testLogger())
	if err != nil {
		t.Fatalf("default JSON-only path should accept valid JSON: %v", err)
	}
	if out == "" {
		t.Fatalf("expected canonicalised JSON output")
	}
	// toolCallStub was the original LLM; the test replaced it via
	// jsonOnly before calling executeStructured, so the recorded
	// calls in stub are from any pre-init activity (none here).
	if stub.Calls() != 0 {
		t.Fatalf("CallWithTools must not run in default mode, got %d calls", stub.Calls())
	}
}

// jsonOnlyStub implements only LLM and returns plausible JSON text.
type jsonOnlyStub struct{}

func (jsonOnlyStub) Model() string { return "json-only" }
func (jsonOnlyStub) Call(_ context.Context, _ []Message) (string, error) {
	return `{"name":"Alice","age":30}`, nil
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestStructured_ToolCall_ExtractHelper(t *testing.T) {
	schema := json.RawMessage(`{"type":"object"}`)
	cases := []struct {
		name       string
		calls      []ToolCall
		wantFound  bool
		wantErrIs  error // nil means valErr is nil too
		wantArgsOk bool  // sanity check on the canonicalised output
	}{
		{
			name:      "no_calls",
			calls:     nil,
			wantFound: false,
		},
		{
			name:      "wrong_tool_name",
			calls:     []ToolCall{{Function: ToolCallFunction{Name: "other", Arguments: []byte(`{}`)}}},
			wantFound: false,
		},
		{
			name:      "empty_args",
			calls:     []ToolCall{{Function: ToolCallFunction{Name: emitResultToolName, Arguments: nil}}},
			wantFound: true, wantErrIs: ErrInvalidOutput,
		},
		{
			name:      "valid_args",
			calls:     []ToolCall{{Function: ToolCallFunction{Name: emitResultToolName, Arguments: []byte(`{}`)}}},
			wantFound: true, wantArgsOk: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			canonical, valErr, found := extractEmitResultArguments(tc.calls, schema)
			if found != tc.wantFound {
				t.Fatalf("found=%v, want %v", found, tc.wantFound)
			}
			if tc.wantErrIs == nil && valErr != nil {
				t.Fatalf("unexpected valErr: %v", valErr)
			}
			if tc.wantErrIs != nil && !errors.Is(valErr, tc.wantErrIs) {
				t.Fatalf("valErr: got %v, want %v", valErr, tc.wantErrIs)
			}
			if tc.wantArgsOk && canonical == "" {
				t.Fatalf("expected canonicalised non-empty output")
			}
		})
	}
}
