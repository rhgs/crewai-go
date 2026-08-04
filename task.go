package crewai

import (
	"os"
	"strings"
	"sync"
)

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

	mu     sync.RWMutex
	output string
	done   bool
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
