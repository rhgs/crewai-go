# Exemplos

> **Languages:** [English](../README.md) · **Português** (atual)

Cada subpasta é um programa executável independente.

| Exemplo         | O que demonstra                                   | Precisa de API?         |
|-----------------|---------------------------------------------------|-------------------------|
| `custom_llm`    | Um LLM customizado 100% offline                   | ❌ Não                  |
| `ollama`        | Ollama local ou cloud (API nativa)                | ❌ Local / ✅ Cloud     |
| `basic`         | Um agente, uma tarefa                             | ✅ OpenAI               |
| `sequential`    | Pipeline de agentes + contexto + memória          | ✅ OpenAI               |
| `hierarchical`  | Gerente delegando tarefas dinamicamente           | ✅ OpenAI               |
| `staged`        | Estágios em sequência, tarefas de um estágio em paralelo | ✅ OpenAI        |
| `streaming`     | Deltas `WithStream` + demux de Task Async (mock offline) | ❌ Não |
| `async_tasks`   | Waves `Task.Async` sob Sequential + merge com `WithContext` (mock offline) | ❌ Não / ✅ OpenAI |
| `memory_file`   | Persistência `FileStore` JSONL entre dois Kickoffs (offline) | ❌ Não |
| `memory_embed`  | `AutoEmbed` + Query por cosseno com embedder mock (offline) | ❌ Não |
| `agentic_loop`  | Ciclo Planejar-Executar-Avaliar-Refinar (mock LLM)| ❌ Não                  |
| `tools`         | Agente usando ferramentas via ReAct               | ✅ OpenAI               |
| `native_tools`  | Function calling nativo do provedor (`ToolModeNative`) | ✅ OpenAI / wiring offline |
| `facts`         | Proveniência `Fact` via tools determinísticas      | ❌ Não                  |
| `guardrails`    | `Guardrail` pós-saída bloqueando resultados inválidos | ❌ Não               |
| `xai_oauth`     | Grok por chave de API ou OAuth de assinatura      | ✅ xAI                  |
| `logging`       | `*slog.Logger` customizado com `RedactHandler`     | ❌ Não                  |
| `delegation`    | Tool inter-agent `delegate_to_coworker` (opt-in)   | ✅ OpenAI / so wiring   |
| `mcp`           | Wiring do client MCP (`FilterTools`, limit de description) | ❌ wiring / ✅ MCP live |

## Rodando

```bash
# Sem chave de API:
go run ./examples/custom_llm
go run ./examples/streaming  # demux de stream mock offline
go run ./examples/async_tasks  # waves Task.Async; USE_OPENAI=1 para live
go run ./examples/memory_file  # FileStore JSONL; MEMORY_DIR opcional
go run ./examples/memory_embed # AutoEmbed + cosseno (mock)
go run ./examples/agentic_loop # offline, mock LLM
go run ./examples/logging      # demo offline de redacao
go run ./examples/mcp          # wiring; live com MCP_ENDPOINT

# Com OpenAI:
export OPENAI_API_KEY=sk-...
go run ./examples/basic
go run ./examples/sequential
go run ./examples/hierarchical
go run ./examples/staged
go run ./examples/tools
go run ./examples/delegation   # wiring; live com OPENAI_API_KEY
```

Para usar outro provedor (Anthropic, Ollama, Groq…), troque a linha de criação
do LLM. Veja [`../../docs/pt-BR/llms.md`](../../docs/pt-BR/llms.md).
