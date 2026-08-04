package mock_test

import (
	"context"
	"testing"

	"github.com/rhgs/crewai-go"
	"github.com/rhgs/crewai-go/llm/mock"
)

func TestMockSequential(t *testing.T) {
	m := mock.New("um", "dois")
	ctx := context.Background()

	if r, _ := m.Call(ctx, nil); r != "um" {
		t.Errorf("1ª = %q", r)
	}
	if r, _ := m.Call(ctx, nil); r != "dois" {
		t.Errorf("2ª = %q", r)
	}
	// Esgotada, repete a última.
	if r, _ := m.Call(ctx, nil); r != "dois" {
		t.Errorf("3ª = %q", r)
	}
	if m.Calls() != 3 {
		t.Errorf("Calls() = %d", m.Calls())
	}
}

func TestMockHandler(t *testing.T) {
	m := &mock.LLM{Handler: func(_ context.Context, msgs []crewai.Message) (string, error) {
		return "recebi " + msgs[0].Content, nil
	}}
	r, err := m.Call(context.Background(), []crewai.Message{crewai.UserMessage("oi")})
	if err != nil {
		t.Fatal(err)
	}
	if r != "recebi oi" {
		t.Errorf("r = %q", r)
	}
	if len(m.LastMessages()) != 1 {
		t.Errorf("LastMessages len = %d", len(m.LastMessages()))
	}
}

func TestMockModel(t *testing.T) {
	if mock.New().Model() != "mock" {
		t.Error("Model padrão deveria ser 'mock'")
	}
	m := &mock.LLM{ModelName: "custom"}
	if m.Model() != "custom" {
		t.Errorf("Model = %q", m.Model())
	}
}
