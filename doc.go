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
// implementations live in the llm/openai, llm/anthropic, llm/ollama,
// llm/xai, and llm/mock subpackages.
//
// # Native tool calling
//
// When an Agent's ToolMode is set to ToolModeNative and its LLM implements
// the ToolCallingLLM interface, the executor uses the provider's native
// function calling API instead of the text-based ReAct protocol. This is
// more reliable and produces structured tool_calls that the executor
// executes directly, without text parsing.
//
// Providers that support ToolCallingLLM: llm/ollama, llm/openai,
// llm/anthropic, llm/xai. Providers that do not: llm/mock (uses a queued
// response mechanism for tests).
//
// ToolTraces in TaskOutput record each native tool invocation (name, args,
// output, duration, failed) for observability. Facts from FactSource tools
// are collected the same way as in ReAct. Tool results carry ToolCallID so
// providers that require it (OpenAI tool_call_id, Anthropic tool_use_id)
// can match a result back to the call.
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
// The built-in validator is a stdlib-only subset of JSON Schema: type,
// properties, required, enum, items, additionalProperties, minLength/
// maxLength (bytes), minimum/maximum/exclusive*, minItems/maxItems,
// pattern, and oneOf/anyOf/allOf. WithStrictSchema rejects unsupported
// keywords ($ref, if/then/else, format, ...) at NewStructuredOutput time.
//
// WithAllowTools() runs a bounded tool gather phase (ReAct or native)
// before JSON capture; FactSource facts are preserved. If gather hits
// MaxIterations without a clean stop, the task records a warning and
// still proceeds to capture.
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
// # Memory
//
// Setting Crew.Memory = true enables short-term, in-RAM memory: later tasks
// with no explicit Task.Context receive the accumulated outputs of earlier
// tasks. MemorySnapshot returns that short-term *Memory.
//
// *Memory also implements MemoryStore, the long-term memory contract used
// by pluggable backends. MemoryEntry adds ID/Scope/CreatedAt/Metadata/
// Embedding on top of MemoryRecord; MemoryQuery bounds recall (Limit, MaxChars,
// hard cap MaxMemoryQueryLimit; Content is capped at MaxMemoryEntryBytes on
// Put). Query searches Content/Task case-insensitively and returns the latest
// entries first when Text is empty. The Crew does not consume MemoryStore yet
// (MemoryPolicy integration and the D-M7 commit barrier arrive with M2); the
// interface lets applications depend on a stable backend contract today.
//
//	var store crewai.MemoryStore = mem // *crewai.Memory
//	hits, _ := store.Query(ctx, crewai.MemoryQuery{Text: "revenue", Limit: 5})
//
// # Logging
//
// crewai-go uses log/slog (structured logging) from the standard library.
// Inject a custom *slog.Logger via the WithLogger method on Crew and Agent:
//
// NOTE: Debug logs include the full LLM output and tool inputs; warn logs
// include upstream error messages (which providers may render with request
// details). Prefer LevelInfo/LevelError in production, or wrap the handler
// with RedactHandler to mask likely API keys, Bearer tokens, and query-string
// secrets (best-effort, opt-in):
//
//	log := slog.New(crewai.RedactHandler(
//	    slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}),
//	))
//	crew := crewai.NewCrew(agents, tasks).WithLogger(log)
//
// If no logger is provided, Kickoff creates a text-format logger on stderr.
// The level is LevelDebug when Crew.Verbose is true and LevelError otherwise
// (matching the legacy behavior, where only Verbose=true emitted any logs).
// When WithLogger is used, the injected logger is used as-is — the caller
// controls level, handler, and destination. Agent.Execute, when called
// standalone (without a crew), falls back to slog.Default() unless
// Agent.WithLogger is used.
//
// Only one Kickoff may run at a time on a given *Crew; a concurrent call
// returns ErrCrewRunning. Task.OutputFile paths are cleaned; optional
// Task.OutputDir / Crew.OutputDir jails writes (symlink-aware, fail closed).
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
// The HTTP client defaults to DefaultHTTPTimeout (30s); override with
// WithHTTPTimeout, WithHTTPClient, or JSON servers[].timeout. Prefer
// FilterTools and WithDescriptionLimit when attaching catalogs.
//
//	clients, _ := mcp.LoadConfig(ctx, "/etc/mcp.json", "auditor", "1.0.0")
//	var tools []crewai.Tool
//	for _, c := range clients {
//	    ts, _ := c.ListTools(ctx)
//	    ts = mcp.FilterTools(ts, map[string]struct{}{"search_docs": {}})
//	    for _, t := range ts {
//	        tools = append(tools, mcp.NewToolAdapter(c, t, mcp.WithDescriptionLimit(500)))
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
//
// # Structured output via tool-call
//
// When a StructuredOutput is configured with WithToolCall(), the executor
// declares a synthetic ToolSpec named "emit_result" whose Parameters are
// the JSON Schema, instructs the model to call it exactly once, and
// parses the call's arguments as the validated output. This is the
// reliable path for providers (notably Ollama Cloud) that do not honour
// structured outputs via the `format` parameter.
//
//	structured, _ := crewai.NewStructuredOutput(schema, crewai.WithToolCall())
//	task.Structured = structured
//
// Tool-call mode requires the agent's LLM to implement ToolCallingLLM;
// otherwise it returns ErrToolCallStructuredUnsupported. The default
// JSON-only mode remains unchanged for backward compatibility.
//
// # Inter-agent delegation
//
// NewDelegationTool(roster) exposes the delegate_to_coworker tool so an
// agent can ask a peer mid-reasoning. Targets must set AllowDelegation.
// Nested calls honor DefaultMaxDelegationDepth with cycle/self guards.
// Crew.EnableDelegationTool (default false) auto-attaches the tool at
// Kickoff; Crew implements DelegationRoster via PeerAgents().
//
//	researcher.AllowDelegation = true
//	crew.EnableDelegationTool = true
//	// or: writer.WithTools(crewai.NewDelegationTool(crew))
package crewai
