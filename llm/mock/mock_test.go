package mock_test

import (
	"context"
	"testing"

	"github.com/rhgs/crewai-go"
	"github.com/rhgs/crewai-go/llm/mock"
)

func TestMockSequential(t *testing.T) {
	m := mock.New("one", "two")
	ctx := context.Background()

	if r, _ := m.Call(ctx, nil); r != "one" {
		t.Errorf("1st = %q", r)
	}
	if r, _ := m.Call(ctx, nil); r != "two" {
		t.Errorf("2nd = %q", r)
	}
	// Exhausted, repeats the last one.
	if r, _ := m.Call(ctx, nil); r != "two" {
		t.Errorf("3rd = %q", r)
	}
	if m.Calls() != 3 {
		t.Errorf("Calls() = %d", m.Calls())
	}
}

func TestMockHandler(t *testing.T) {
	m := &mock.LLM{Handler: func(_ context.Context, msgs []crewai.Message) (string, error) {
		return "received " + msgs[0].Content, nil
	}}
	r, err := m.Call(context.Background(), []crewai.Message{crewai.UserMessage("hi")})
	if err != nil {
		t.Fatal(err)
	}
	if r != "received hi" {
		t.Errorf("r = %q", r)
	}
	if len(m.LastMessages()) != 1 {
		t.Errorf("LastMessages len = %d", len(m.LastMessages()))
	}
}

func TestMockModel(t *testing.T) {
	if mock.New().Model() != "mock" {
		t.Error("default Model should be 'mock'")
	}
	m := &mock.LLM{ModelName: "custom"}
	if m.Model() != "custom" {
		t.Errorf("Model = %q", m.Model())
	}
}

func TestMockCallWithTools_Queue(t *testing.T) {
	m := &mock.LLM{
		ModelName: "mock",
		ToolCallResponses: []*crewai.ToolCallResponse{
			{Content: "", ToolCalls: []crewai.ToolCall{{Function: crewai.ToolCallFunction{Name: "calc", Arguments: []byte(`"1+1"`)}}}},
			{Content: "the answer is 2", ToolCalls: nil},
		},
	}
	ctx := context.Background()

	// First call returns tool calls.
	resp1, err := m.CallWithTools(ctx, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp1.ToolCalls) != 1 {
		t.Fatalf("resp1 toolCalls = %d, want 1", len(resp1.ToolCalls))
	}
	if resp1.ToolCalls[0].Function.Name != "calc" {
		t.Errorf("toolCall name = %q", resp1.ToolCalls[0].Function.Name)
	}

	// Second call returns final answer.
	resp2, err := m.CallWithTools(ctx, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp2.Content != "the answer is 2" {
		t.Errorf("resp2 content = %q", resp2.Content)
	}
	if len(resp2.ToolCalls) != 0 {
		t.Errorf("resp2 toolCalls = %d, want 0", len(resp2.ToolCalls))
	}
}

func TestMockCallWithTools_EmptyQueue(t *testing.T) {
	m := mock.New()
	resp, err := m.CallWithTools(context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp == nil {
		t.Fatal("resp should not be nil")
	}
	if resp.Content != "" {
		t.Errorf("content = %q, want empty", resp.Content)
	}
	if len(resp.ToolCalls) != 0 {
		t.Errorf("toolCalls = %d, want 0", len(resp.ToolCalls))
	}
}

func TestMockCallWithTools_CallsIncrement(t *testing.T) {
	m := &mock.LLM{
		ModelName: "mock",
		ToolCallResponses: []*crewai.ToolCallResponse{
			{Content: "done"},
		},
	}
	m.CallWithTools(context.Background(), nil, nil)
	if m.Calls() != 1 {
		t.Errorf("Calls = %d, want 1", m.Calls())
	}
}
