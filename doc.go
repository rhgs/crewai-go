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
// # Native tool calling
//
// When an Agent's ToolMode is set to ToolModeNative and its LLM implements
// the ToolCallingLLM interface, the executor uses the provider's native
// function calling API instead of the text-based ReAct protocol. This is
// more reliable and produces structured tool_calls that the executor
// executes directly, without text parsing.
//
// Providers that support ToolCallingLLM: llm/ollama, llm/openai, llm/anthropic.
// Providers that do not: llm/mock (uses a queued response mechanism for tests).
//
// ToolTraces in TaskOutput record each native tool invocation (name, args,
// output, duration, failed) for observability. Facts from FactSource tools
// are collected the same way as in ReAct.
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
//
// # Guardrails
//
// Guardrails are code-enforced post-output validation hooks that block
// publication of outputs violating business invariants. Set Crew.Guardrails
// for crew-level checks (run after all tasks) or Task.Guardrail for
// task-level checks (run at task completion). If a guardrail returns a
// non-nil error, Kickoff returns ErrBlockedByGuardrail and the output is
// never partially returned. Guardrails complement structured output:
// schema checks the shape, guardrails check the meaning.
//
//	crew.Guardrails = []crewai.Guardrail{
//	    func(_ context.Context, out *crewai.CrewOutput) error {
//	        if len(out.Final) < 10 {
//	            return fmt.Errorf("output too short")
//	        }
//	        return nil
//	    },
//	}
//
// # Facts & provenance
//
// A Fact is data from a deterministic connector tool, never from the LLM.
// Use NewFactSourceTool to create a tool that produces facts. Facts are
// collected by the executor after successful tool calls and attached to
// CrewOutput.Facts and TaskOutput.Facts. Use AllFactsProvenanced in a
// guardrail to enforce that every fact has source and payload hash.
//
//	tool := crewai.NewFactSourceTool("cnpj_lookup", "...", fn, factsFn)
//	crew.Guardrails = []crewai.Guardrail{
//	    func(_ context.Context, out *crewai.CrewOutput) error {
//	        return crewai.AllFactsProvenanced(out.Facts)
//	    },
//	}
package crewai
