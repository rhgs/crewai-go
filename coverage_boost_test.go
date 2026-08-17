package crewai_test

import (
	"testing"

	"github.com/rhgs/crewai-go"
	"github.com/rhgs/crewai-go/llm/mock"
)

func TestCrewOutputString(t *testing.T) {
	out := &crewai.CrewOutput{Final: "hello"}
	if out.String() != "hello" {
		t.Fatalf("String = %q", out.String())
	}
}

func TestMockEmptyModelAndLastMessages(t *testing.T) {
	m := &mock.LLM{} // ModelName empty -> default "mock"
	if m.Model() != "mock" {
		t.Fatalf("Model = %q", m.Model())
	}
	if m.LastMessages() != nil {
		t.Fatalf("LastMessages before call should be nil")
	}
}
