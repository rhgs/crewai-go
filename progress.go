package crewai

import (
	"context"
	"log/slog"
	"time"
)

// ProgressFunc is a callback invoked during Kickoff to report execution
// progress. It MAY be called from multiple goroutines (stages run in
// parallel) and MUST therefore be safe for concurrent use, like an
// slog.Handler. If ProgressFunc panics, the executor recovers and logs
// via slog.Default(); Kickoff is never aborted by a callback panic.
type ProgressFunc func(Progress)

// Progress carries metadata about a single execution event. It NEVER
// contains prompt bodies, LLM outputs, or tool inputs — only
// metadata safe to surface to a frontend.
type Progress struct {
	// Stage is the stage name in the Staged process; empty for
	// Sequential and Hierarchical.
	Stage string
	// Task is the task label (Name if set, otherwise "Task N").
	Task string
	// Agent is the role of the agent assigned to the task.
	Agent string
	// Event is one of:
	//   "stage_started", "stage_completed",
	//   "task_started",  "task_completed",
	//   "tool_invoked".
	Event string
	// Tool is the name of the tool invoked (only for "tool_invoked").
	Tool string
	// Duration is the tool execution duration (only for "tool_invoked").
	Duration time.Duration
	// Err is the terminal error on "task_completed" when the task
	// failed (nil on success). It is redacted via redactError before
	// being passed to the callback so providers' API keys in error
	// messages are masked.
	Err error
}

// progressKey is the unexported context key used to attach a
// ProgressFunc to ctx.
type progressKey struct{}

// ContextWithProgress attaches a ProgressFunc to ctx. Passing nil is a
// no-op. Exposed for tests; end users normally set the callback via
// Crew.WithProgress.
func ContextWithProgress(ctx context.Context, fn ProgressFunc) context.Context {
	if fn == nil {
		return ctx
	}
	return context.WithValue(ctx, progressKey{}, fn)
}

// emitProgress invokes the callback attached to ctx, if any. Panics
// from the callback are recovered and logged via slog.Default().
func emitProgress(ctx context.Context, p Progress) {
	fn, ok := ctx.Value(progressKey{}).(ProgressFunc)
	if !ok || fn == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			slog.Default().WarnContext(ctx, "progress callback panicked",
				"event", p.Event, "panic", r)
		}
	}()
	fn(p)
}
