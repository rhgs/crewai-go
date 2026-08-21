package crewai

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

type eventCollector struct {
	mu     sync.Mutex
	events []CrewEvent
}

func (c *eventCollector) Collect(ev CrewEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, ev)
}

func (c *eventCollector) Types() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.events))
	for i, e := range c.events {
		out[i] = e.Type
	}
	return out
}

func (c *eventCollector) Has(typ string) bool {
	for _, t := range c.Types() {
		if t == typ {
			return true
		}
	}
	return false
}

func TestEvents_KickoffAndTask(t *testing.T) {
	coll := &eventCollector{}
	llm := &callOnlyLLM{out: "Final Answer: done"}
	agent := NewAgent("A", "g", "b", llm)
	task := NewTask("work", "out", agent)
	task.Name = "t1"
	crew := NewCrew([]*Agent{agent}, []*Task{task}).WithEvents(coll.Collect)
	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if !coll.Has(EventKickoffStarted) || !coll.Has(EventKickoffCompleted) {
		t.Fatalf("types %v", coll.Types())
	}
	if !coll.Has(EventTaskStarted) || !coll.Has(EventTaskCompleted) {
		t.Fatalf("missing task events %v", coll.Types())
	}
	if !coll.Has(EventLLMCallStarted) || !coll.Has(EventLLMCallCompleted) {
		t.Fatalf("missing llm events %v", coll.Types())
	}
	// KickoffID stable
	coll.mu.Lock()
	id := coll.events[0].KickoffID
	for _, e := range coll.events {
		if e.KickoffID != id || id == "" {
			t.Fatalf("kickoff id mismatch %#v", e)
		}
	}
	coll.mu.Unlock()
}

func TestEvents_ReactIteration(t *testing.T) {
	coll := &eventCollector{}
	llm := &toolReactLLM{}
	agent := NewAgent("A", "g", "b", llm)
	agent.Tools = []Tool{&progressTool{}}
	task := NewTask("t", "x", agent)
	crew := NewCrew([]*Agent{agent}, []*Task{task}).WithEvents(coll.Collect)
	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if !coll.Has(EventReactIteration) {
		t.Fatalf("types %v", coll.Types())
	}
	if !coll.Has(EventToolInvoked) {
		t.Fatalf("expected tool_invoked via progress dual-emit: %v", coll.Types())
	}
}

func TestEvents_PanicRecovered(t *testing.T) {
	llm := &callOnlyLLM{out: "Final Answer: ok"}
	agent := NewAgent("A", "g", "b", llm)
	task := NewTask("t", "x", agent)
	crew := NewCrew([]*Agent{agent}, []*Task{task}).WithEvents(func(CrewEvent) {
		panic("boom")
	})
	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
}

func TestEvents_DualEmitProgress(t *testing.T) {
	var prog int
	var ev int
	llm := &callOnlyLLM{out: "Final Answer: x"}
	agent := NewAgent("A", "g", "b", llm)
	task := NewTask("t", "x", agent)
	crew := NewCrew([]*Agent{agent}, []*Task{task}).
		WithProgress(func(Progress) { prog++ }).
		WithEvents(func(CrewEvent) { ev++ })
	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if prog == 0 || ev == 0 {
		t.Fatalf("prog=%d ev=%d", prog, ev)
	}
}

func TestEvents_GuardrailBlocked(t *testing.T) {
	coll := &eventCollector{}
	llm := &callOnlyLLM{out: "Final Answer: bad"}
	agent := NewAgent("A", "g", "b", llm)
	task := NewTask("t", "x", agent)
	crew := NewCrew([]*Agent{agent}, []*Task{task}).WithEvents(coll.Collect)
	crew.Guardrails = []Guardrail{func(ctx context.Context, out *CrewOutput) error {
		return errors.New("nope")
	}}
	_, err := crew.Kickoff(context.Background(), nil)
	if err == nil {
		t.Fatal("expected guardrail error")
	}
	if !coll.Has(EventGuardrailBlocked) {
		t.Fatalf("types %v", coll.Types())
	}
}

func TestProgressAsEvents(t *testing.T) {
	var got CrewEvent
	fn := ProgressAsEvents(func(e CrewEvent) { got = e })
	fn(Progress{Event: "task_started", Task: "T", Agent: "A"})
	if got.Type != "task_started" || got.Task != "T" {
		t.Fatalf("%#v", got)
	}
}

func TestEmitEvent_NilNoop(t *testing.T) {
	emitEvent(context.Background(), CrewEvent{Type: "x"})
}

func TestNewKickoffID_Unique(t *testing.T) {
	a, b := newKickoffID(), newKickoffID()
	if a == "" || a == b {
		t.Fatalf("%q %q", a, b)
	}
	if len(a) != 32 {
		t.Fatalf("len %d", len(a))
	}
}

func TestEventLogger_NoPanic(t *testing.T) {
	EventLogger(nil)(CrewEvent{Type: EventKickoffStarted})
}

func TestEvents_AsyncWave(t *testing.T) {
	coll := &eventCollector{}
	a1 := NewAgent("A1", "g", "b", &callOnlyLLM{out: "Final Answer: a"})
	a2 := NewAgent("A2", "g", "b", &callOnlyLLM{out: "Final Answer: b"})
	t1 := NewTask("do a", "x", a1).WithAsync()
	t1.Name = "ta"
	t2 := NewTask("do b", "y", a2).WithAsync()
	t2.Name = "tb"
	crew := NewCrew([]*Agent{a1, a2}, []*Task{t1, t2}).WithEvents(coll.Collect)
	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if !coll.Has(EventWaveStarted) || !coll.Has(EventWaveCompleted) {
		t.Fatalf("types %v", coll.Types())
	}
}

// ensure callOnlyLLM available - defined in stream_test.go same package
var _ = strings.Contains
