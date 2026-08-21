package crewai

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"time"
)

// Event type strings (stable, additive-only).
const (
	EventKickoffStarted   = "kickoff_started"
	EventKickoffCompleted = "kickoff_completed"
	EventStageStarted     = "stage_started"
	EventStageCompleted   = "stage_completed"
	EventWaveStarted      = "wave_started"
	EventWaveCompleted    = "wave_completed"
	EventTaskStarted      = "task_started"
	EventTaskCompleted    = "task_completed"
	EventToolInvoked      = "tool_invoked"
	EventLLMCallStarted   = "llm_call_started"
	EventLLMCallCompleted = "llm_call_completed"
	EventReactIteration   = "react_iteration"
	EventStructuredRepair = "structured_repair"
	EventLoopPhase        = "loop_phase"
	EventGuardrailBlocked = "guardrail_blocked"
)

// CrewEvent is an exportable lifecycle record. Default fields are
// metadata-only (no prompt bodies, LLM outputs, or tool inputs) — same
// safety posture as Progress (D-C4).
type CrewEvent struct {
	Type       string         `json:"type"`
	Time       time.Time      `json:"time"`
	KickoffID  string         `json:"kickoff_id,omitempty"`
	Stage      string         `json:"stage,omitempty"`
	Task       string         `json:"task,omitempty"`
	Agent      string         `json:"agent,omitempty"`
	Iteration  int            `json:"iteration,omitempty"`
	Phase      string         `json:"phase,omitempty"`
	Tool       string         `json:"tool,omitempty"`
	DurationMs int64          `json:"duration_ms,omitempty"`
	Err        string         `json:"err,omitempty"`
	Attrs      map[string]any `json:"attrs,omitempty"`
}

// EventFunc receives CrewEvents during Kickoff. It MAY be called from
// multiple goroutines and MUST be safe for concurrent use. Panics are
// recovered in emitEvent; Kickoff is not aborted (D-C10).
type EventFunc func(CrewEvent)

type eventsKey struct{}
type kickoffIDKey struct{}

// ContextWithEvents attaches an EventFunc to ctx. Nil is a no-op.
func ContextWithEvents(ctx context.Context, fn EventFunc) context.Context {
	if fn == nil {
		return ctx
	}
	return context.WithValue(ctx, eventsKey{}, fn)
}

// ContextWithKickoffID attaches a correlation id (tests / advanced apps).
func ContextWithKickoffID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, kickoffIDKey{}, id)
}

func eventFuncFromCtx(ctx context.Context) EventFunc {
	fn, _ := ctx.Value(eventsKey{}).(EventFunc)
	return fn
}

func kickoffIDFromCtx(ctx context.Context) string {
	id, _ := ctx.Value(kickoffIDKey{}).(string)
	return id
}

func newKickoffID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Extremely unlikely; still return something stable-length.
		return hex.EncodeToString([]byte("crewai-kickoff-fb"))[:32]
	}
	return hex.EncodeToString(b[:])
}

// emitEvent invokes the EventFunc on ctx, if any. Fills Time and KickoffID
// when empty. Err strings should already be safe; empty Err is fine.
func emitEvent(ctx context.Context, ev CrewEvent) {
	fn := eventFuncFromCtx(ctx)
	if fn == nil {
		return
	}
	if ev.Time.IsZero() {
		ev.Time = time.Now().UTC()
	}
	if ev.KickoffID == "" {
		ev.KickoffID = kickoffIDFromCtx(ctx)
	}
	defer func() {
		if r := recover(); r != nil {
			slog.Default().WarnContext(ctx, "event callback panicked",
				"type", ev.Type, "panic", r)
		}
	}()
	fn(ev)
}

// emitEventErr is emitEvent with a redacted error string.
func emitEventErr(ctx context.Context, ev CrewEvent, err error) {
	if err != nil {
		if re := redactError(err); re != nil {
			ev.Err = re.Error()
		}
	}
	emitEvent(ctx, ev)
}

// progressToEvent maps a Progress value to a CrewEvent (lossy bridge).
func progressToEvent(p Progress) CrewEvent {
	ev := CrewEvent{
		Type:  p.Event,
		Stage: p.Stage,
		Task:  p.Task,
		Agent: p.Agent,
		Tool:  p.Tool,
	}
	if p.Duration > 0 {
		ev.DurationMs = p.Duration.Milliseconds()
	}
	if p.Err != nil {
		ev.Err = p.Err.Error() // already redacted at Progress emit sites
	}
	return ev
}

// ProgressAsEvents wraps an EventFunc as a ProgressFunc so apps can use a
// single sink for legacy Progress-shaped events only (lossy).
func ProgressAsEvents(fn EventFunc) ProgressFunc {
	if fn == nil {
		return nil
	}
	return func(p Progress) {
		fn(progressToEvent(p))
	}
}

// EventLogger returns an EventFunc that logs each event at Info via logger.
// Nil logger uses slog.Default().
func EventLogger(logger *slog.Logger) EventFunc {
	if logger == nil {
		logger = slog.Default()
	}
	return func(ev CrewEvent) {
		logger.Info("crew event",
			"type", ev.Type,
			"kickoff_id", ev.KickoffID,
			"stage", ev.Stage,
			"task", ev.Task,
			"agent", ev.Agent,
			"iteration", ev.Iteration,
			"phase", ev.Phase,
			"tool", ev.Tool,
			"duration_ms", ev.DurationMs,
			"err", ev.Err,
		)
	}
}
