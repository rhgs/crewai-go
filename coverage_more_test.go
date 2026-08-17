package crewai_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rhgs/crewai-go"
	"github.com/rhgs/crewai-go/llm/mock"
)

func TestHierarchicalDelegatePaths(t *testing.T) {
	// single agent short-circuit
	llm := mock.New("Final Answer: only")
	a := crewai.NewAgent("Solo", "g", "b", llm)
	task := crewai.NewTask("do", "out", nil) // agent filled by crew
	crew := crewai.NewCrew([]*crewai.Agent{a}, []*crewai.Task{task})
	crew.Process = crewai.Hierarchical
	crew.ManagerLLM = mock.New("ignored")
	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Final != "only" {
		t.Fatalf("Final = %q", out.Final)
	}

	// multi agent: exact role match
	a1 := crewai.NewAgent("Writer", "write", "b", mock.New("Final Answer: wrote"))
	a2 := crewai.NewAgent("Researcher", "research", "b", mock.New("Final Answer: researched"))
	mgr := mock.New("Writer")
	task2 := crewai.NewTask("compose", "text", nil)
	crew2 := crewai.NewCrew([]*crewai.Agent{a1, a2}, []*crewai.Task{task2})
	crew2.Process = crewai.Hierarchical
	crew2.ManagerLLM = mgr
	out, err = crew2.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Final != "wrote" {
		t.Fatalf("Final = %q", out.Final)
	}

	// substring match
	a3 := crewai.NewAgent("Analyst", "a", "b", mock.New("Final Answer: analyzed"))
	a4 := crewai.NewAgent("Editor", "e", "b", mock.New("Final Answer: edited"))
	crew3 := crewai.NewCrew([]*crewai.Agent{a3, a4}, []*crewai.Task{crewai.NewTask("t", "o", nil)})
	crew3.Process = crewai.Hierarchical
	crew3.ManagerLLM = mock.New("I pick the Analyst please")
	out, err = crew3.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Final != "analyzed" {
		t.Fatalf("Final = %q", out.Final)
	}

	// manager LLM error -> first agent
	fail := &mock.LLM{Handler: func(context.Context, []crewai.Message) (string, error) {
		return "", errors.New("manager down")
	}}
	crew4 := crewai.NewCrew([]*crewai.Agent{a3, a4}, []*crewai.Task{crewai.NewTask("t", "o", nil)})
	crew4.Process = crewai.Hierarchical
	crew4.ManagerLLM = fail
	out, err = crew4.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Final != "analyzed" { // first agent
		t.Fatalf("Final = %q", out.Final)
	}

	// unknown role -> first agent
	crew5 := crewai.NewCrew([]*crewai.Agent{a3, a4}, []*crewai.Task{crewai.NewTask("t", "o", nil)})
	crew5.Process = crewai.Hierarchical
	crew5.ManagerLLM = mock.New("Nobody")
	out, err = crew5.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Final != "analyzed" {
		t.Fatalf("Final = %q", out.Final)
	}

	// ManagerAgent path
	manager := crewai.NewAgent("Boss", "manage", "b", mock.New("Editor"))
	crew6 := crewai.NewCrew([]*crewai.Agent{a3, a4}, []*crewai.Task{crewai.NewTask("t", "o", nil)})
	crew6.Process = crewai.Hierarchical
	crew6.ManagerAgent = manager
	out, err = crew6.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Final != "edited" {
		t.Fatalf("Final = %q", out.Final)
	}
}

func TestAgentForIndexViaSequentialNilAgentsOnLaterTasks(t *testing.T) {
	// More tasks than agents: later tasks use last agent.
	a := crewai.NewAgent("A", "g", "b", mock.New(
		"Final Answer: one",
		"Final Answer: two",
	))
	t1 := crewai.NewTask("t1", "o1", nil)
	t2 := crewai.NewTask("t2", "o2", nil)
	crew := crewai.NewCrew([]*crewai.Agent{a}, []*crewai.Task{t1, t2})
	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Final != "two" {
		t.Fatalf("Final = %q", out.Final)
	}
}

func TestTaskToolsOverrideAgentTools(t *testing.T) {
	// effectiveTools: task tools replace agent tools.
	var used string
	agentTool := crewai.NewTool("agent_tool", "a", func(context.Context, string) (string, error) {
		used = "agent"
		return "a", nil
	})
	taskTool := crewai.NewTool("task_tool", "t", func(context.Context, string) (string, error) {
		used = "task"
		return "t", nil
	})
	llm := &mock.LLM{Handler: func(_ context.Context, msgs []crewai.Message) (string, error) {
		// first call with tools: take action
		last := msgs[len(msgs)-1].Content
		if strings.Contains(last, "Observation:") {
			return "Final Answer: done", nil
		}
		return "Action: task_tool\nAction Input: x", nil
	}}
	a := crewai.NewAgent("A", "g", "b", llm).WithTools(agentTool)
	task := crewai.NewTask("work", "out", a)
	task.Tools = []crewai.Tool{taskTool}
	if _, err := a.Execute(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if used != "task" {
		t.Fatalf("used = %q, want task", used)
	}
}

func TestParseActionEdgesViaExecute(t *testing.T) {
	// Action without Action Input, then final.
	llm := mock.New(
		"Action: missing_tool\n",
		"Final Answer: recovered",
	)
	a := crewai.NewAgent("A", "g", "b", llm).WithTools(
		crewai.NewTool("real", "r", func(context.Context, string) (string, error) { return "ok", nil }),
	)
	task := crewai.NewTask("t", "o", a)
	out, err := a.Execute(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	if out != "recovered" {
		t.Fatalf("out = %q", out)
	}
}
