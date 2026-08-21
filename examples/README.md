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
| `async_tasks`   | `Task.Async` waves under Sequential + `WithContext` merge (offline mock) | ❌ No / ✅ OpenAI |
| `memory_file`   | `FileStore` JSONL persistence across two Kickoffs (offline) | ❌ No |
| `memory_embed`  | `AutoEmbed` + cosine Query with mock embedder (offline) | ❌ No |
| `agentic_loop`  | Plan-Execute-Evaluate-Refine cycle (mock LLM)      | ❌ No                   |
| `tools`         | An agent using tools via ReAct                     | ✅ OpenAI               |
| `xai_oauth`     | Grok via API key or subscription OAuth             | ✅ xAI                  |
| `logging`       | Custom `*slog.Logger` with `RedactHandler`           | ❌ No                   |
| `delegation`    | Inter-agent `delegate_to_coworker` tool (opt-in)   | ✅ OpenAI / wiring only |
| `mcp`           | MCP client wiring (`FilterTools`, description limit) | ❌ wiring / ✅ live MCP |

## Running

```bash
# Without an API key:
go run ./examples/custom_llm

# With OpenAI:
export OPENAI_API_KEY=sk-...
go run ./examples/basic
go run ./examples/sequential
go run ./examples/hierarchical
go run ./examples/staged
go run ./examples/async_tasks  # offline mock; USE_OPENAI=1 for live
go run ./examples/memory_file  # FileStore JSONL; MEMORY_DIR optional
go run ./examples/memory_embed # AutoEmbed + cosine (mock)
go run ./examples/agentic_loop   # offline, mock LLM
go run ./examples/tools
go run ./examples/logging      # offline redaction demo
go run ./examples/delegation   # wiring; live with OPENAI_API_KEY
go run ./examples/mcp          # wiring; live with MCP_ENDPOINT
```

To use another provider (Anthropic, Ollama, Groq…), swap the LLM creation line.
See [`../docs/llms.md`](../docs/llms.md).

