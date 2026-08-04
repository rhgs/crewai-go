package crewai

// Process defines the strategy for orchestrating tasks in a crew.
type Process string

const (
	// Sequential executes the tasks in the order they were defined, passing
	// each task's output as context to the following ones.
	Sequential Process = "sequential"

	// Hierarchical uses a manager agent (or a ManagerLLM) to coordinate the
	// execution, deciding which agent runs each task.
	Hierarchical Process = "hierarchical"
)

// valid reports whether the process is recognized.
func (p Process) valid() bool {
	switch p {
	case Sequential, Hierarchical:
		return true
	default:
		return false
	}
}
