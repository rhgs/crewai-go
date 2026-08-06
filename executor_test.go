package crewai

import (
	"context"
	"strings"
	"testing"
)

func TestParseFinalAnswer(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"Thought: done\nFinal Answer: 42", "42", true},
		{"FINAL ANSWER: uppercase", "uppercase", true},
		{"Final Answer:  spaces  ", "spaces", true},
		{"no marker", "", false},
		{"Action: x", "", false},
	}
	for _, c := range cases {
		got, ok := parseFinalAnswer(c.in)
		if ok != c.ok {
			t.Errorf("parseFinalAnswer(%q) ok = %v, want %v", c.in, ok, c.ok)
			continue
		}
		if ok && strings.TrimSpace(got) != c.want {
			t.Errorf("parseFinalAnswer(%q) = %q, want %q", c.in, strings.TrimSpace(got), c.want)
		}
	}
}

func TestParseAction(t *testing.T) {
	in := "Thought: need to calculate\nAction: calculator\nAction Input: 2 + 2"
	action, input, ok := parseAction(in)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if action != "calculator" {
		t.Errorf("action = %q, want %q", action, "calculator")
	}
	if input != "2 + 2" {
		t.Errorf("input = %q, want %q", input, "2 + 2")
	}
}

func TestParseActionStopsAtObservation(t *testing.T) {
	in := "Action: search\nAction Input: golang\nObservation: something old"
	_, input, ok := parseAction(in)
	if !ok {
		t.Fatal("expected ok")
	}
	if input != "golang" {
		t.Errorf("input = %q, want %q", input, "golang")
	}
}

func TestParseActionNoAction(t *testing.T) {
	if _, _, ok := parseAction("just text"); ok {
		t.Error("should not find an action")
	}
}

// llmStub is a minimal local LLM to avoid a dependency on the mock package
// here.
type llmStub struct {
	responses []string
	i         int
}

func (s *llmStub) Call(_ context.Context, _ []Message) (string, error) {
	r := s.responses[s.i]
	if s.i < len(s.responses)-1 {
		s.i++
	}
	return r, nil
}
func (s *llmStub) Model() string { return "stub" }

func TestExecuteTaskNoTools(t *testing.T) {
	agent := NewAgent("Writer", "write", "", &llmStub{responses: []string{"A poem."}})
	task := NewTask("Write a poem", "a short poem", agent)

	out, _, err := executeTask(context.Background(), agent, task, "", nopLogger{})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if out != "A poem." {
		t.Errorf("out = %q", out)
	}
}

func TestExecuteTaskWithTool(t *testing.T) {
	llm := &llmStub{responses: []string{
		"Thought: I'll add\nAction: calc\nAction Input: 2+2",
		"Thought: done\nFinal Answer: The result is 4.",
	}}
	agent := NewAgent("Mathematician", "calculate", "", llm)
	agent.WithTools(NewTool("calc", "add", func(_ context.Context, in string) (string, error) {
		return "4", nil
	}))
	task := NewTask("What is 2+2?", "the number", agent)

	out, _, err := executeTask(context.Background(), agent, task, "", nopLogger{})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !strings.Contains(out, "4") {
		t.Errorf("out = %q, expected to contain 4", out)
	}
}

func TestExecuteTaskUnknownToolRecovers(t *testing.T) {
	llm := &llmStub{responses: []string{
		"Action: nonexistent\nAction Input: x",
		"Final Answer: recovered",
	}}
	agent := NewAgent("A", "", "", llm)
	agent.WithTools(NewTool("real", "", func(_ context.Context, _ string) (string, error) { return "", nil }))
	task := NewTask("do something", "", agent)

	out, _, err := executeTask(context.Background(), agent, task, "", nopLogger{})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if out != "recovered" {
		t.Errorf("out = %q", out)
	}
}

func TestExecuteTaskNoLLM(t *testing.T) {
	agent := &Agent{Role: "X"}
	task := NewTask("t", "", agent)
	if _, _, err := executeTask(context.Background(), agent, task, "", nopLogger{}); err != ErrNoLLM {
		t.Errorf("error = %v, want %v", err, ErrNoLLM)
	}
}

func TestExecuteTaskMaxIterations(t *testing.T) {
	// Always asks for a tool, never gives a Final Answer.
	llm := &llmStub{responses: []string{"Action: loop\nAction Input: x"}}
	agent := &Agent{Role: "L", LLM: llm, MaxIterations: 3}
	agent.WithTools(NewTool("loop", "", func(_ context.Context, _ string) (string, error) { return "ok", nil }))
	task := NewTask("loop", "", agent)

	_, _, err := executeTask(context.Background(), agent, task, "", nopLogger{})
	if err == nil || !strings.Contains(err.Error(), "iterations") {
		t.Errorf("expected a max iterations error, got %v", err)
	}
}

func TestExecuteTaskContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	llm := &llmStub{responses: []string{"Action: t\nAction Input: x"}}
	agent := &Agent{Role: "C", LLM: llm}
	agent.WithTools(NewTool("t", "", func(_ context.Context, _ string) (string, error) { return "", nil }))
	task := NewTask("t", "", agent)

	if _, _, err := executeTask(ctx, agent, task, "", nopLogger{}); err != context.Canceled {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}
