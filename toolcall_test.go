package crewai_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/rhgs/crewai-go"
	"github.com/rhgs/crewai-go/llm/mock"
)

func TestCrewNativeToolTracesPopulated(t *testing.T) {
	tool := crewai.NewTool("calc", "calc", func(_ context.Context, _ string) (string, error) {
		return "42", nil
	})
	m := &mock.LLM{
		ModelName: "mock",
		ToolCallResponses: []*crewai.ToolCallResponse{
			{ToolCalls: []crewai.ToolCall{{Function: crewai.ToolCallFunction{Name: "calc", Arguments: json.RawMessage(`"2+2"`)}}}},
			{Content: "the answer is 42"},
		},
	}
	a := crewai.NewAgent("Agent", "goal", "backstory", m)
	a.ToolMode = crewai.ToolModeNative
	a.Tools = []crewai.Tool{tool}
	task := crewai.NewTask("calc 2+2", "text", a)
	crew := crewai.NewCrew([]*crewai.Agent{a}, []*crewai.Task{task})

	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.Final != "the answer is 42" {
		t.Errorf("final = %q", out.Final)
	}
	if len(out.TasksOutput[0].ToolTraces) != 1 {
		t.Fatalf("traces = %d, want 1", len(out.TasksOutput[0].ToolTraces))
	}
	if out.TasksOutput[0].ToolTraces[0].Tool != "calc" {
		t.Errorf("trace tool = %q", out.TasksOutput[0].ToolTraces[0].Tool)
	}
}

func TestCrewReActNoToolTraces(t *testing.T) {
	m := mock.New("react answer")
	a := crewai.NewAgent("Agent", "goal", "backstory", m)
	task := crewai.NewTask("task", "text", a)
	crew := crewai.NewCrew([]*crewai.Agent{a}, []*crewai.Task{task})

	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(out.TasksOutput[0].ToolTraces) != 0 {
		t.Errorf("traces = %d, want 0", len(out.TasksOutput[0].ToolTraces))
	}
}

func TestCrewNativeToolTracesWithMultipleTools(t *testing.T) {
	calc := crewai.NewTool("calc", "calc", func(_ context.Context, _ string) (string, error) { return "4", nil })
	clock := crewai.NewTool("time", "time", func(_ context.Context, _ string) (string, error) { return "12:00", nil })
	m := &mock.LLM{
		ModelName: "mock",
		ToolCallResponses: []*crewai.ToolCallResponse{
			{ToolCalls: []crewai.ToolCall{
				{Function: crewai.ToolCallFunction{Name: "calc", Arguments: json.RawMessage(`"2+2"`)}},
				{Function: crewai.ToolCallFunction{Name: "time", Arguments: json.RawMessage(`""`)}},
			}},
			{Content: "The answer is 4 and the time is 12:00"},
		},
	}
	a := crewai.NewAgent("Agent", "goal", "backstory", m)
	a.ToolMode = crewai.ToolModeNative
	a.Tools = []crewai.Tool{calc, clock}
	task := crewai.NewTask("calc and check time", "text", a)
	crew := crewai.NewCrew([]*crewai.Agent{a}, []*crewai.Task{task})

	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(out.TasksOutput[0].ToolTraces) != 2 {
		t.Fatalf("traces = %d, want 2", len(out.TasksOutput[0].ToolTraces))
	}
	if out.TasksOutput[0].ToolTraces[0].Tool != "calc" {
		t.Errorf("trace[0] tool = %q", out.TasksOutput[0].ToolTraces[0].Tool)
	}
	if out.TasksOutput[0].ToolTraces[1].Tool != "time" {
		t.Errorf("trace[1] tool = %q", out.TasksOutput[0].ToolTraces[1].Tool)
	}
}

func TestCrewNativeFactsCollected(t *testing.T) {
	fsTool := crewai.NewFactSourceTool(
		"lookup", "looks up data",
		func(_ context.Context, _ string) (string, error) { return "data123", nil },
		func(_ context.Context, output string) []crewai.Fact {
			return []crewai.Fact{crewai.NewFact(output, "TestOrg", "https://test.com", []byte(output))}
		},
	)
	m := &mock.LLM{
		ModelName: "mock",
		ToolCallResponses: []*crewai.ToolCallResponse{
			{ToolCalls: []crewai.ToolCall{{Function: crewai.ToolCallFunction{Name: "lookup", Arguments: json.RawMessage(`"q"`)}}}},
			{Content: "result"},
		},
	}
	a := crewai.NewAgent("Agent", "goal", "backstory", m)
	a.ToolMode = crewai.ToolModeNative
	a.Tools = []crewai.Tool{fsTool}
	task := crewai.NewTask("lookup data", "text", a)
	crew := crewai.NewCrew([]*crewai.Agent{a}, []*crewai.Task{task})

	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(out.Facts) != 1 {
		t.Fatalf("facts = %d, want 1", len(out.Facts))
	}
	if out.Facts[0].SourceOrg != "TestOrg" {
		t.Errorf("fact SourceOrg = %q", out.Facts[0].SourceOrg)
	}
}

func TestCrewNativeErrNativeToolsUnsupported(t *testing.T) {
	// mock.New() returns an LLM that does implement ToolCallingLLM (with empty queue),
	// so we need a custom LLM that does NOT implement it.
	plainLLM := &nonTCLLM{}
	a := crewai.NewAgent("Agent", "goal", "backstory", plainLLM)
	a.ToolMode = crewai.ToolModeNative
	task := crewai.NewTask("task", "text", a)
	crew := crewai.NewCrew([]*crewai.Agent{a}, []*crewai.Task{task})

	_, err := crew.Kickoff(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, crewai.ErrNativeToolsUnsupported) {
		t.Errorf("err should be ErrNativeToolsUnsupported: %v", err)
	}
	if !strings.Contains(err.Error(), "ToolCallingLLM") {
		t.Errorf("err should mention ToolCallingLLM: %v", err)
	}
}

// TestCrewNativeErrNativeToolsUnsupportedWithTools verifies that the error
// is clear and actionable when a provider without ToolCallingLLM is used
// with ToolModeNative AND tools are configured.
func TestCrewNativeErrNativeToolsUnsupportedWithTools(t *testing.T) {
	plainLLM := &nonTCLLM{}
	tool := crewai.NewTool("calc", "calculator", func(_ context.Context, _ string) (string, error) {
		return "1", nil
	})
	a := crewai.NewAgent("Agent", "goal", "backstory", plainLLM)
	a.ToolMode = crewai.ToolModeNative
	a.Tools = []crewai.Tool{tool}
	task := crewai.NewTask("calc 1+1", "text", a)
	crew := crewai.NewCrew([]*crewai.Agent{a}, []*crewai.Task{task})

	_, err := crew.Kickoff(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, crewai.ErrNativeToolsUnsupported) {
		t.Errorf("err should be ErrNativeToolsUnsupported: %v", err)
	}
	if !strings.Contains(err.Error(), "ToolCallingLLM") {
		t.Errorf("err should mention ToolCallingLLM: %v", err)
	}
}

// TestCrewNativeReActFallbackWhenNoToolMode verifies that the default
// (empty ToolMode) uses ReAct even for LLMs that DO implement ToolCallingLLM.
func TestCrewNativeReActFallbackWhenNoToolMode(t *testing.T) {
	m := mock.New("react answer")
	a := crewai.NewAgent("Agent", "goal", "backstory", m)
	// ToolMode is NOT set — should use ReAct, not native.
	task := crewai.NewTask("task", "text", a)
	crew := crewai.NewCrew([]*crewai.Agent{a}, []*crewai.Task{task})

	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.Final != "react answer" {
		t.Errorf("final = %q", out.Final)
	}
	// ReAct path should not produce tool traces.
	if len(out.TasksOutput[0].ToolTraces) != 0 {
		t.Errorf("traces = %d, want 0 (ReAct)", len(out.TasksOutput[0].ToolTraces))
	}
}

// nonTCLLM implements LLM but NOT ToolCallingLLM.
type nonTCLLM struct{}

func (n *nonTCLLM) Call(_ context.Context, _ []crewai.Message) (string, error) {
	return "ok", nil
}
func (n *nonTCLLM) Model() string { return "non-tcllm" }
