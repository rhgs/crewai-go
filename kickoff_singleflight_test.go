package crewai

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// blockingLLM holds Call open until release is closed (or ctx ends).
type blockingLLM struct {
	started chan struct{}
	release chan struct{}
}

func (b *blockingLLM) Call(ctx context.Context, _ []Message) (string, error) {
	select {
	case b.started <- struct{}{}:
	default:
	}
	select {
	case <-b.release:
		return "Final Answer: done", nil
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(10 * time.Second):
		return "Final Answer: timeout", nil
	}
}

func (b *blockingLLM) Model() string { return "blocking-test" }

// fixedLLM always returns the same final answer.
type fixedLLM struct{ out string }

func (f *fixedLLM) Call(ctx context.Context, _ []Message) (string, error) {
	return f.out, nil
}

func (f *fixedLLM) Model() string { return "fixed-test" }

func TestKickoff_ConcurrentReturnsErrCrewRunning(t *testing.T) {
	llm := &blockingLLM{
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	agent := NewAgent("a", "g", "b", llm)
	task := NewTask("do it", "out", agent)
	crew := NewCrew([]*Agent{agent}, []*Task{task})

	var (
		wg      sync.WaitGroup
		firstOK atomic.Bool
	)

	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err := crew.Kickoff(context.Background(), nil)
		if err != nil {
			t.Errorf("first Kickoff: %v", err)
			return
		}
		firstOK.Store(true)
	}()

	select {
	case <-llm.started:
	case <-time.After(3 * time.Second):
		t.Fatal("first Kickoff did not start")
	}

	_, second := crew.Kickoff(context.Background(), nil)
	if !errors.Is(second, ErrCrewRunning) {
		close(llm.release)
		wg.Wait()
		t.Fatalf("second Kickoff: got %v, want ErrCrewRunning", second)
	}

	close(llm.release)
	wg.Wait()
	if !firstOK.Load() {
		t.Fatal("first Kickoff should succeed")
	}
}

func TestKickoff_SequentialReuseOK(t *testing.T) {
	llm := &fixedLLM{out: "Final Answer: once"}
	agent := NewAgent("a", "g", "b", llm)
	t1 := NewTask("one", "out", agent)
	crew := NewCrew([]*Agent{agent}, []*Task{t1})
	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatalf("first: %v", err)
	}
	t2 := NewTask("two", "out", agent)
	crew.Tasks = []*Task{t2}
	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatalf("second sequential: %v", err)
	}
}
