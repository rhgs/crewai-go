package crewai

import (
	"context"
	"fmt"
)

// Guardrail is a post-output validation hook. It receives the crew output
// and returns nil if the output is safe to publish, or a descriptive error
// identifying what failed.
//
// Guardrails are pure validation: they MUST NOT mutate the output. They are
// code-enforced checks that run after the LLM produces output, not
// prompt-level instructions. When a guardrail returns a non-nil error,
// Kickoff returns ErrBlockedByGuardrail and the output is never published.
//
// Guardrails complement structured-output schema validation: the schema
// checks the SHAPE (types, required fields), guardrails check the MEANING
// (business invariants, provenance, anti-hallucination rules).
type Guardrail func(ctx context.Context, out *CrewOutput) error

// WithGuardrails returns a functional option that adds crew-level guardrails
// to a Crew. The guardrails run after all tasks complete, in the order
// provided.
func WithGuardrails(guards ...Guardrail) func(*Crew) {
	return func(c *Crew) { c.Guardrails = append(c.Guardrails, guards...) }
}

// runTaskGuardrail runs the task-level guardrail (if any) against the
// task's output. It returns nil if the guardrail passes or is not set,
// or an error wrapping ErrBlockedByGuardrail if it fails.
//
// The guardrail receives a CrewOutput populated with only this task's
// output: Final is the task result and TasksOutput contains a single
// entry for this task.
func runTaskGuardrail(ctx context.Context, task *Task, label, result string) error {
	if task.Guardrail == nil {
		return nil
	}
	out := &CrewOutput{
		Final: result,
		TasksOutput: []TaskOutput{{
			Task:   label,
			Output: result,
		}},
	}
	if err := task.Guardrail(ctx, out); err != nil {
		return fmt.Errorf("%w: task %q: %v", ErrBlockedByGuardrail, label, err)
	}
	return nil
}

// runCrewGuardrails runs all crew-level guardrails in order against the
// full CrewOutput. It returns nil if all pass, or an error wrapping
// ErrBlockedByGuardrail on the first failure (short-circuit).
func runCrewGuardrails(ctx context.Context, guards []Guardrail, out *CrewOutput) error {
	for _, g := range guards {
		if err := g(ctx, out); err != nil {
			return fmt.Errorf("%w: %v", ErrBlockedByGuardrail, err)
		}
	}
	return nil
}
