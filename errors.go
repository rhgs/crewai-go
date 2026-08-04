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
)
