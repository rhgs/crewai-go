package crewai

// Process defines the strategy for orchestrating tasks in a crew.
type Process string

const (
	// Sequential executes the tasks in the order they were defined, passing
	// each task's output as context to the following ones.
	Sequential Process = "sequential"

	// DAG is an alias for Sequential (D-A7): naming sugar for crews that
	// schedule Task.Async + Task.Context as a DAG with barrier+fold.
	// It does not change scheduling. Pair with Crew.WithAsyncAll when every
	// task should be wave-eligible. Staged is a separate process.
	DAG = Sequential

	// Hierarchical uses a manager agent (or a ManagerLLM) to coordinate the
	// execution, deciding which agent runs each task.
	Hierarchical Process = "hierarchical"

	// Staged groups tasks into stages: stages run in sequence, but the
	// tasks within a single stage run concurrently. The output of each
	// stage feeds the following ones.
	Staged Process = "staged"
)

// valid reports whether the process is recognized.
func (p Process) valid() bool {
	switch p {
	case Sequential, Hierarchical, Staged:
		return true
	default:
		return false
	}
}
