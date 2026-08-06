package crewai

import "context"

const defaultMaxIterations = 15

// Agent is an autonomous worker with a role, a goal, and a backstory.
// It uses an LLM to reason and, optionally, tools to act, executing the tasks
// the crew assigns to it.
//
// The zero value is not usable; create agents with NewAgent or by filling the
// required fields (Role and LLM) directly.
type Agent struct {
	// Role is the agent's role/function (e.g. "Senior Researcher").
	Role string
	// Goal is the personal goal that guides the agent's decisions.
	Goal string
	// Backstory gives context and personality to the agent.
	Backstory string

	// LLM is the language model used by the agent. Required.
	LLM LLM

	// Tools are the tools the agent can use for any task.
	Tools []Tool

	// MaxIterations limits the number of reasoning/tool-use cycles per task.
	// If <= 0, the default (15) is used.
	MaxIterations int

	// AllowDelegation enables this agent to be considered as a manager in the
	// hierarchical process and to delegate work (informational in this version).
	AllowDelegation bool
}

// NewAgent creates an agent with the essential fields filled in.
func NewAgent(role, goal, backstory string, llm LLM) *Agent {
	return &Agent{
		Role:      role,
		Goal:      goal,
		Backstory: backstory,
		LLM:       llm,
	}
}

// WithTools adds tools to the agent and returns the agent itself for fluent
// chaining.
func (a *Agent) WithTools(tools ...Tool) *Agent {
	a.Tools = append(a.Tools, tools...)
	return a
}

// Execute runs a standalone task with this agent and returns the output. It is
// useful for using an agent outside a crew or in tests.
func (a *Agent) Execute(ctx context.Context, t *Task) (string, error) {
	out, _, err := executeTask(ctx, a, t, "", nopLogger{})
	return out, err
}
