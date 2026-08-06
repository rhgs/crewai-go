// Package crewai is an idiomatic Go port of the CrewAI framework for
// orchestrating autonomous, collaborative AI agents.
//
// The framework revolves around four main types:
//
//   - Agent: a worker with a role, a goal, a backstory, an LLM, and tools.
//   - Task:  a unit of work with a description, an expected output, and an assignee.
//   - Crew:  groups agents and tasks and orchestrates them according to a Process.
//   - Tool:  a capability the agent can invoke during reasoning.
//
// An LLM is any type implementing the LLM interface; ready-made
// implementations live in the llm/openai, llm/anthropic, and llm/mock
// subpackages.
//
// Minimal example:
//
//	llm := openai.New("gpt-4o-mini")
//	agent := crewai.NewAgent("Poet", "Write poems", "...", llm)
//	task := crewai.NewTask("Write a haiku about Go.", "3 lines", agent)
//	crew := crewai.NewCrew([]*crewai.Agent{agent}, []*crewai.Task{task})
//	out, err := crew.Kickoff(context.Background(), nil)
//
// Orchestration can be sequential (Sequential) or hierarchical
// (Hierarchical), the latter with a manager that delegates tasks dynamically.
//
// # Structured output
//
// When a task needs typed, trustworthy data, set Task.Structured to a
// StructuredOutput configured with a JSON Schema. The executor instructs
// the model to reply with JSON only, validates the output in Go, and retries
// up to RepairMax times if validation fails. On success, Task.Output
// returns the canonicalized JSON string; on failure, it returns
// ErrRepairBudgetExceeded. The executor never returns invalid JSON or
// invented data.
//
//	schema := map[string]any{
//	    "type": "object",
//	    "properties": map[string]any{
//	        "name": map[string]any{"type": "string"},
//	    },
//	    "required": []string{"name"},
//	}
//	structured, _ := crewai.NewStructuredOutput(schema, crewai.WithRepairMax(3))
//	task.Structured = structured
//
// The built-in validator supports a subset of JSON Schema (type, properties,
// required, enum, items) and uses only the standard library.
package crewai
