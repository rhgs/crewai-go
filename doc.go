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
// Orchestration can be sequential (Sequential), hierarchical (Hierarchical),
// or staged (Staged). The staged process groups tasks into stages: stages run
// in sequence, but the tasks within a single stage run concurrently. The
// output of each stage is available as context to the tasks of the following
// stages (via Task.Context). A stage marked Optional does not abort the crew
// when one of its tasks fails; otherwise the first failure aborts Kickoff.
//
//	crew := crewai.NewCrew(agents, nil)
//	crew.Process = crewai.Staged
//	crew.Stages = []crewai.Stage{
//	    {Name: "collect", Tasks: []*crewai.Task{researchA, researchB}},
//	    {Name: "synthesize", Tasks: []*crewai.Task{write}},
//	}
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
// # Agentic loop
//
// By default an agent uses a single-pass ReAct executor. For tasks that
// benefit from self-assessment and iterative refinement, set Agent.Loop (or
// Task.Loop) to an AgenticLoop, which follows a
// Plan-Execute-Evaluate-Refine cycle: the agent plans, executes, evaluates its
// output against the expected output, and refines it up to MaxRefinements
// times. An optional separate evaluator agent (WithEvaluator) can score the
// output independently.
//
//	agent.Loop = crewai.NewAgenticLoop(
//	    crewai.WithMaxRefinements(3),
//	    crewai.WithPassThreshold(80),
//	)
//
// If the output never passes evaluation, Kickoff returns ErrEvaluationFailed;
// if the evaluator returns an unparseable response, ErrInvalidEvaluation.
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
//
// # Web search
//
// The framework supports web search in two complementary patterns:
//
// Agent-driven — the agent's Go code decides what to search, when, and how
// many results to fetch. Any LLM that implements the optional WebSearcher
// interface can be searched directly via the SearchWeb helper:
//
//	type WebSearcher interface {
//	    WebSearch(ctx context.Context, query string, max int) ([]SearchHit, error)
//	}
//
//	hits, err := crewai.SearchWeb(ctx, llm, "Go programming", 5)
//
// SearchHit is a single result with Title, URL, and Content (a short
// snippet, not the full page).
//
// Providers that implement WebSearcher:
//
//   - llm/ollama  — POST /api/web_search (pure search, no model invocation).
//     Works with both Ollama Cloud and local Ollama (if the endpoint is
//     available).
//   - llm/openai  — uses web_search_options (NOT the tools field) with a
//     search-capable model (gpt-4o-search-preview, gpt-5-search-api, etc.).
//     The model is invoked, so this consumes tokens.
//   - llm/anthropic — uses the web_search_20250305 server tool. The model
//     searches and returns web_search_tool_result content blocks.
//     Consumes tokens.
//   - llm/xai     — delegates to the OpenAI-compatible client (same
//     web_search_options wire format). Requires a search-capable model.
//
// If the LLM does not implement WebSearcher, SearchWeb returns
// ErrWebSearchUnsupported.
//
// Model-driven — the WebSearchTool (in the tools package) is a Tool that
// also implements FactSource. It searches the web via a pluggable
// SearchProvider and returns formatted results for the ReAct observation.
// The LLM decides when to search.
//
//	import "github.com/rhgs/crewai-go/tools"
//
//	tool := tools.NewWebSearch(tools.NewWikipediaSearch())
//	agent.WithTools(tool)
//
// Available SearchProvider implementations:
//
//   - Wikipedia (default, free, no API key) — searches Wikipedia articles.
//   - LangSearch (100% free) — general web search, requires API key.
//   - Serpstack (1000 requests/month free) — requires API key.
//   - DuckDuckGo (optional, may be rate-limited or blocked) — no API key.
//   - Google Custom Search (requires API key + CX ID).
//   - Brave Search (requires API key).
//
// All URLs returned by any provider are filtered through SSRF protection:
// non-http(s) schemes, loopback/private/link-local/unspecified addresses are
// blocked, and domain names are resolved via DNS to prevent rebinding attacks.
// Fail-closed: unresolvable hosts are blocked.
//
// # Logging
//
// crewai-go uses log/slog (structured logging) from the standard library.
// Inject a custom *slog.Logger via the WithLogger method on Crew and Agent:
//
// NOTE: Debug logs include the full LLM output and tool inputs; warn logs
// include upstream error messages (which providers may render with request
// details). Pick your log destination accordingly — avoid shared remote
// sinks when prompts/outputs may contain sensitive data.
//
//	crew := crewai.NewCrew(agents, tasks)
//	crew.WithLogger(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
//	    Level: slog.LevelDebug,
//	})))
//
// If no logger is provided, Kickoff creates a text-format logger on stderr.
// The level is LevelDebug when Crew.Verbose is true and LevelError otherwise
// (matching the legacy behavior, where only Verbose=true emitted any logs).
// When WithLogger is used, the injected logger is used as-is — the caller
// controls level, handler, and destination. Agent.Execute, when called
// standalone (without a crew), falls back to slog.Default() unless
// Agent.WithLogger is used.
//
// # MCP (Model Context Protocol)
//
// The subpackage mcp/ exposes tools from external MCP servers as
// crewai.Tool values. The original inputSchema is preserved verbatim
// (the adapter implements SchemaProvider) so native tool calling
// receives the real schema. Configuration can be programmatic
// (mcp.New + mcp.Initialize) or via a JSON file (mcp.LoadConfig).
// See docs/en/mcp.md (or docs/pt-BR/mcp.md) for usage and security
// notes.
//
//	clients, _ := mcp.LoadConfig(ctx, "/etc/mcp.json", "auditor", "1.0.0")
//	var tools []crewai.Tool
//	for _, c := range clients {
//	    ts, _ := c.ListTools(ctx)
//	    for _, t := range ts {
//	        tools = append(tools, mcp.NewToolAdapter(c, t))
//	    }
//	}
//	agent.WithTools(tools...)
//
// # Per-task warnings (graceful degradation)
//
// A task may record non-fatal diagnostics when a secondary source was
// unavailable or a non-critical step failed, while the task itself
// still succeeds. Recorded warnings are aggregated into TaskOutput.Warnings
// and CrewOutput.Warnings in execution order — distinct from Stage.Optional,
// which concerns TASK FAILURE not partial success.
//
// Use Task.AddWarning from the agent code, or crewai.AddWarningFromCtx
// from inside a Tool that has only context.Context to work with:
//
//	crewai.AddWarningFromCtx(ctx, "secondary source timeout")
//
// The Crew executor injects the current Task as a WarningSink into ctx
// before invoking tools; Agent.Execute standalone does not inject a sink
// (calls become silent no-ops).
//
// # Progress observability
//
// Crews emit real-time progress events through a ProgressFunc set via
// Crew.WithProgress. Events include stage_started, stage_completed,
// task_started, task_completed, and tool_invoked (both ReAct and native
// tool calling paths). The callback runs from multiple goroutines when
// stages run in parallel — it MUST be thread-safe, like an slog.Handler.
// A panic inside the callback is recovered and logged; Kickoff continues.
// Progress payloads carry only metadata (Stage, Task, Agent, Event, Tool,
// Duration, Err) — never prompt bodies, LLM outputs, or tool inputs.
//
//	crew.WithProgress(func(p crewai.Progress) {
//	    fmt.Printf("%s/%s/%s tool=%s dur=%s\n",
//	        p.Stage, p.Task, p.Agent, p.Tool, p.Duration)
//	})
package crewai
