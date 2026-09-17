package crewai

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// TaskTraceRecord is one JSONL line written by TraceRecorder (D-X3).
// Default is metadata-only (D-X4 / D-C4); bodies require WithTraceBodies.
type TaskTraceRecord struct {
	KickoffID  string          `json:"kickoff_id,omitempty"`
	Wave       string          `json:"wave,omitempty"`
	Task       string          `json:"task"`
	Agent      string          `json:"agent,omitempty"`
	Prompt     *PromptSnapshot `json:"prompt,omitempty"`
	Output     string          `json:"output,omitempty"`
	Facts      []Fact          `json:"facts,omitempty"`
	Tools      []ToolTrace     `json:"tools,omitempty"`
	DurationMs int64           `json:"duration_ms,omitempty"`
	Err        string          `json:"err,omitempty"`
}

// PromptSnapshot is the opt-in prompt body (D-X4). Empty unless bodies on.
type PromptSnapshot struct {
	Description string `json:"description,omitempty"`
	Context     string `json:"context,omitempty"`
}

// TraceFilter decides whether a captured record is published (D-X6).
type TraceFilter func(TaskTraceRecord) bool

// TraceRecorder captures per-task records during Kickoff (D-X1).
// Safe for concurrent capture (wave workers); publish is serialized (D-X7).
type TraceRecorder struct {
	mu      sync.Mutex
	pending map[*Task]TaskTraceRecord
	records []TaskTraceRecord
	bodies  bool
	filter  TraceFilter
}

type traceWaveKey struct{}

func contextWithTraceWave(ctx context.Context, wave string) context.Context {
	if wave == "" {
		return ctx
	}
	return context.WithValue(ctx, traceWaveKey{}, wave)
}

func traceWaveFromCtx(ctx context.Context) string {
	s, _ := ctx.Value(traceWaveKey{}).(string)
	return s
}

// NewTraceRecorder builds a recorder. Attach with Crew.WithTracer.
func NewTraceRecorder(opts ...func(*TraceRecorder)) *TraceRecorder {
	r := &TraceRecorder{pending: map[*Task]TaskTraceRecord{}}
	for _, o := range opts {
		o(r)
	}
	return r
}

// WithTraceBodies enables prompt/output/tool-arg capture (default off, D-X4).
// Values still pass through redactString.
func WithTraceBodies(on bool) func(*TraceRecorder) {
	return func(r *TraceRecorder) { r.bodies = on }
}

// WithTraceFilter sets the publication filter (D-X6). Nil keeps everything.
func WithTraceFilter(fn TraceFilter) func(*TraceRecorder) {
	return func(r *TraceRecorder) { r.filter = fn }
}

// WithTracer attaches a TraceRecorder to the crew (D-X1).
func (c *Crew) WithTracer(r *TraceRecorder) *Crew {
	c.Tracer = r
	return c
}

// Records returns a copy of published records in fold order (D-X8).
func (r *TraceRecorder) Records() []TaskTraceRecord {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]TaskTraceRecord, len(r.records))
	copy(out, r.records)
	return out
}

// Reset clears pending and published records.
func (r *TraceRecorder) Reset() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.pending = map[*Task]TaskTraceRecord{}
	r.records = nil
	r.mu.Unlock()
}

// Save writes published records as JSONL with mode 0600 (D-X3, D-X5).
// Path is caller-trusted.
func (r *TraceRecorder) Save(path string) error {
	if r == nil {
		return fmt.Errorf("crewai: nil TraceRecorder")
	}
	recs := r.Records()
	var buf []byte
	for _, rec := range recs {
		line, err := json.Marshal(rec)
		if err != nil {
			return fmt.Errorf("crewai: trace marshal: %w", err)
		}
		buf = append(buf, line...)
		buf = append(buf, '\n')
	}
	if err := os.WriteFile(path, buf, 0o600); err != nil {
		return fmt.Errorf("crewai: TraceRecorder.Save: %w", err)
	}
	return nil
}

func (r *TraceRecorder) capture(ctx context.Context, task *Task, agent *Agent, contextText, output string, facts []Fact, dur time.Duration, execErr error) {
	if r == nil || task == nil {
		return
	}
	rec := TaskTraceRecord{
		KickoffID:  kickoffIDFromCtx(ctx),
		Wave:       traceWaveFromCtx(ctx),
		Task:       taskLabel(task, 0),
		DurationMs: dur.Milliseconds(),
	}
	if agent != nil {
		rec.Agent = agent.Role
	}
	if execErr != nil {
		rec.Err = redactString(execErr.Error())
	}
	if r.bodies {
		rec.Output = redactString(output)
		rec.Prompt = &PromptSnapshot{
			Description: redactString(task.Description),
			Context:     redactString(contextText),
		}
		rec.Facts = bodyFacts(facts)
		rec.Tools = bodyTools(task.ToolTraces())
	} else {
		rec.Facts = metadataFacts(facts)
		rec.Tools = metadataTools(task.ToolTraces())
	}
	r.mu.Lock()
	if r.pending == nil {
		r.pending = map[*Task]TaskTraceRecord{}
	}
	r.pending[task] = rec
	r.mu.Unlock()
}

func (r *TraceRecorder) publish(task *Task) {
	if r == nil || task == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.pending[task]
	if !ok {
		return
	}
	delete(r.pending, task)
	if r.filter != nil && !r.filter(rec) {
		return
	}
	r.records = append(r.records, rec)
}

// bodyFacts redacts free-text free-text fields (Claim) while keeping
// provenance metadata. PayloadHash is unchanged (it is a hash, not a secret).
func bodyFacts(facts []Fact) []Fact {
	if len(facts) == 0 {
		return nil
	}
	out := make([]Fact, len(facts))
	for i, f := range facts {
		out[i] = Fact{
			Claim:       redactString(f.Claim),
			SourceOrg:   redactString(f.SourceOrg),
			SourceURL:   redactString(f.SourceURL),
			CollectedAt: f.CollectedAt,
			PayloadHash: f.PayloadHash,
		}
	}
	return out
}

// bodyTools keeps args/output for bodies mode but redacts their string
// content so secrets do not pass through unmarked.
func bodyTools(tr []ToolTrace) []ToolTrace {
	if len(tr) == 0 {
		return nil
	}
	out := make([]ToolTrace, len(tr))
	for i, t := range tr {
		out[i] = ToolTrace{
			Tool:     t.Tool,
			Failed:   t.Failed,
			Duration: t.Duration,
			Output:   redactString(t.Output),
		}
		if len(t.Args) > 0 {
			red := redactString(string(t.Args))
			out[i].Args = json.RawMessage(red)
		}
	}
	return out
}

func metadataFacts(facts []Fact) []Fact {
	if len(facts) == 0 {
		return nil
	}
	out := make([]Fact, len(facts))
	for i, f := range facts {
		out[i] = Fact{
			SourceOrg:   f.SourceOrg,
			SourceURL:   f.SourceURL,
			CollectedAt: f.CollectedAt,
			PayloadHash: f.PayloadHash,
		}
	}
	return out
}

func metadataTools(tr []ToolTrace) []ToolTrace {
	if len(tr) == 0 {
		return nil
	}
	out := make([]ToolTrace, len(tr))
	for i, t := range tr {
		out[i] = ToolTrace{Tool: t.Tool, Failed: t.Failed, Duration: t.Duration}
	}
	return out
}
