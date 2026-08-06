package crewai

import "errors"

var (
	// ErrNoLLM is returned when an agent is executed without a configured LLM.
	ErrNoLLM = errors.New("crewai: agent without a configured LLM")
	// ErrNoAgent is returned when a task has no assigned agent and the crew
	// cannot resolve a responsible one.
	ErrNoAgent = errors.New("crewai: task without an assigned agent")
	// ErrNoTasks is returned when a crew is started with no tasks.
	ErrNoTasks = errors.New("crewai: crew without tasks")
	// ErrNoManager is returned when the hierarchical process is used without
	// a ManagerLLM or a ManagerAgent.
	ErrNoManager = errors.New("crewai: hierarchical process requires ManagerLLM or ManagerAgent")
	// ErrMaxIterations is returned when the agent reaches the maximum number
	// of iterations without producing a final answer.
	ErrMaxIterations = errors.New("crewai: maximum number of iterations reached")
	// ErrInvalidOutput is returned when the model returns JSON that does
	// not validate against the required schema. It wraps the underlying
	// validation error(s).
	ErrInvalidOutput = errors.New("crewai: structured output failed schema validation")
	// ErrRepairBudgetExceeded is returned when repair attempts are
	// exhausted and the model never produced valid JSON. It wraps the
	// last validation error.
	ErrRepairBudgetExceeded = errors.New("crewai: repair budget exceeded, structured output could not be validated")
)
