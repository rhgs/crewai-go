# Examples

> **Languages:** **English** (current) · [Português](pt-BR/README.md)

> **Languages:** [**English**](pt-BR/README.md) · [Português](pt-BR/README.md)


Each subfolder is a standalone executable program.

| Example         | What it demonstrates                              | Needs an API key?       |
|-----------------|---------------------------------------------------|-------------------------|
| `custom_llm`    | A 100% offline custom LLM                         | ❌ No                   |
| `ollama`        | Local or cloud Ollama (native API)                | ❌ Local / ✅ Cloud     |
| `basic`         | One agent, one task                                | ✅ OpenAI               |
| `sequential`    | Agent pipeline + context + memory                 | ✅ OpenAI               |
| `hierarchical`  | A manager delegating tasks dynamically             | ✅ OpenAI               |
| `tools`         | An agent using tools via ReAct                     | ✅ OpenAI               |
| `xai_oauth`     | Grok via API key or subscription OAuth             | ✅ xAI                  |

## Running

```bash
# Without an API key:
go run ./examples/custom_llm

# With OpenAI:
export OPENAI_API_KEY=sk-...
go run ./examples/basic
go run ./examples/sequential
go run ./examples/hierarchical
go run ./examples/tools
```

To use another provider (Anthropic, Ollama, Groq…), swap the LLM creation line.
See [`../docs/llms.md`](../docs/llms.md).