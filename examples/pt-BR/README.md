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
| `agentic_loop`  | Ciclo Planejar-Executar-Avaliar-Refinar (mock LLM)| ❌ Não                  |
| `tools`         | Agente usando ferramentas via ReAct               | ✅ OpenAI               |
| `xai_oauth`     | Grok por chave de API ou OAuth de assinatura      | ✅ xAI                  |
| `logging`       | `*slog.Logger` customizado com wrapper de redação  | ❌ Não                  |

## Rodando

```bash
# Sem chave de API:
go run ./examples/custom_llm

# Com OpenAI:
export OPENAI_API_KEY=sk-...
go run ./examples/basic
go run ./examples/sequential
go run ./examples/hierarchical
go run ./examples/staged
go run ./examples/agentic_loop   # offline, mock LLM
go run ./examples/tools
```

Para usar outro provedor (Anthropic, Ollama, Groq…), troque a linha de criação
do LLM. Veja [`../../docs/pt-BR/llms.md`](../../docs/pt-BR/llms.md).
