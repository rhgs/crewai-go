package crewai

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type flowState struct {
	mu   sync.Mutex
	log  []string
	n    int
	path string
}

func (s *flowState) add(name string) {
	s.mu.Lock()
	s.log = append(s.log, name)
	s.n++
	s.mu.Unlock()
}

func TestFlow_LinearAndStart(t *testing.T) {
	f := NewFlow[flowState]().
		Start("a", func(_ context.Context, s *flowState) error {
			s.add("a")
			return nil
		}).
		Listen("b", func(_ context.Context, s *flowState) error {
			s.add("b")
			return nil
		}, "a")
	res, err := f.Run(context.Background(), flowState{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(res.State.log, ",") != "a,b" {
		t.Fatalf("%v", res.State.log)
	}
	if len(res.Steps) != 2 || res.Steps[0].Name != "a" || res.Steps[1].Name != "b" {
		t.Fatalf("%v", res.Steps)
	}
}

func TestFlow_ZeroDepIsStart(t *testing.T) {
	f := NewFlow[flowState]().
		Listen("boot", func(_ context.Context, s *flowState) error {
			s.add("boot")
			return nil
		})
	res, err := f.Run(context.Background(), flowState{})
	if err != nil {
		t.Fatal(err)
	}
	if res.State.log[0] != "boot" {
		t.Fatal(res.State.log)
	}
}

func TestFlow_StartWithDepsRejected(t *testing.T) {
	f := NewFlow[flowState]()
	f.nodes = append(f.nodes, flowNode[flowState]{
		name: "x", start: true, deps: []string{"y"}, kind: kindListen,
		listen: func(context.Context, *flowState) error { return nil },
	})
	f.nodes = append(f.nodes, flowNode[flowState]{
		name: "y", kind: kindListen,
		listen: func(context.Context, *flowState) error { return nil },
	})
	if _, err := f.Run(context.Background(), flowState{}); !errors.Is(err, ErrFlowStartDeps) {
		t.Fatalf("%v", err)
	}
}

func TestFlow_DuplicateAndUnknownAndCycle(t *testing.T) {
	dup := NewFlow[flowState]().
		Start("a", func(context.Context, *flowState) error { return nil }).
		Listen("a", func(context.Context, *flowState) error { return nil })
	if _, err := dup.Run(context.Background(), flowState{}); !errors.Is(err, ErrFlowDuplicateStep) {
		t.Fatalf("dup %v", err)
	}
	unk := NewFlow[flowState]().
		Start("a", func(context.Context, *flowState) error { return nil }).
		Listen("b", func(context.Context, *flowState) error { return nil }, "missing")
	if _, err := unk.Run(context.Background(), flowState{}); !errors.Is(err, ErrFlowUnknownStep) {
		t.Fatalf("unk %v", err)
	}
	cyc := NewFlow[flowState]().
		Start("boot", func(context.Context, *flowState) error { return nil }).
		Listen("a", func(context.Context, *flowState) error { return nil }, "b").
		Listen("b", func(context.Context, *flowState) error { return nil }, "a")
	if _, err := cyc.Run(context.Background(), flowState{}); !errors.Is(err, ErrFlowCycle) {
		t.Fatalf("cyc %v", err)
	}
	self := NewFlow[flowState]().
		Start("boot", func(context.Context, *flowState) error { return nil }).
		Listen("a", func(context.Context, *flowState) error { return nil }, "a")
	if _, err := self.Run(context.Background(), flowState{}); !errors.Is(err, ErrFlowCycle) {
		t.Fatalf("self %v", err)
	}
}

func TestFlow_NoStart(t *testing.T) {
	f := NewFlow[flowState]()
	if _, err := f.Run(context.Background(), flowState{}); !errors.Is(err, ErrFlowNoStart) {
		t.Fatalf("%v", err)
	}
}

func TestFlow_EmptyNameAndNilFn(t *testing.T) {
	f := NewFlow[flowState]()
	f.nodes = append(f.nodes, flowNode[flowState]{name: "", kind: kindListen, listen: func(context.Context, *flowState) error { return nil }})
	if _, err := f.Run(context.Background(), flowState{}); !errors.Is(err, ErrFlowUnknownStep) {
		t.Fatalf("empty %v", err)
	}
	f2 := NewFlow[flowState]()
	f2.nodes = append(f2.nodes, flowNode[flowState]{name: "a", kind: kindListen})
	if _, err := f2.Run(context.Background(), flowState{}); err == nil {
		t.Fatal("nil listen")
	}
	f3 := NewFlow[flowState]()
	f3.nodes = append(f3.nodes, flowNode[flowState]{name: "r", kind: kindRouter})
	if _, err := f3.Run(context.Background(), flowState{}); err == nil {
		t.Fatal("nil router")
	}
}

func TestFlow_ParallelFoldByRegistration(t *testing.T) {
	var order []string
	var mu sync.Mutex
	f := NewFlow[flowState]().
		Start("boot", func(context.Context, *flowState) error { return nil }).
		Listen("slow", func(context.Context, *flowState) error {
			time.Sleep(30 * time.Millisecond)
			mu.Lock()
			order = append(order, "slow")
			mu.Unlock()
			return nil
		}, "boot").
		Listen("fast", func(context.Context, *flowState) error {
			mu.Lock()
			order = append(order, "fast")
			mu.Unlock()
			return nil
		}, "boot")
	res, err := f.Run(context.Background(), flowState{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Steps) != 3 || res.Steps[1].Name != "slow" || res.Steps[2].Name != "fast" {
		t.Fatalf("fold %v", names(res.Steps))
	}
	if len(order) != 2 {
		t.Fatalf("ran %v", order)
	}
}

func TestFlow_FailFastDefault(t *testing.T) {
	f := NewFlow[flowState]().
		Start("a", func(_ context.Context, s *flowState) error {
			s.add("a")
			return errors.New("boom")
		}).
		Listen("b", func(_ context.Context, s *flowState) error {
			s.add("b")
			return nil
		}, "a")
	res, err := f.Run(context.Background(), flowState{})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("%v", err)
	}
	if strings.Join(res.State.log, ",") != "a" {
		t.Fatalf("b must not run: %v", res.State.log)
	}
}

func TestFlow_ContinueOnError(t *testing.T) {
	f := NewFlow[flowState]().WithFlowContinueOnError().
		Start("a", func(_ context.Context, s *flowState) error {
			s.add("a")
			return errors.New("boom")
		}).
		Listen("b", func(_ context.Context, s *flowState) error {
			s.add("b")
			return nil
		}, "a").
		Listen("c", func(_ context.Context, s *flowState) error {
			s.add("c")
			return nil
		})
	res, err := f.Run(context.Background(), flowState{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Errors["a"] == nil {
		t.Fatal("expected error map")
	}
	joined := strings.Join(res.State.log, ",")
	if !strings.Contains(joined, "a") || !strings.Contains(joined, "c") || strings.Contains(joined, "b") {
		t.Fatalf("log %v", res.State.log)
	}
}

func TestFlow_RouterSelectsBranch(t *testing.T) {
	f := NewFlow[flowState]().
		Start("boot", func(_ context.Context, s *flowState) error {
			s.path = "write"
			return nil
		}).
		Router("choose", func(_ context.Context, s *flowState) ([]string, error) {
			return []string{s.path}, nil
		}, "boot").
		Listen("write", func(_ context.Context, s *flowState) error {
			s.add("write")
			return nil
		}, "choose").
		Listen("skipme", func(_ context.Context, s *flowState) error {
			s.add("skipme")
			return nil
		}, "choose")
	res, err := f.Run(context.Background(), flowState{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(res.State.log, ",") != "write" {
		t.Fatalf("%v", res.State.log)
	}
	skipped := false
	for _, st := range res.Steps {
		if st.Name == "skipme" && st.Skipped {
			skipped = true
		}
	}
	if !skipped {
		t.Fatalf("steps %v", res.Steps)
	}
	// D-F12: write is registered before skipme, so it folds first.
	got := names(res.Steps)
	if len(got) < 2 || got[len(got)-2] != "write" || got[len(got)-1] != "skipme" {
		t.Fatalf("fold order %v", got)
	}
}

func TestFlow_RouterUnknownAndNonListener(t *testing.T) {
	unk := NewFlow[flowState]().
		Start("boot", func(context.Context, *flowState) error { return nil }).
		Router("r", func(context.Context, *flowState) ([]string, error) {
			return []string{"nope"}, nil
		}, "boot")
	if _, err := unk.Run(context.Background(), flowState{}); !errors.Is(err, ErrFlowUnknownStep) {
		t.Fatalf("unk %v", err)
	}
	nl := NewFlow[flowState]().
		Start("boot", func(context.Context, *flowState) error { return nil }).
		Start("other", func(context.Context, *flowState) error { return nil }).
		Router("r", func(context.Context, *flowState) ([]string, error) {
			return []string{"other"}, nil
		}, "boot")
	if _, err := nl.Run(context.Background(), flowState{}); !errors.Is(err, ErrFlowUnknownStep) {
		t.Fatalf("non-listener %v", err)
	}
}

func TestFlow_JoinAllDeps(t *testing.T) {
	var n atomic.Int32
	f := NewFlow[flowState]().
		Start("a", func(context.Context, *flowState) error { return nil }).
		Start("b", func(context.Context, *flowState) error { return nil }).
		Listen("join", func(_ context.Context, s *flowState) error {
			n.Add(1)
			s.add("join")
			return nil
		}, "a", "b")
	res, err := f.Run(context.Background(), flowState{})
	if err != nil {
		t.Fatal(err)
	}
	if n.Load() != 1 {
		t.Fatalf("join ran %d", n.Load())
	}
	if res.Steps[len(res.Steps)-1].Name != "join" {
		t.Fatalf("%v", names(res.Steps))
	}
}

func TestFlow_SingleFlight(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	f := NewFlow[flowState]().Start("a", func(context.Context, *flowState) error {
		close(started)
		<-release
		return nil
	})
	done := make(chan error, 1)
	go func() {
		_, err := f.Run(context.Background(), flowState{})
		done <- err
	}()
	<-started
	if _, err := f.Run(context.Background(), flowState{}); !errors.Is(err, ErrFlowRunning) {
		t.Fatalf("%v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestFlow_CancelAtBarrier(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	f := NewFlow[flowState]().
		Start("a", func(_ context.Context, s *flowState) error {
			s.add("a")
			cancel()
			return nil
		}).
		Listen("b", func(_ context.Context, s *flowState) error {
			s.add("b")
			return nil
		}, "a")
	res, err := f.Run(ctx, flowState{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("%v", err)
	}
	if strings.Join(res.State.log, ",") != "a" {
		t.Fatalf("b must wait for barrier: %v", res.State.log)
	}
}

func TestFlow_PanicRecovered(t *testing.T) {
	f := NewFlow[flowState]().Start("a", func(context.Context, *flowState) error {
		panic("nope")
	})
	_, err := f.Run(context.Background(), flowState{})
	if err == nil || !strings.Contains(err.Error(), "panic") {
		t.Fatalf("%v", err)
	}
}

func TestFlow_Events(t *testing.T) {
	var got []string
	var mu sync.Mutex
	f := NewFlow[flowState]().WithEvents(func(ev CrewEvent) {
		mu.Lock()
		got = append(got, ev.Type)
		mu.Unlock()
	}).Start("a", func(context.Context, *flowState) error { return nil })
	if _, err := f.Run(context.Background(), flowState{}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	joined := strings.Join(got, ",")
	for _, want := range []string{EventFlowStarted, EventFlowStepStarted, EventFlowStepCompleted, EventFlowCompleted} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %s in %v", want, got)
		}
	}
}

func TestFlow_NilContext(t *testing.T) {
	f := NewFlow[flowState]().Start("a", func(context.Context, *flowState) error { return nil })
	if _, err := f.Run(nil, flowState{}); err != nil { //nolint:staticcheck
		t.Fatal(err)
	}
}

func TestFlow_DuplicateDepIgnored(t *testing.T) {
	f := NewFlow[flowState]().
		Start("a", func(context.Context, *flowState) error { return nil }).
		Listen("b", func(_ context.Context, s *flowState) error {
			s.add("b")
			return nil
		}, "a", "a")
	res, err := f.Run(context.Background(), flowState{})
	if err != nil {
		t.Fatal(err)
	}
	if res.State.log[0] != "b" {
		t.Fatal(res.State.log)
	}
}

func names(steps []FlowStepTrace) []string {
	out := make([]string, len(steps))
	for i, s := range steps {
		out[i] = s.Name
	}
	return out
}
