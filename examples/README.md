# Exemplos

Cada subpasta é um programa executável independente.

| Exemplo         | O que demonstra                                   | Precisa de API?         |
|-----------------|---------------------------------------------------|-------------------------|
| `custom_llm`    | Um LLM customizado 100% offline                   | ❌ Não                  |
| `ollama`        | Ollama local ou cloud (API nativa)                | ❌ Local / ✅ Cloud     |
| `basic`         | Um agente, uma tarefa                             | ✅ OpenAI               |
| `sequential`    | Pipeline de agentes + contexto + memória          | ✅ OpenAI               |
| `hierarchical`  | Gerente delegando tarefas dinamicamente           | ✅ OpenAI               |
| `tools`         | Agente usando ferramentas via ReAct               | ✅ OpenAI               |
| `xai_oauth`     | Grok por chave de API ou OAuth de assinatura      | ✅ xAI                  |

## Rodando

```bash
# Sem chave de API:
go run ./examples/custom_llm

# Com OpenAI:
export OPENAI_API_KEY=sk-...
go run ./examples/basic
go run ./examples/sequential
go run ./examples/hierarchical
go run ./examples/tools
```

Para usar outro provedor (Anthropic, Ollama, Groq…), troque a linha de criação
do LLM. Veja [`../docs/llms.md`](../docs/llms.md).
