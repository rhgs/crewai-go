# Examples

> **Languages:** **English** (current) · [Português](pt-BR/README.md)

Each subfolder is a standalone executable program.

| Example         | What it demonstrates                              | Needs an API key?       |
|-----------------|---------------------------------------------------|-------------------------|
| `custom_llm`    | A 100% offline custom LLM                         | ❌ No                   |
| `ollama`        | Local or cloud Ollama (native API)                | ❌ Local / ✅ Cloud     |
| `basic`         | One agent, one task                                | ✅ OpenAI               |
| `sequential`    | Agent pipeline + context + memory                 | ✅ OpenAI               |
| `hierarchical`  | A manager delegating tasks dynamically             | ✅ OpenAI               |
| `staged`        | Stages in sequence, tasks within a stage in parallel | ✅ OpenAI            |
| `events`        | `WithEvents` lifecycle JSON lines (offline mock) | ❌ No |
| `streaming`     | `WithStream` deltas + Async Task demux (offline mock) | ❌ No |
| `async_tasks`   | `Task.Async` waves under Sequential + `WithContext` merge (offline mock) | ❌ No / ✅ OpenAI |
| `memory_file`   | `FileStore` JSONL persistence across two Kickoffs (offline) | ❌ No |
| `memory_embed`  | `AutoEmbed` + cosine Query with mock embedder (offline) | ❌ No |
| `memory_tools`  | Opt-in `recall_memory` / `remember` tools (offline) | ❌ No |
| `agentic_loop`  | Plan-Execute-Evaluate-Refine cycle (mock LLM)      | ❌ No                   |
| `tools`         | An agent using tools via ReAct                     | ✅ OpenAI               |
| `tools_http`    | `HTTPFetch` allowlist + SSRF deny (httptest, offline) | ❌ No                |
| `tools_files`   | `FileRead`/`FileWrite` directory jail (offline)    | ❌ No                   |
| `rag_file`      | RAG pattern: FileStore + embedder as a Tool (offline) | ❌ No                |
| `flows_research`| `Flow[S]` Start/Listen/Router (offline)            | ❌ No                   |
| `declarative`   | `LoadCrewFile` JSON-subset + Build (offline)       | ❌ No                   |
| `trace_export`  | `TraceRecorder` JSONL metadata-only (offline)      | ❌ No                   |
| `native_tools`  | Provider-native function calling (`ToolModeNative`) | ✅ OpenAI / offline wiring |
| `facts`         | `Fact` provenance from deterministic tools         | ❌ No                   |
| `guardrails`    | Post-output `Guardrail` blocking invalid results   | ❌ No                   |
| `xai_oauth`     | Grok via API key or subscription OAuth             | ✅ xAI                  |
| `logging`       | Custom `*slog.Logger` with `RedactHandler`           | ❌ No                   |
| `delegation`    | Inter-agent `delegate_to_coworker` tool (opt-in)   | ✅ OpenAI / wiring only |
| `mcp`           | MCP client wiring (`FilterTools`, description limit) | ❌ wiring / ✅ live MCP |

## Running

```bash
# Without an API key:
go run ./examples/custom_llm
go run ./examples/flows_research  # Flow[S] offline demo
go run ./examples/declarative  # LoadCrewFile JSON-subset (offline)
go run ./examples/trace_export  # TraceRecorder JSONL (offline)

# With OpenAI:
export OPENAI_API_KEY=sk-...
go run ./examples/basic
go run ./examples/sequential
go run ./examples/hierarchical
go run ./examples/staged
go run ./examples/events      # WithEvents JSONL (offline)
go run ./examples/streaming  # offline mock stream demux
go run ./examples/async_tasks  # offline mock; USE_OPENAI=1 for live
go run ./examples/memory_file  # FileStore JSONL; MEMORY_DIR optional
go run ./examples/memory_embed # AutoEmbed + cosine (mock)
go run ./examples/memory_tools # recall_memory / remember (offline)
go run ./examples/agentic_loop   # offline, mock LLM
go run ./examples/tools
go run ./examples/tools_http   # HTTPFetch httptest (offline)
go run ./examples/tools_files  # FileRead/FileWrite jail (offline)
go run ./examples/rag_file     # RAG pattern FileStore (offline)
go run ./examples/logging      # offline redaction demo
go run ./examples/delegation   # wiring; live with OPENAI_API_KEY
go run ./examples/mcp          # wiring; live with MCP_ENDPOINT
```

To use another provider (Anthropic, Ollama, Groq…), swap the LLM creation line.
See [`../docs/llms.md`](../docs/llms.md).

