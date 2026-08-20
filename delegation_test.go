package crewai

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
)

type fixedRoleLLM struct {
	out   string
	calls int32
}

func (f *fixedRoleLLM) Call(ctx context.Context, _ []Message) (string, error) {
	atomic.AddInt32(&f.calls, 1)
	return f.out, nil
}
func (f *fixedRoleLLM) Model() string { return "fixed-role" }

type sliceRoster []*Agent

func (s sliceRoster) PeerAgents() []*Agent { return []*Agent(s) }

func TestDelegationTool_HappyPath(t *testing.T) {
	coworkerLLM := &fixedRoleLLM{out: "Final Answer: peer answer"}
	coworker := NewAgent("Researcher", "research", "", coworkerLLM)
	coworker.AllowDelegation = true

	callerLLM := &fixedRoleLLM{out: "unused"}
	caller := NewAgent("Writer", "write", "", callerLLM)

	crew := NewCrew([]*Agent{caller, coworker}, nil)
	tool := NewDelegationTool(crew)

	ctx := ContextWithAgentRole(context.Background(), caller.Role)
	in, _ := json.Marshal(map[string]string{
		"coworker": "Researcher",
		"request":  "Summarize X",
		"context":  "extra",
	})
	out, err := tool.Call(ctx, string(in))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "peer answer") {
		t.Fatalf("out=%q", out)
	}
	if atomic.LoadInt32(&coworkerLLM.calls) != 1 {
		t.Fatalf("coworker calls=%d", coworkerLLM.calls)
	}
}

func TestDelegationTool_UnknownCoworker(t *testing.T) {
	a := NewAgent("A", "g", "", &fixedRoleLLM{out: "x"})
	a.AllowDelegation = true
	tool := NewDelegationTool(sliceRoster{a})
	ctx := ContextWithAgentRole(context.Background(), "Other")
	out, err := tool.Call(ctx, `{"coworker":"Missing","request":"hi"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "not found") {
		t.Fatalf("out=%q", out)
	}
}

func TestDelegationTool_TargetNotEligible(t *testing.T) {
	target := NewAgent("B", "g", "", &fixedRoleLLM{out: "Final Answer: nope"})
	// AllowDelegation false
	caller := NewAgent("A", "g", "", &fixedRoleLLM{out: "x"})
	tool := NewDelegationTool(sliceRoster{caller, target})
	ctx := ContextWithAgentRole(context.Background(), "A")
	out, err := tool.Call(ctx, `{"coworker":"B","request":"hi"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "not eligible") {
		t.Fatalf("out=%q", out)
	}
}

func TestDelegationTool_SelfDelegate(t *testing.T) {
	a := NewAgent("Solo", "g", "", &fixedRoleLLM{out: "Final Answer: me"})
	a.AllowDelegation = true
	tool := NewDelegationTool(sliceRoster{a})
	ctx := ContextWithAgentRole(context.Background(), "Solo")
	out, err := tool.Call(ctx, `{"coworker":"Solo","request":"hi"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "yourself") {
		t.Fatalf("out=%q", out)
	}
}

func TestDelegationTool_DepthLimit(t *testing.T) {
	// Build a chain A -> B -> C where each always tries to delegate further.
	// We test the tool depth guard directly by pre-seeding depth.
	c := NewAgent("C", "g", "", &fixedRoleLLM{out: "Final Answer: leaf"})
	c.AllowDelegation = true
	tool := NewDelegationTool(sliceRoster{c})
	ctx := ContextWithAgentRole(context.Background(), "B")
	ctx = withDelegationDepth(ctx, DefaultMaxDelegationDepth)
	out, err := tool.Call(ctx, `{"coworker":"C","request":"go"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "depth limit") {
		t.Fatalf("out=%q", out)
	}
}

func TestDelegationTool_Cycle(t *testing.T) {
	b := NewAgent("B", "g", "", &fixedRoleLLM{out: "Final Answer: b"})
	b.AllowDelegation = true
	tool := NewDelegationTool(sliceRoster{b})
	// Stack already contains B (A called B before, now B tries A... we simulate
	// B calling B again via stack containing B).
	ctx := ContextWithAgentRole(context.Background(), "A")
	ctx = withDelegationStack(ctx, []string{"B"})
	out, err := tool.Call(ctx, `{"coworker":"B","request":"loop"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "cycle") {
		t.Fatalf("out=%q", out)
	}
}

func TestDelegationTool_InvalidJSON(t *testing.T) {
	tool := NewDelegationTool(sliceRoster{})
	out, err := tool.Call(context.Background(), "not-json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "invalid delegation JSON") {
		t.Fatalf("out=%q", out)
	}
}

func TestDelegationTool_NilRoster(t *testing.T) {
	tool := NewDelegationTool(nil)
	out, err := tool.Call(context.Background(), `{"coworker":"A","request":"x"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "roster") {
		t.Fatalf("out=%q", out)
	}
}

func TestCrew_EnableDelegationTool_AttachesOnce(t *testing.T) {
	llm := &fixedRoleLLM{out: "Final Answer: done"}
	a := NewAgent("A", "g", "", llm)
	a.AllowDelegation = true
	b := NewAgent("B", "g", "", llm)
	b.AllowDelegation = true
	task := NewTask("say hi", "hi", a)
	crew := NewCrew([]*Agent{a, b}, []*Task{task})
	crew.EnableDelegationTool = true

	// Kickoff should attach tools
	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if !hasDelegationTool(a.Tools) {
		t.Fatal("A missing delegation tool")
	}
	if !hasDelegationTool(b.Tools) {
		t.Fatal("B missing delegation tool")
	}
	// Second kickoff must not duplicate
	n1 := len(a.Tools)
	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if len(a.Tools) != n1 {
		t.Fatalf("tools duplicated: %d -> %d", n1, len(a.Tools))
	}
}

func TestCrew_PeerAgents(t *testing.T) {
	a := NewAgent("A", "g", "", &fixedRoleLLM{out: "x"})
	c := NewCrew([]*Agent{a}, nil)
	if len(c.PeerAgents()) != 1 || c.PeerAgents()[0] != a {
		t.Fatal("PeerAgents")
	}
	var r DelegationRoster = c
	if len(r.PeerAgents()) != 1 {
		t.Fatal("roster")
	}
}

func TestHasDelegationTool(t *testing.T) {
	if hasDelegationTool(nil) {
		t.Fatal("nil")
	}
	t1 := NewDelegationTool(sliceRoster{})
	if !hasDelegationTool([]Tool{t1}) {
		t.Fatal("expected true")
	}
}
