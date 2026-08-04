package crewai_test

import (
	"context"
	"strings"
	"testing"

	"github.com/rhgs/crewai-go"
	"github.com/rhgs/crewai-go/llm/mock"
)

func TestCrewSequential(t *testing.T) {
	llm := mock.New("research result", "final summary")
	researcher := crewai.NewAgent("Researcher", "research", "", llm)
	writer := crewai.NewAgent("Writer", "write", "", llm)

	t1 := crewai.NewTask("Research about Go", "facts", researcher)
	t1.Name = "research"
	t2 := crewai.NewTask("Write a summary", "text", writer).WithContext(t1)

	crew := crewai.NewCrew([]*crewai.Agent{researcher, writer}, []*crewai.Task{t1, t2})
	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("Kickoff error: %v", err)
	}

	if out.Final != "final summary" {
		t.Errorf("Final = %q", out.Final)
	}
	if len(out.TasksOutput) != 2 {
		t.Fatalf("expected 2 outputs, got %d", len(out.TasksOutput))
	}
	if out.TasksOutput[0].Agent != "Researcher" {
		t.Errorf("task 1 agent = %q", out.TasksOutput[0].Agent)
	}
}

func TestCrewContextPropagation(t *testing.T) {
	// The second LLM call must receive, in the prompt, the output of the 1st
	// task.
	var secondPrompt string
	calls := 0
	llm := &mock.LLM{Handler: func(_ context.Context, msgs []crewai.Message) (string, error) {
		calls++
		if calls == 1 {
			return "OUTPUT_ONE", nil
		}
		for _, m := range msgs {
			secondPrompt += m.Content + "\n"
		}
		return "OUTPUT_TWO", nil
	}}

	a := crewai.NewAgent("A", "", "", llm)
	t1 := crewai.NewTask("task 1", "", a)
	t2 := crewai.NewTask("task 2", "", a).WithContext(t1)

	crew := crewai.NewCrew([]*crewai.Agent{a}, []*crewai.Task{t1, t2})
	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatalf("error: %v", err)
	}
	if !strings.Contains(secondPrompt, "OUTPUT_ONE") {
		t.Errorf("context not propagated; 2nd task prompt: %q", secondPrompt)
	}
}

func TestCrewInputsInterpolation(t *testing.T) {
	var prompt string
	llm := &mock.LLM{Handler: func(_ context.Context, msgs []crewai.Message) (string, error) {
		for _, m := range msgs {
			prompt += m.Content
		}
		return "ok", nil
	}}
	a := crewai.NewAgent("A", "", "", llm)
	task := crewai.NewTask("Analyze {company}", "", a)

	crew := crewai.NewCrew([]*crewai.Agent{a}, []*crewai.Task{task})
	_, err := crew.Kickoff(context.Background(), map[string]string{"company": "Acme"})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !strings.Contains(prompt, "Acme") {
		t.Errorf("interpolation failed; prompt = %q", prompt)
	}
}

func TestCrewNoTasks(t *testing.T) {
	crew := crewai.NewCrew(nil, nil)
	if _, err := crew.Kickoff(context.Background(), nil); err != crewai.ErrNoTasks {
		t.Errorf("error = %v, want %v", err, crewai.ErrNoTasks)
	}
}

func TestCrewNoAgentForTask(t *testing.T) {
	task := crewai.NewTask("t", "", nil)
	crew := crewai.NewCrew(nil, []*crewai.Task{task})
	if _, err := crew.Kickoff(context.Background(), nil); err != crewai.ErrNoAgent {
		t.Errorf("error = %v, want %v", err, crewai.ErrNoAgent)
	}
}

func TestCrewMemory(t *testing.T) {
	llm := mock.New("first", "second")
	a := crewai.NewAgent("A", "", "", llm)
	t1 := crewai.NewTask("t1", "", a)
	t1.Name = "t1"
	t2 := crewai.NewTask("t2", "", a)

	crew := crewai.NewCrew([]*crewai.Agent{a}, []*crewai.Task{t1, t2})
	crew.Memory = true
	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatalf("error: %v", err)
	}

	mem := crew.MemorySnapshot()
	if mem == nil {
		t.Fatal("memory nil")
	}
	if len(mem.Records()) != 2 {
		t.Errorf("expected 2 records, got %d", len(mem.Records()))
	}
}

func TestCrewHierarchical(t *testing.T) {
	worker := &mock.LLM{Handler: func(_ context.Context, _ []crewai.Message) (string, error) {
		return "work done", nil
	}}
	// The manager always picks "Specialist".
	manager := &mock.LLM{Handler: func(_ context.Context, _ []crewai.Message) (string, error) {
		return "Specialist", nil
	}}

	generalist := crewai.NewAgent("Generalist", "general", "", worker)
	specialist := crewai.NewAgent("Specialist", "specific", "", worker)

	task := crewai.NewTask("do the work", "", nil) // no agent => delegation

	crew := crewai.NewCrew([]*crewai.Agent{generalist, specialist}, []*crewai.Task{task})
	crew.Process = crewai.Hierarchical
	crew.ManagerLLM = manager

	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if out.TasksOutput[0].Agent != "Specialist" {
		t.Errorf("wrong delegation: %q", out.TasksOutput[0].Agent)
	}
}

func TestCrewHierarchicalNoManager(t *testing.T) {
	a := crewai.NewAgent("A", "", "", mock.New("x"))
	task := crewai.NewTask("t", "", nil)
	crew := crewai.NewCrew([]*crewai.Agent{a}, []*crewai.Task{task})
	crew.Process = crewai.Hierarchical
	if _, err := crew.Kickoff(context.Background(), nil); err != crewai.ErrNoManager {
		t.Errorf("error = %v, want %v", err, crewai.ErrNoManager)
	}
}

func TestCrewInvalidProcess(t *testing.T) {
	a := crewai.NewAgent("A", "", "", mock.New("x"))
	task := crewai.NewTask("t", "", a)
	crew := crewai.NewCrew([]*crewai.Agent{a}, []*crewai.Task{task})
	crew.Process = "parallel"
	if _, err := crew.Kickoff(context.Background(), nil); err == nil {
		t.Error("expected an invalid-process error")
	}
}
