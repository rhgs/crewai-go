package crewai

import (
	"context"
	"log/slog"
	"strings"
)

// DefaultStreamChanBuffer is the recommended buffer size for channels
// returned by StreamingLLM.CallStream (D-S11).
const DefaultStreamChanBuffer = 16

// StreamChunk is one unit of a streaming completion.
//
// v1 is text-centric: providers map wire deltas into Delta only.
// Task and Agent are filled by the executor wrapper (D-S13) so concurrent
// Async waves can be demultiplexed by the app sink.
type StreamChunk struct {
	// Delta is the incremental text fragment. Providers SHOULD omit
	// empty/null wire deltas (no empty-Delta spam).
	Delta string

	// Task is the task label (Name or "Task N"). Set by the executor when
	// delivering to the app sink. Empty when CallStream is used outside a
	// task (tests / CollectStream).
	Task string

	// Agent is the agent role for the in-flight task. Same rules as Task.
	Agent string

	// Done is true on the terminal successful chunk. Delta may still hold
	// a final fragment (D-S2-A). After a Done chunk the channel is closed.
	// Done and Err MUST NOT both be set (D-S14); CollectStream prioritizes Err.
	Done bool

	// Err is non-nil on a terminal failure chunk. Delta is empty.
	// After an Err chunk the channel is closed.
	Err error
}

// StreamFunc is invoked for each chunk delivered to the app (deltas, Done,
// Err). It MAY be called from multiple goroutines when Async waves or
// Staged stages run in parallel and MUST therefore be safe for concurrent
// use, like ProgressFunc. Set via Crew.WithStream BEFORE Kickoff.
// Panics are recovered in emitStream (D-S12); Kickoff is not aborted.
type StreamFunc func(StreamChunk)

// StreamingLLM is an optional capability that an LLM may implement when
// the underlying provider supports token/delta streaming. It embeds LLM
// so Model and Call remain available on the asserted value (same pattern
// as ToolCallingLLM).
//
// When a stream sink is configured (Crew.WithStream / ContextWithStream)
// and the path matrix allows streaming, the executor type-asserts this
// interface. Implementers that only support Call remain valid forever.
type StreamingLLM interface {
	LLM
	// CallStream starts a completion and returns a channel of chunks.
	// The channel is NEVER nil (D-S14). Setup failures (marshal, auth, dial)
	// are delivered as the first chunk {Err: ...} then the channel is closed.
	// The channel is closed after the terminal Done or Err chunk, or when
	// ctx is cancelled (producer exits; CollectStream maps bare close to
	// ctx.Err() or ErrStreamIncomplete). Implementers MUST:
	//   - respect ctx cancellation promptly;
	//   - not block forever if the caller stops receiving (prefer ctx);
	//   - enforce MaxProviderResponseBytes on accumulated text AND on
	//     raw HTTP body bytes read (D-S8); use ErrStreamResponseTooLarge;
	//   - be safe for concurrent Call/CallStream on the same client;
	//   - leave Task/Agent empty (executor fills them);
	//   - never set Done and Err on the same chunk.
	// The returned channel SHOULD use DefaultStreamChanBuffer (D-S11).
	CallStream(ctx context.Context, messages []Message) <-chan StreamChunk
}

// streamKey is the unexported context key used to attach a StreamFunc.
type streamKey struct{}

// ContextWithStream attaches a StreamFunc to ctx. Passing nil is a no-op.
// End users normally set the callback via Crew.WithStream.
func ContextWithStream(ctx context.Context, fn StreamFunc) context.Context {
	if fn == nil {
		return ctx
	}
	return context.WithValue(ctx, streamKey{}, fn)
}

// streamFuncFromCtx returns the StreamFunc attached to ctx, or nil.
func streamFuncFromCtx(ctx context.Context) StreamFunc {
	fn, _ := ctx.Value(streamKey{}).(StreamFunc)
	return fn
}

// emitStream invokes sink with chunk. Panics from the sink are recovered
// and logged via slog.Default() (D-S12). Nil sink is a no-op. Err on the
// chunk is passed through redactError before delivery (same posture as
// Progress.Err).
func emitStream(ctx context.Context, sink StreamFunc, chunk StreamChunk) {
	if sink == nil {
		return
	}
	if chunk.Err != nil {
		chunk.Err = redactError(chunk.Err)
	}
	defer func() {
		if r := recover(); r != nil {
			slog.Default().WarnContext(ctx, "stream callback panicked",
				"task", chunk.Task, "agent", chunk.Agent, "panic", r)
		}
	}()
	sink(chunk)
}

// withTaskAgent returns a StreamFunc that copies task/agent onto every
// chunk before forwarding (D-S13). Providers stay metadata-free.
func withTaskAgent(sink StreamFunc, task, agent string) StreamFunc {
	if sink == nil {
		return nil
	}
	return func(chunk StreamChunk) {
		if chunk.Task == "" {
			chunk.Task = task
		}
		if chunk.Agent == "" {
			chunk.Agent = agent
		}
		sink(chunk)
	}
}

// CollectStream drains ch and returns the concatenated Delta text.
//
// Returns the first non-nil chunk.Err (Done is ignored if Err is set),
// ctx.Err() if ctx is done when the channel closes without a terminal
// chunk, ErrStreamIncomplete if the channel closes with neither Done nor
// Err while ctx is still OK, or nil on clean Done. Task/Agent are ignored
// for concatenation. Public stable API (O-S3).
func CollectStream(ctx context.Context, ch <-chan StreamChunk) (string, error) {
	if ch == nil {
		return "", ErrStreamIncomplete
	}
	var b strings.Builder
	for {
		select {
		case <-ctx.Done():
			return b.String(), ctx.Err()
		case chunk, ok := <-ch:
			if !ok {
				if err := ctx.Err(); err != nil {
					return b.String(), err
				}
				return b.String(), ErrStreamIncomplete
			}
			if chunk.Err != nil {
				return b.String(), chunk.Err
			}
			if chunk.Delta != "" {
				b.WriteString(chunk.Delta)
			}
			if chunk.Done {
				// Drain remaining (should be closed soon) without blocking forever.
				return b.String(), nil
			}
		}
	}
}

// drainToSink concatenates deltas while forwarding every chunk to sink via
// emitStream. Same terminal semantics as CollectStream.
func drainToSink(ctx context.Context, ch <-chan StreamChunk, sink StreamFunc) (string, error) {
	if ch == nil {
		err := ErrStreamIncomplete
		emitStream(ctx, sink, StreamChunk{Err: err})
		return "", err
	}
	var b strings.Builder
	for {
		select {
		case <-ctx.Done():
			return b.String(), ctx.Err()
		case chunk, ok := <-ch:
			if !ok {
				if err := ctx.Err(); err != nil {
					return b.String(), err
				}
				err := ErrStreamIncomplete
				emitStream(ctx, sink, StreamChunk{Err: err})
				return b.String(), err
			}
			if chunk.Delta != "" {
				b.WriteString(chunk.Delta)
			}
			// Forward terminal and non-empty deltas; always forward Done/Err.
			if chunk.Err != nil || chunk.Done || chunk.Delta != "" {
				emitStream(ctx, sink, chunk)
			}
			if chunk.Err != nil {
				return b.String(), chunk.Err
			}
			if chunk.Done {
				return b.String(), nil
			}
		}
	}
}

// CallOrStream prefers StreamingLLM when sink != nil, otherwise Call.
// Always returns the full text for the executor; sink receives deltas.
// All sink invocations go through emitStream (D-S12). When sink is nil,
// always uses Call (D-S9). Non-streaming LLMs with a sink emit one
// Delta+Done chunk (D-S7).
func CallOrStream(ctx context.Context, llm LLM, messages []Message, sink StreamFunc) (string, error) {
	if llm == nil {
		return "", ErrNoLLM
	}
	if sink != nil {
		if s, ok := llm.(StreamingLLM); ok {
			ch := s.CallStream(ctx, messages)
			return drainToSink(ctx, ch, sink)
		}
	}
	out, err := llm.Call(ctx, messages)
	if err != nil {
		if sink != nil {
			emitStream(ctx, sink, StreamChunk{Err: err})
		}
		return "", err
	}
	if sink != nil {
		emitStream(ctx, sink, StreamChunk{Delta: out, Done: true})
	}
	return out, nil
}

// callLLMText streams when a sink is on ctx (path matrix Yes only).
// Do NOT use as a global drop-in for every a.LLM.Call site (D-S4).
func callLLMText(ctx context.Context, llm LLM, messages []Message, task *Task, agent *Agent) (string, error) {
	sink := streamFuncFromCtx(ctx)
	if sink != nil {
		taskName := ""
		if task != nil {
			taskName = taskLabel(task, 0)
		}
		agentRole := ""
		if agent != nil {
			agentRole = agent.Role
		}
		sink = withTaskAgent(sink, taskName, agentRole)
	}
	return CallOrStream(ctx, llm, messages, sink)
}
