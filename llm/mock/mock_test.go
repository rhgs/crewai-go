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
