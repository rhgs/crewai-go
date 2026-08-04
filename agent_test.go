package crewai

import (
	"context"
	"strings"
	"testing"
)

func TestNewAgent(t *testing.T) {
	llm := &llmStub{responses: []string{"ok"}}
	a := NewAgent("Role", "Goal", "Backstory", llm)
	if a.Role != "Role" || a.Goal != "Goal" || a.Backstory != "Backstory" {
		t.Errorf("incorrect fields: %+v", a)
	}
	if a.LLM != llm {
		t.Error("LLM not assigned")
	}
}

func TestAgentWithTools(t *testing.T) {
	a := NewAgent("A", "", "", nil)
	tool := NewTool("t", "", nil)
	a.WithTools(tool)
	if len(a.Tools) != 1 || a.Tools[0] != tool {
		t.Error("WithTools did not add the tool")
	}
}

func TestAgentExecute(t *testing.T) {
	a := NewAgent("Writer", "write", "", &llmStub{responses: []string{"generated text"}})
	task := NewTask("write", "", a)
	out, err := a.Execute(context.Background(), task)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if out != "generated text" {
		t.Errorf("out = %q", out)
	}
}

func TestBuildSystemPrompt(t *testing.T) {
	a := NewAgent("Researcher", "find the truth", "You are curious.", nil)
	s := buildSystemPrompt(a)
	if !strings.Contains(s, "Researcher") || !strings.Contains(s, "find the truth") || !strings.Contains(s, "curious") {
		t.Errorf("incomplete system prompt: %q", s)
	}
}

func TestBuildToolInstructions(t *testing.T) {
	tools := []Tool{NewTool("search", "web search", nil)}
	s := buildToolInstructions(tools)
	if !strings.Contains(s, "search") || !strings.Contains(s, "Action:") || !strings.Contains(s, "Final Answer:") {
		t.Errorf("incomplete tool instructions: %q", s)
	}
	if buildToolInstructions(nil) != "" {
		t.Error("no tools should return an empty string")
	}
}
