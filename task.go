package crewai

import (
	"context"
	"os"
	"strings"
	"sync"
)

// WarningSink is an optional interface available via context.Context
// during task execution. A tool or agent component that encounters a
// non-fatal issue (e.g. a secondary data source is down) can call
// AddWarning to record it without failing the task. The executor
// injects the Task into the context as a WarningSink before invoking
// tools and aggregates recorded warnings into TaskOutput.Warnings and
// CrewOutput.Warnings after success.
type WarningSink interface {
	AddWarning(msg string)
}

// warningSinkKey is the unexported context key used to attach a
// WarningSink to ctx. Using an unexported empty struct prevents
// collisions with other libraries.
type warningSinkKey struct{}

// ContextWithWarningSink returns a child context that carries the
// given sink as a WarningSink. Exposed for tests and for callers that
// run their own executors; end users normally do not call this — the
// built-in executor injects the sink automatically.
func ContextWithWarningSink(ctx context.Context, sink WarningSink) context.Context {
	if sink == nil {
		return ctx
	}
	return context.WithValue(ctx, warningSinkKey{}, sink)
}

// warningSinkFromCtx retrieves the WarningSink from ctx, or nil when
// none was attached.
func warningSinkFromCtx(ctx context.Context) WarningSink {
	if ws, ok := ctx.Value(warningSinkKey{}).(WarningSink); ok {
		return ws
	}
	return nil
}

// AddWarningFromCtx is a convenience for tools that prefer not to
// perform the type assertion themselves. It is a no-op when no sink is
// attached to ctx.
//
//	if crewai.AddWarningFromCtx(ctx, "secondary source timeout") { /* ignored */ }
func AddWarningFromCtx(ctx context.Context, msg string) {
	if ws := warningSinkFromCtx(ctx); ws != nil {
		ws.AddWarning(msg)
	}
}

// Task describes a unit of work to be performed by an agent.
type Task struct {
	// Name is a short, optional identifier, useful in logs and memory.
	Name string
	// Description is the detailed instruction of what to do. It supports
	// variable interpolation in the {key} format via the Kickoff inputs.
	Description string
	// ExpectedOutput describes the expected format/quality of the answer.
	ExpectedOutput string

	// Agent is the task's assignee. If nil in the sequential process, the
	// crew uses the next available agent.
	Agent *Agent

	// Tools, when present, replaces the agent's tools for this specific task.
	Tools []Tool

	// Context lists tasks whose outputs should be provided as context to
	// this task.
	Context []*Task

	// OutputFile, when set, causes the task output to be written to that file.
	OutputFile string

	// Structured, when non-nil, requires this task to produce JSON output
	// validated against the embedded JSON Schema. The executor enters
	// structured mode and bypasses the ReAct tool-use loop.
	Structured *StructuredOutput

	// Guardrail, when set, is run against this task's output as soon as
	// the task completes (before the next task starts). If it returns a
	// non-nil error, the crew execution halts and Kickoff returns
	// ErrBlockedByGuardrail. The guardrail receives a CrewOutput with
	// Final set to this task's output.
	Guardrail Guardrail

	// Loop, when set, overrides the agent's loop for this specific task.
	Loop Loop

	mu     sync.RWMutex
	output string
	done   bool
	// toolTraces holds native tool call traces (empty when using ReAct).
	toolTraces []ToolTrace
	// warnings holds non-fatal diagnostics recorded during execution
	// (e.g. a secondary data source unavailable). Thread-safe via mu.
	warnings []string
}

// NewTask creates a task with a description, an expected output, and the
// responsible agent.
func NewTask(description, expectedOutput string, agent *Agent) *Task {
	return &Task{
		Description:    description,
		ExpectedOutput: expectedOutput,
		Agent:          agent,
	}
}

// WithContext sets the context tasks (dependencies) of this task.
func (t *Task) WithContext(tasks ...*Task) *Task {
	t.Context = append(t.Context, tasks...)
	return t
}

// WithGuardrail sets a task-level guardrail and returns the task for
// fluent chaining. The guardrail runs against this task's output as soon
// as the task completes.
func (t *Task) WithGuardrail(g Guardrail) *Task {
	t.Guardrail = g
	return t
}

// Output returns the output already produced by the task (empty if not yet
// executed).
func (t *Task) Output() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.output
}

// setOutput records the task output and, if configured, writes it to a file.
func (t *Task) setOutput(out string) error {
	t.mu.Lock()
	t.output = out
	t.done = true
	file := t.OutputFile
	t.mu.Unlock()

	if file != "" {
		return os.WriteFile(file, []byte(out), 0o600)
	}
	return nil
}

// setToolTraces records the native tool call traces for this task.
func (t *Task) setToolTraces(traces []ToolTrace) {
	t.mu.Lock()
	t.toolTraces = traces
	t.mu.Unlock()
}

// ToolTraces returns the native tool call traces for this task (empty when
// using ReAct).
func (t *Task) ToolTraces() []ToolTrace {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.toolTraces
}

// AddWarning records a non-fatal diagnostic for this task. Warnings
// represent partial successes: the task completed, but a secondary
// source was unavailable or a non-critical step failed. They do NOT
// cause Kickoff to fail and are preserved in TaskOutput.Warnings and
// CrewOutput.Warnings so the caller can render a degraded section
// rather than aborting the whole investigation.
//
// Thread-safe: may be called from any goroutine (e.g. a tool running in
// a parallel stage).
func (t *Task) AddWarning(msg string) {
	if msg == "" {
		return
	}
	t.mu.Lock()
	t.warnings = append(t.warnings, msg)
	t.mu.Unlock()
}

// Warnings returns a copy of the warnings recorded so far. The slice
// is in insertion order.
func (t *Task) Warnings() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if len(t.warnings) == 0 {
		return nil
	}
	out := make([]string, len(t.warnings))
	copy(out, t.warnings)
	return out
}

// contextText builds the context text from the task's dependencies.
func (t *Task) contextText() string {
	var b strings.Builder
	for _, dep := range t.Context {
		out := dep.Output()
		if out == "" {
			continue
		}
		if dep.Name != "" {
			b.WriteString(dep.Name)
			b.WriteString(":\n")
		}
		b.WriteString(out)
		b.WriteString("\n\n")
	}
	return strings.TrimSpace(b.String())
}

// interpolate replaces occurrences of {key} in the description and expected
// output using the inputs map.
func (t *Task) interpolate(inputs map[string]string) {
	if len(inputs) == 0 {
		return
	}
	t.Description = interpolate(t.Description, inputs)
	t.ExpectedOutput = interpolate(t.ExpectedOutput, inputs)
}

func interpolate(s string, inputs map[string]string) string {
	for k, v := range inputs {
		s = strings.ReplaceAll(s, "{"+k+"}", v)
	}
	return s
}
