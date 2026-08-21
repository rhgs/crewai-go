package crewai

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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

	// OutputFile, when set, causes the task output to be written to that file
	// with mode 0600. The path is cleaned; empty paths after trim/clean are
	// rejected. When OutputDir (or Crew.OutputDir) is set, the path must
	// resolve inside that directory (symlink-aware). Treat OutputFile as a
	// trusted application path — never pass unvalidated model output here.
	OutputFile string

	// OutputDir, when set, jails OutputFile writes to that directory.
	// Paths are resolved with filepath.Abs and filepath.EvalSymlinks
	// (fail closed on eval errors). Relative OutputFile values are
	// interpreted relative to the process working directory before the
	// jail check. Empty means no task-level jail (Crew.OutputDir may still apply).
	OutputDir string

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

	// Async, when true, marks the task as eligible to run concurrently with
	// other ready Async tasks in the sequential and hierarchical processes.
	// Default false keeps today's behavior (fully serial outside Staged).
	// Dependencies (Context) are always honored: an Async task only starts
	// after every dependency finished in an earlier wave. Under the Staged
	// process this flag is ignored (stage batches own the parallelism).
	Async bool

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
//
// Under async scheduling (Async) every dependency listed here must finish in
// a strictly earlier wave: a Context edge to a task in the same wave (or a
// cycle) is rejected at Kickoff with ErrTaskDependencyCycle.
func (t *Task) WithContext(tasks ...*Task) *Task {
	t.Context = append(t.Context, tasks...)
	return t
}

// WithAsync marks the task Async (concurrent-friendly under
// sequential/hierarchical) and returns it for fluent chaining.
func (t *Task) WithAsync() *Task {
	t.Async = true
	return t
}

// WithGuardrail sets a task-level guardrail and returns the task for
// fluent chaining. The guardrail runs against this task's output as soon
// as the task completes.
func (t *Task) WithGuardrail(g Guardrail) *Task {
	t.Guardrail = g
	return t
}

// WithOutputDir sets a directory jail for OutputFile writes and returns
// the task for fluent chaining. See OutputDir for symlink evaluation rules.
func (t *Task) WithOutputDir(dir string) *Task {
	t.OutputDir = dir
	return t
}

// Output returns the output already produced by the task (empty if not yet
// executed).
func (t *Task) Output() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.output
}

// setOutput records the task output and, if configured, writes it to a file
// using the task-level OutputDir jail (if any).
func (t *Task) setOutput(out string) error {
	return t.setOutputWithJail(out, "")
}

// setOutputWithJail records the task output and writes OutputFile when set.
// jail overrides or supplies the directory jail: if non-empty it is used;
// otherwise the task's OutputDir is used. An empty jail means no path jail
// (only Clean + empty rejection). Called by the crew with Crew.OutputDir
// when the task has no OutputDir of its own.
func (t *Task) setOutputWithJail(out, jail string) error {
	t.mu.Lock()
	t.output = out
	t.done = true
	file := t.OutputFile
	taskJail := t.OutputDir
	t.mu.Unlock()

	if strings.TrimSpace(file) == "" {
		return nil
	}
	if jail == "" {
		jail = taskJail
	}
	return writeTaskOutputFile(file, jail, []byte(out))
}

// writeTaskOutputFile cleans path, optionally enforces a directory jail with
// symlink evaluation (fail closed), and writes data with mode 0600.
func writeTaskOutputFile(path, jail string, data []byte) error {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" || path == "." {
		return fmt.Errorf("%w: empty path", ErrOutputPathRejected)
	}
	if jail != "" {
		resolved, err := resolveOutputPathInJail(path, jail)
		if err != nil {
			return err
		}
		path = resolved
	}
	return os.WriteFile(path, data, 0o600)
}

// resolveOutputPathInJail returns an absolute, symlink-resolved path that
// must equal jail or live under jail+separator. EvalSymlinks is applied to
// the jail and to the deepest existing ancestor of the target (so new files
// can still be created). Any resolution error fails closed.
func resolveOutputPathInJail(path, jail string) (string, error) {
	absJail, err := filepath.Abs(strings.TrimSpace(jail))
	if err != nil {
		return "", fmt.Errorf("%w: jail abs: %v", ErrOutputPathRejected, err)
	}
	absJail, err = filepath.EvalSymlinks(absJail)
	if err != nil {
		return "", fmt.Errorf("%w: jail symlinks: %v", ErrOutputPathRejected, err)
	}
	absJail = filepath.Clean(absJail)

	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("%w: path abs: %v", ErrOutputPathRejected, err)
	}
	resolved, err := evalSymlinksExisting(absPath)
	if err != nil {
		return "", fmt.Errorf("%w: path symlinks: %v", ErrOutputPathRejected, err)
	}
	resolved = filepath.Clean(resolved)

	sep := string(filepath.Separator)
	if resolved == absJail || strings.HasPrefix(resolved, absJail+sep) {
		return resolved, nil
	}
	return "", fmt.Errorf("%w: %q escapes output dir", ErrOutputPathRejected, path)
}

// evalSymlinksExisting resolves symlinks for path. If path does not exist
// yet, it resolves the deepest existing ancestor and rejoins the missing
// trailing components (so OutputFile can create a new file inside the jail).
func evalSymlinksExisting(path string) (string, error) {
	if _, err := os.Lstat(path); err == nil {
		return filepath.EvalSymlinks(path)
	}
	// Walk up until an existing ancestor is found.
	var missing []string
	cur := path
	for {
		parent := filepath.Dir(cur)
		if parent == cur {
			// Reached root without a resolvable ancestor.
			return "", fmt.Errorf("no existing ancestor for %s", path)
		}
		missing = append([]string{filepath.Base(cur)}, missing...)
		if _, err := os.Lstat(parent); err == nil {
			resolvedParent, err := filepath.EvalSymlinks(parent)
			if err != nil {
				return "", err
			}
			return filepath.Join(append([]string{resolvedParent}, missing...)...), nil
		}
		cur = parent
	}
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
