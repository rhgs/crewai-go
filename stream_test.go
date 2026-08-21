package crewai

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// streamStub implements StreamingLLM with scripted chunks or a Handler.
type streamStub struct {
	chunks  []StreamChunk
	callOut string
	callErr error
	// delay between chunks (optional)
	delay time.Duration
	// hang until ctx done after first chunk
	hangAfterFirst bool
}

func (s *streamStub) Call(ctx context.Context, messages []Message) (string, error) {
	if s.callErr != nil {
		return "", s.callErr
	}
	if s.callOut != "" {
		return s.callOut, nil
	}
	var b strings.Builder
	for _, c := range s.chunks {
		b.WriteString(c.Delta)
	}
	return b.String(), nil
}

func (s *streamStub) Model() string { return "stream-stub" }

// CallWithTools implements ToolCallingLLM so native no-tools path is reachable.
func (s *streamStub) CallWithTools(ctx context.Context, messages []Message, tools []ToolSpec) (*ToolCallResponse, error) {
	out, err := s.Call(ctx, messages)
	if err != nil {
		return nil, err
	}
	return &ToolCallResponse{Content: out}, nil
}

func (s *streamStub) CallStream(ctx context.Context, messages []Message) <-chan StreamChunk {
	ch := make(chan StreamChunk, DefaultStreamChanBuffer)
	go func() {
		defer close(ch)
		if len(s.chunks) == 0 && s.callErr != nil {
			select {
			case ch <- StreamChunk{Err: s.callErr}:
			case <-ctx.Done():
			}
			return
		}
		for i, c := range s.chunks {
			if s.delay > 0 {
				select {
				case <-time.After(s.delay):
				case <-ctx.Done():
					return
				}
			}
			select {
			case <-ctx.Done():
				return
			case ch <- c:
			}
			if s.hangAfterFirst && i == 0 {
				<-ctx.Done()
				return
			}
		}
	}()
	return ch
}

// callOnlyLLM implements LLM but not StreamingLLM.
type callOnlyLLM struct {
	out string
	err error
}

func (c *callOnlyLLM) Call(ctx context.Context, messages []Message) (string, error) {
	return c.out, c.err
}
func (c *callOnlyLLM) Model() string { return "call-only" }

func TestCollectStream_ConcatAndDone(t *testing.T) {
	ch := make(chan StreamChunk, 4)
	ch <- StreamChunk{Delta: "Hel"}
	ch <- StreamChunk{Delta: "lo", Done: true}
	close(ch)

	got, err := CollectStream(context.Background(), ch)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "Hello" {
		t.Fatalf("got %q", got)
	}
}

func TestCollectStream_ErrPrioritizesOverDone(t *testing.T) {
	ch := make(chan StreamChunk, 2)
	ch <- StreamChunk{Delta: "x", Done: true, Err: errors.New("boom")}
	close(ch)
	_, err := CollectStream(context.Background(), ch)
	if err == nil || err.Error() != "boom" {
		t.Fatalf("want boom, got %v", err)
	}
}

func TestCollectStream_Incomplete(t *testing.T) {
	ch := make(chan StreamChunk)
	close(ch)
	_, err := CollectStream(context.Background(), ch)
	if !errors.Is(err, ErrStreamIncomplete) {
		t.Fatalf("want ErrStreamIncomplete, got %v", err)
	}
}

func TestCollectStream_NilChan(t *testing.T) {
	_, err := CollectStream(context.Background(), nil)
	if !errors.Is(err, ErrStreamIncomplete) {
		t.Fatalf("want ErrStreamIncomplete, got %v", err)
	}
}

func TestCollectStream_CtxCancel(t *testing.T) {
	ch := make(chan StreamChunk)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := CollectStream(ctx, ch)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want canceled, got %v", err)
	}
}

func TestCallOrStream_StreamingPath(t *testing.T) {
	llm := &streamStub{chunks: []StreamChunk{
		{Delta: "a"},
		{Delta: "b", Done: true},
	}}
	var got []StreamChunk
	sink := func(c StreamChunk) { got = append(got, c) }
	out, err := CallOrStream(context.Background(), llm, nil, sink)
	if err != nil {
		t.Fatal(err)
	}
	if out != "ab" {
		t.Fatalf("out %q", out)
	}
	if len(got) != 2 || got[0].Delta != "a" || !got[1].Done {
		t.Fatalf("sink %#v", got)
	}
}

func TestCallOrStream_NonStreamingFallback(t *testing.T) {
	llm := &callOnlyLLM{out: "full"}
	var got []StreamChunk
	out, err := CallOrStream(context.Background(), llm, nil, func(c StreamChunk) {
		got = append(got, c)
	})
	if err != nil {
		t.Fatal(err)
	}
	if out != "full" || len(got) != 1 || got[0].Delta != "full" || !got[0].Done {
		t.Fatalf("out=%q got=%#v", out, got)
	}
}

func TestCallOrStream_NilSinkUsesCall(t *testing.T) {
	llm := &streamStub{
		chunks:  []StreamChunk{{Delta: "streamed", Done: true}},
		callOut: "from-call",
	}
	out, err := CallOrStream(context.Background(), llm, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	// D-S9: sink nil → Call, not CallStream
	if out != "from-call" {
		t.Fatalf("want from-call, got %q", out)
	}
}

func TestCallOrStream_CallErrorEmitted(t *testing.T) {
	boom := errors.New("nope")
	llm := &callOnlyLLM{err: boom}
	var got StreamChunk
	_, err := CallOrStream(context.Background(), llm, nil, func(c StreamChunk) { got = c })
	if !errors.Is(err, boom) {
		t.Fatalf("err %v", err)
	}
	if got.Err == nil {
		t.Fatal("expected Err chunk")
	}
}

func TestEmitStream_RecoversPanic(t *testing.T) {
	// Should not panic the test.
	emitStream(context.Background(), func(StreamChunk) {
		panic("boom")
	}, StreamChunk{Delta: "x"})
}

func TestWithTaskAgent_FillsMetadata(t *testing.T) {
	var got StreamChunk
	sink := withTaskAgent(func(c StreamChunk) { got = c }, "T1", "AgentA")
	sink(StreamChunk{Delta: "hi", Done: true})
	if got.Task != "T1" || got.Agent != "AgentA" || got.Delta != "hi" {
		t.Fatalf("%#v", got)
	}
}

func TestCallLLMText_WiresTaskAgent(t *testing.T) {
	llm := &streamStub{chunks: []StreamChunk{
		{Delta: "z", Done: true},
	}}
	agent := NewAgent("Writer", "g", "b", llm)
	task := NewTask("write the draft", "expected", agent)
	task.Name = "draft"
	var got StreamChunk
	ctx := ContextWithStream(context.Background(), func(c StreamChunk) { got = c })
	out, err := callLLMText(ctx, llm, []Message{UserMessage("u")}, task, agent)
	if err != nil {
		t.Fatal(err)
	}
	if out != "z" || got.Task != "draft" || got.Agent != "Writer" {
		t.Fatalf("out=%q got=%#v", out, got)
	}
}

func TestExecutor_NoTools_Streams(t *testing.T) {
	llm := &streamStub{chunks: []StreamChunk{
		{Delta: "Hel"},
		{Delta: "lo world", Done: true},
	}}
	agent := NewAgent("A", "g", "b", llm)
	task := NewTask("say", "hello", agent)
	var mu sync.Mutex
	var deltas []string
	crew := NewCrew([]*Agent{agent}, []*Task{task}).WithStream(func(c StreamChunk) {
		mu.Lock()
		defer mu.Unlock()
		if c.Delta != "" {
			deltas = append(deltas, c.Delta)
		}
	})
	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.TasksOutput) != 1 || out.TasksOutput[0].Output != "Hello world" {
		t.Fatalf("output %#v deltas %v", out, deltas)
	}
	mu.Lock()
	defer mu.Unlock()
	joined := strings.Join(deltas, "")
	if joined != "Hello world" {
		t.Fatalf("deltas %v joined %q", deltas, joined)
	}
}

func TestExecutor_WithTools_DoesNotStreamIntermediate(t *testing.T) {
	// ReAct with tools must use Call, not stream intermediate thoughts.
	type toolLLM struct {
		n int
	}
	// not StreamingLLM
	llm := &toolReactLLM{}
	agent := NewAgent("A", "g", "b", llm)
	agent.Tools = []Tool{&progressTool{}}
	task := NewTask("t", "x", agent)
	var calls int
	crew := NewCrew([]*Agent{agent}, []*Task{task}).WithStream(func(StreamChunk) {
		calls++
	})
	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("expected no stream chunks for tool path, got %d", calls)
	}
}

// toolReactLLM drives one tool call then Final Answer (implements LLM only).
type toolReactLLM struct {
	n int
}

func (t *toolReactLLM) Call(ctx context.Context, messages []Message) (string, error) {
	t.n++
	if t.n == 1 {
		return "Action: ptool\nAction Input: ", nil
	}
	return "Final Answer: done", nil
}
func (t *toolReactLLM) Model() string { return "tool-react" }

func TestExecutor_NativeNoTools_Streams(t *testing.T) {
	llm := &streamStub{chunks: []StreamChunk{
		{Delta: "native-ok", Done: true},
	}}
	agent := NewAgent("A", "g", "b", llm)
	agent.ToolMode = ToolModeNative
	// no tools → fallthrough plain Call path
	task := NewTask("n", "x", agent)
	var saw string
	crew := NewCrew([]*Agent{agent}, []*Task{task}).WithStream(func(c StreamChunk) {
		if c.Delta != "" {
			saw += c.Delta
		}
	})
	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if saw != "native-ok" {
		t.Fatalf("saw %q out=%v", saw, out.TasksOutput)
	}
}

func TestStream_AsyncWave_DemuxTaskLabels(t *testing.T) {
	makeLLM := func(text string) *streamStub {
		return &streamStub{chunks: []StreamChunk{
			{Delta: text, Done: true},
		}}
	}
	// Same StreamingLLM instance is fine; sequential chunks per CallStream.
	// Use separate stubs so concurrent CallStream don't share chunk slices.
	llmA := makeLLM("AAA")
	llmB := makeLLM("BBB")
	a1 := NewAgent("A1", "g", "b", llmA)
	a2 := NewAgent("A2", "g", "b", llmB)
	t1 := NewTask("do a", "x", a1).WithAsync()
	t1.Name = "task-a"
	t2 := NewTask("do b", "y", a2).WithAsync()
	t2.Name = "task-b"

	var mu sync.Mutex
	seen := map[string]string{}
	crew := NewCrew([]*Agent{a1, a2}, []*Task{t1, t2}).WithStream(func(c StreamChunk) {
		mu.Lock()
		defer mu.Unlock()
		if c.Done || c.Delta != "" {
			seen[c.Task] += c.Delta
		}
	})
	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if seen["task-a"] != "AAA" || seen["task-b"] != "BBB" {
		t.Fatalf("demux %#v", seen)
	}
}

func TestStream_CallbackPanic_KickoffContinues(t *testing.T) {
	llm := &streamStub{chunks: []StreamChunk{{Delta: "ok", Done: true}}}
	agent := NewAgent("A", "g", "b", llm)
	task := NewTask("t", "x", agent)
	crew := NewCrew([]*Agent{agent}, []*Task{task}).WithStream(func(StreamChunk) {
		panic("ui boom")
	})
	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.TasksOutput) != 1 || out.TasksOutput[0].Output != "ok" {
		t.Fatalf("%#v", out.TasksOutput)
	}
}

var _ StreamingLLM = (*streamStub)(nil)
