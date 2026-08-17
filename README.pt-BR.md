# crewai-go

> **Languages:** [English](README.md) · **Português** (atual)

[![Go Reference](https://pkg.go.dev/badge/github.com/rhgs/crewai-go.svg)](https://pkg.go.dev/github.com/rhgs/crewai-go)

[![CI](https://github.com/rhgs/crewai-go/actions/workflows/ci.yml/badge.svg)](https://github.com/rhgs/crewai-go/actions/workflows/ci.yml)

[![Release](https://img.shields.io/github/v/release/rhgs/crewai-go?label=release)](https://github.com/rhgs/crewai-go/releases)

[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](https://github.com/rhgs/crewai-go/blob/main/LICENSE)

<p align="center">
  <img src="gopher.jpg" alt="capa do crewai-go" width="600">
</p>

**Orquestração de agentes de IA autônomos e colaborativos, em Go.**

`crewai-go` é um port idiomático em Go do framework [CrewAI](https://github.com/crewAIInc/crewAI). Ele permite montar equipes (_crews_) de agentes com papéis distintos que colaboram — de forma sequencial ou hierárquica — para concluir tarefas complexas usando modelos de linguagem (LLMs).

> Feito com **zero dependências externas** — apenas a biblioteca padrão do Go. Fácil de instalar, auditar e integrar.

---

## Sumário

- [Por que crewai-go](#por-que-crewai-go)
- [Conceitos](#conceitos)
- [Instalação](#instalação)
- [Início rápido](#início-rápido)
- [Provedores de LLM](#provedores-de-llm)
- [Ferramentas](#ferramentas)
- [Native tool calling](#native-tool-calling)
- [Web search](#web-search)
- [Logging](#logging)
- [Processos: sequencial e hierárquico](#processos-sequencial-e-hierárquico)
- [Saida estruturada](#saida-estruturada)
- [Guardrails](#guardrails)
- [Facts e proveniência](#facts-e-proveniência)
- [Memória](#memória)
- [Exemplos](#exemplos)
- [Documentação](#documentação)
- [Testes](#testes)
- [Comparação com o CrewAI (Python)](#comparação-com-o-crewai-python)
- [Licença](#licença)

---

## Por que crewai-go

- 🧩 **API simples e composável** — `Agent`, `Task`, `Crew`, `Tool`, `LLM`.
- ⚡ **Sem dependências** — só stdlib; builds pequenos e rápidos.
- 🔌 **Qualquer LLM** — OpenAI (e compatíveis: Ollama, Groq, Azure…), Anthropic (Claude) e qualquer implementação sua da interface `LLM`.
- 🛠️ **Ferramentas via ReAct** — agentes raciocinam e chamam ferramentas em texto.
- 📋 **Saída estruturada** — tarefas podem exigir JSON validado contra um JSON Schema, com loop de reparo limitado.
- 🛡️ **Guardrails** — validação pós-saída em código que bloqueia publicação de saídas que violam invariantes de negócio.
- 📌 **Facts e proveniência** — tipo Fact de primeira classe, populado apenas por ferramentas conectoras determinísticas, nunca pelo LLM, com metadados de proveniência completos.
- 🔧 **Native tool calling** — use function calling nativa do provedor (OpenAI, Anthropic, Ollama) em vez de ReAct baseado em texto, com fallback automático e observabilidade de traces.
- 🔍 **Web search** — busque a web via `WebSearcher` (Ollama, OpenAI, Anthropic, xAI) com `crewai.SearchWeb`, ou via `WebSearchTool` no loop ReAct com 7 provedores (Wikipedia, LangSearch, Serpstack, DuckDuckGo, Google, Brave) e proteção SSRF.
- 📝 **Logging estruturado** via `log/slog` da stdlib — injetar `*slog.Logger` customizado em `Crew` e `Agent`, com fallback para o `Verbose` legado.
- 🧠 **Memória** entre tarefas e **contexto** encadeável.
- 👔 **Processo hierárquico** com gerente que delega dinamicamente.
- 🔁 **Agentic loop** — ciclo opcional Planejar-Executar-Avaliar-Refinar com autoavaliação e refinamento iterativo.
- ✅ **Testável** — LLM mock incluído; ~90% de cobertura no núcleo.

## Conceitos

| Conceito     | O que é                                                                 |
|--------------|-------------------------------------------------------------------------|
| **Agent**    | Um trabalhador com papel, objetivo, história, um LLM e ferramentas.     |
| **Task**     | Uma unidade de trabalho com descrição, saída esperada e responsável.    |
| **Crew**     | A equipe: agrupa agentes e tarefas e as orquestra.                      |
| **Process**  | Estratégia de execução: `Sequential` ou `Hierarchical`.                 |
| **Tool**     | Uma capacidade que o agente pode invocar (cálculo, busca, API…).        |
| **LLM**      | Abstração do modelo de linguagem. Vários provedores prontos.            |
| **Memory**   | Armazena saídas de tarefas para dar contexto às seguintes.             |
| **StructuredOutput** | Configura uma tarefa para exigir JSON validado por um JSON Schema. |
| **Guardrail** | Hook de validacao pos-saida que bloqueia publicacao de saidas invalidas. |
| **Fact** | Dado de um conector deterministico com proveniencia (fonte, hash). |
| **FactSource** | Interface opcional para tools que produzem Facts. |
| **ToolMode** | Estrategia de execucao de tools: `"react"` (padrao) ou `"native"`. |
| **ToolCallingLLM** | Interface LLM opcional para function calling nativo. |
| **ToolTrace** | Registra cada chamada nativa de tool (nome, args, output, duracao). |

## Instalação

Requer **Go 1.24+**.

```bash
go get github.com/rhgs/crewai-go@latest
```

No seu código:

```go
import "github.com/rhgs/crewai-go"
```

## Início rápido

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/rhgs/crewai-go"
	"github.com/rhgs/crewai-go/llm/openai"
)

func main() {
	// 1. Escolha um LLM (usa OPENAI_API_KEY do ambiente).
	llm := openai.New("gpt-4o-mini")

	// 2. Crie um agente.
	poeta := crewai.NewAgent(
		"Poeta",
		"Escrever poemas curtos e memoráveis",
		"Você é um poeta premiado, mestre da concisão.",
		llm,
	)

	// 3. Defina uma tarefa.
	tarefa := crewai.NewTask(
		"Escreva um haikai sobre a linguagem Go.",
		"Um haikai (3 versos) em português.",
		poeta,
	)

	// 4. Monte a crew e execute.
	crew := crewai.NewCrew([]*crewai.Agent{poeta}, []*crewai.Task{tarefa})
	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(out.Final)
}
```

```bash
export OPENAI_API_KEY=sk-...
go run .
```

## Provedores de LLM

Qualquer tipo que implemente a interface abaixo funciona:

```go
type LLM interface {
	Call(ctx context.Context, messages []Message) (string, error)
	Model() string
}
```

Prontos para uso:

```go
import (
	"github.com/rhgs/crewai-go/llm/openai"
	"github.com/rhgs/crewai-go/llm/anthropic"
	"github.com/rhgs/crewai-go/llm/ollama"
	"github.com/rhgs/crewai-go/llm/xai"
	"github.com/rhgs/crewai-go/llm/mock" // para testes
)

// OpenAI (e compatíveis: Groq, Azure, Together…)
llm := openai.New("gpt-4o-mini")

// Anthropic (Claude)
llm := anthropic.New("claude-sonnet-5")

// Ollama local (sem chave)
llm := ollama.New("llama3.2")

// Ollama Cloud (usa OLLAMA_API_KEY)
llm := ollama.NewCloud("gpt-oss:120b")

// xAI (Grok) por chave de API
llm := xai.New("grok-4")

// xAI (Grok) por OAuth de assinatura (SuperGrok / X Premium) — sem chave cobrada
df := xai.NewDeviceFlow(clientID)
ts, _ := xai.LoadTokenSource("~/.crewai-xai-token.json", df)
llm := xai.NewWithOAuth("grok-4", ts)
```

| Provedor | Pacote | Autenticação |
|----------|--------|--------------|
| OpenAI (e compatíveis) | `llm/openai` | `OPENAI_API_KEY` / `WithTokenSource` |
| Anthropic (Claude) | `llm/anthropic` | `ANTHROPIC_API_KEY` |
| Ollama local | `llm/ollama` (`New`) | nenhuma |
| Ollama Cloud | `llm/ollama` (`NewCloud`) | `OLLAMA_API_KEY` |
| xAI (Grok) — API key | `llm/xai` (`New`) | `XAI_API_KEY` |
| xAI (Grok) — assinatura | `llm/xai` (`NewWithOAuth`) | OAuth Device Flow |
| Mock (testes) | `llm/mock` | nenhuma |

Veja [`docs/llms.md`](docs/llms.md) para todas as opções.

## Ferramentas

Crie uma ferramenta a partir de qualquer função Go:

```go
busca := crewai.NewTool(
	"busca_web",
	"Busca um termo na web. Entrada: o termo de busca.",
	func(ctx context.Context, termo string) (string, error) {
		// ... sua lógica ...
		return resultado, nil
	},
)

agente.WithTools(busca)
```

Ferramentas embutidas no pacote `tools`:

```go
import "github.com/rhgs/crewai-go/tools"

agente.WithTools(
	tools.Calculator(),        // avalia expressões aritméticas
	tools.CurrentTime(""),     // data/hora atual
	tools.WordCount(),         // conta palavras/caracteres
)
```

O agente usa ferramentas via o protocolo **ReAct** (`Thought → Action → Action Input → Observation → Final Answer`). Detalhes em [`docs/tools.md`](docs/tools.md).

## Native tool calling

Para provedores que suportam function calling nativo (OpenAI, Anthropic, Ollama), defina `Agent.ToolMode` para usar o tool calling embutido do provedor em vez do ReAct baseado em texto:

```go
llm := ollama.New("llama3.2")

agent := crewai.NewAgent("Pesquisador", "Encontrar respostas", "Voce e um pesquisador.", llm)
agent.ToolMode = crewai.ToolModeNative
agent.WithTools(
    tools.Calculator(),
    tools.CurrentTime(""),
)

task := crewai.NewTask("Quanto e 15% de 200?", "Uma resposta curta.", agent)
crew := crewai.NewCrew([]*crewai.Agent{agent}, []*crewai.Task{task})
out, _ := crew.Kickoff(context.Background(), nil)

// ToolTraces registram cada chamada nativa de tool para observabilidade.
for _, trace := range out.TasksOutput[0].ToolTraces {
    fmt.Printf("%s(%s) -> %s\n", trace.Tool, string(trace.Args), trace.Output)
}
```

Quando `ToolMode` e `"native"` mas o LLM nao implementa `ToolCallingLLM`, o executor retorna `ErrNativeToolsUnsupported`. O padrao (`""` ou `"react"`) usa o loop ReAct existente — sem mudancas no codigo existente. Detalhes em [`docs/tools.md`](docs/tools.md).

## Web search

Web search esta disponivel em dois padroes — agent-driven (codigo Go controla as queries) e model-driven (o LLM decide quando buscar via loop ReAct).

### Agent-driven: interface `WebSearcher`

Para provedores LLM que tem API nativa de web search (Ollama Cloud, OpenAI, Anthropic, xAI), chame `SearchWeb` diretamente do codigo Go:

```go
import "github.com/rhgs/crewai-go"

// OpenAI requer um modelo de busca (ex: gpt-4o-search-preview).
llm := openai.New("gpt-4o-search-preview")

hits, err := crewai.SearchWeb(ctx, llm, "Go programming language", 5)
if err != nil {
    // Retorna ErrWebSearchUnsupported se o LLM nao implementa WebSearcher.
    log.Fatal(err)
}
for _, hit := range hits {
    fmt.Printf("%s -- %s\n%s\n\n", hit.Title, hit.URL, hit.Content)
}
```

| Provedor | Como funciona |
|----------|--------------|
| **Ollama Cloud** | `POST /api/web_search` — endpoint puro de busca, sem invocar o modelo, sem consumir tokens |
| **OpenAI** | `web_search_options` no Chat Completions (NAO `tools`); requer modelos de busca (`gpt-4o-search-preview`, `gpt-5-search-api`); resultados como annotations `url_citation` aninhadas |
| **Anthropic** | tool `web_search_20250305` (server tool); resultados como blocos `web_search_tool_result` com `encrypted_content` |
| **xAI (Grok)** | Delega para o cliente compativel com OpenAI |

### Model-driven: `WebSearchTool`

Para o loop ReAct, use `WebSearchTool` para o LLM decidir quando buscar. Implementa `Tool` e `FactSource`, entao os resultados sao coletados como `Fact`s com proveniencia.

```go
import "github.com/rhgs/crewai-go/tools"

// Wikipedia (default, gratuito, sem API key) — busca apenas artigos da Wikipedia.
search := tools.NewWebSearch(nil)

// LangSearch (100% gratuito, summaries semanticos) — web search generalista.
// Obtenha uma key gratuita em https://langsearch.com
search := tools.NewWebSearch(tools.NewLangSearch("LANGSEARCH_API_KEY"))

// Serpstack (1000 buscas gratuitas/mes) — dados do Google SERP.
search := tools.NewWebSearch(tools.NewSerpstack("SERPSTACK_API_KEY"))

// Google Custom Search (requer API key + CSE ID).
search := tools.NewWebSearch(tools.NewGoogleSearch("GOOGLE_API_KEY", "GOOGLE_CSE_ID"))

// Brave Search (requer API key).
search := tools.NewWebSearch(tools.NewBraveSearch("BRAVE_API_KEY"))

// DuckDuckGo (sem key, mas pode ser bloqueado por captcha).
search := tools.NewWebSearch(tools.NewDuckDuckGoSearch())

agente.WithTools(search)
```

Todos os resultados de busca tem **protecao SSRF**: URLs apontando para localhost, IPs privados, `0.0.0.0`, enderecos link-local (`169.254.x`) e enderecos nao especificados sao filtrados. Nomes de dominio sao resolvidos via DNS para prevenir ataques de DNS rebinding.

Detalhes em [`docs/pt-BR/llms.md`](docs/pt-BR/llms.md) (WebSearcher) e [`docs/pt-BR/tools.md`](docs/pt-BR/tools.md) (WebSearchTool).

## Logging

crewai-go usa [`log/slog`](https://pkg.go.dev/log/slog) (logging estruturado, Go 1.21+) da biblioteca padrao. Cada chamada de log e estruturada (pares chave-valor), nao strings de formato.

Injete um `*slog.Logger` customizado via `WithLogger` em `Crew` e `Agent`:

```go
log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
    Level: slog.LevelDebug,
}))

crew := crewai.NewCrew(agentes, tarefas).WithLogger(log)
agente := crewai.NewAgent("a", "g", "b", llm).WithLogger(log)
```

Se nenhum logger for injetado, `Kickoff` cria um logger de texto no stderr. O nivel padrao depende de `Crew.Verbose`:

| `Verbose` | Nivel padrao | Efeito                                                   |
|-----------|--------------|----------------------------------------------------------|
| `false`   | `LevelError` | Apenas warnings e erros (equivalente ao nop legado).      |
| `true`    | `LevelDebug` | Tudo: debug, info, warn, error.                          |

Quando `WithLogger` e usado, o logger injetado e usado como esta — o chamador controla o nivel e o handler. `Agent.Execute` (standalone, sem crew) usa `slog.Default()` a menos que `Agent.WithLogger` seja definido.

Os subpackages (`llm/*`, `tools/*`) nao logam internamente — eles retornam erros que o executor loga no nivel apropriado.

### Seguranca de logging

> Logs em nivel Debug incluem a saida completa do LLM (`agent thought` → resposta inteira do modelo) e inputs de ferramentas. Se o destino do log for compartilhado (agregador remoto, por exemplo), a saida pode conter PII ou respostas proprietarias do modelo. Erros de provedores (logados em `WARN` em falhas de delegacao, por exemplo) podem incluir API keys na mensagem.
>
> Mitigacoes:
>
> - Escolha o destino adequado (sink em arquivos locais, nao streams compartilhados, quando lidar com dados de usuario).
> - Use filtro de nivel (ex.: somente `LevelError`) para manter secrets fora dos logs por padrao.
> - Envolva o seu handler com um redator. Veja [`examples/logging/`](examples/logging/) para um handler redator que mascara secrets provaveis (API keys, bearer tokens, tokens alfanumericos longos).

### Thread-safety

`WithLogger` **nao e concorrente-safe**. Defina o logger antes de chamar `Kickoff`/`Execute` e nao o mude concorrentemente. Multiplas chamadas sequenciais a `WithLogger` sao idempotentes — a ultima vence.

## Processos: sequencial e hierárquico

**Sequencial** — tarefas em ordem, cada saída vira contexto da próxima:

```go
crew := crewai.NewCrew(agentes, tarefas)
crew.Process = crewai.Sequential
```

**Hierárquico** — um gerente delega cada tarefa ao agente mais adequado:

```go
crew.Process = crewai.Hierarchical
crew.ManagerLLM = llm // ou crew.ManagerAgent = meuGerente
```

Encadeie contexto explicitamente com `WithContext`:

```go
analise := crewai.NewTask("Analise os dados", "insights", analista).
	WithContext(coleta) // recebe a saída da tarefa 'coleta'
```

## Saida estruturada

Quando uma tarefa precisa de dados tipados e confiaveis (ex. para
persistir em um banco de dados), defina o campo `Structured` com um JSON
Schema. O executor instrui o modelo a responder apenas com JSON, valida a
saida em Go, e tenta reparar ate `RepairMax` vezes se a validacao falhar.

```go
schema := map[string]any{
    "type": "object",
    "properties": map[string]any{
        "name":  map[string]any{"type": "string"},
        "count": map[string]any{"type": "integer"},
    },
    "required": []string{"name", "count"},
}

structured, _ := crewai.NewStructuredOutput(schema, crewai.WithRepairMax(3))

tarefa := crewai.NewTask("Extraia o nome e a contagem do produto.", "JSON", agente)
tarefa.Structured = structured
```

Em caso de sucesso, `tarefa.Output()` retorna o JSON **canonizado**
(compactado, estavel). Se o modelo nunca produzir JSON valido dentro do
budget de reparo, a tarefa falha com `crewai.ErrRepairBudgetExceeded`. O
executor nunca retorna JSON invalido ou inventa dados.

O validador embutido suporta um subconjunto do JSON Schema (`type`,
`properties`, `required`, `enum`, `items`) — apenas stdlib, sem
dependencias externas. Detalhes em [`docs/pt-BR/tasks.md`](docs/pt-BR/tasks.md).

## Guardrails

Guardrails sao hooks de validacao pos-output aplicados em codigo. Eles rodam
depois que a crew (ou tarefa) produz a saida e bloqueiam a publicacao se uma
invariante de negocio for violada. Diferente de instrucoes no prompt,
guardrails sao uma garantia em codigo.

```go
crew.Guardrails = []crewai.Guardrail{
    func(_ context.Context, out *crewai.CrewOutput) error {
        if !strings.Contains(out.Final, "http") {
            return fmt.Errorf("faltando URL de origem")
        }
        return nil
    },
}
```

Guardrails de task tambem podem ser definidos via `tarefa.WithGuardrail(...)`.
Em caso de falha, `Kickoff` retorna `crewai.ErrBlockedByGuardrail` — a saida
nunca e parcialmente retornada. Detalhes em [`docs/pt-BR/crews.md`](docs/pt-BR/crews.md).

## Facts e proveniencia

Um **Fact** e uma peca de dado produzida por um conector deterministico, nao
pelo LLM. Carrega proveniencia (fonte, URL, timestamp, hash do payload) para
que um valor errado nunca possa ser apresentado como um "fato que o modelo
lembrou".

```go
tool := crewai.NewFactSourceTool(
    "cnpj_lookup",
    "Consulta status do CNPJ. Entrada: o numero do CNPJ.",
    func(_ context.Context, cnpj string) (string, error) { /* ... */ },
    func(_ context.Context, output string) []crewai.Fact {
        return []crewai.Fact{
            crewai.NewFact(output, "Receita Federal", "https://...", []byte(rawPayload)),
        }
    },
)
```

Facts vem apenas de tools, nunca do modelo. Sao deduplicados por PayloadHash
e aparecem em `CrewOutput.Facts` e `TaskOutput.Facts`. Use
`crewai.AllFactsProvenanced` em um guardrail para exigir proveniencia. Detalhes
em [`docs/pt-BR/tools.md`](docs/pt-BR/tools.md).

## Memória

```go
crew.Memory = true
// ...
crew.Kickoff(ctx, nil)

for _, r := range crew.MemorySnapshot().Records() {
	fmt.Printf("[%s] %s\n", r.Agent, r.Content)
}
```

## Exemplos

Execute os exemplos incluídos:

```bash
go run ./examples/custom_llm     # offline, sem chave de API
go run ./examples/ollama         # Ollama local (ou OLLAMA_CLOUD=1)

export OPENAI_API_KEY=sk-...
go run ./examples/basic
go run ./examples/sequential
go run ./examples/hierarchical
go run ./examples/tools

export XAI_API_KEY=xai-...        # ou XAI_OAUTH=1 + XAI_CLIENT_ID
go run ./examples/xai_oauth
```

## Documentação

> 📖 Toda a documentação está disponível em **Português** (este) e **English** (padrão, `docs/*.md` / `*.md`). Cada arquivo tem um link de troca de idioma no topo.

### Guias principais

| Guia | Português | English |
|------|-----------|---------|
| Getting Started | [PT](docs/pt-BR/getting-started.md) | [EN](docs/getting-started.md) |
| Agents | [PT](docs/pt-BR/agents.md) | [EN](docs/agents.md) |
| Tasks | [PT](docs/pt-BR/tasks.md) | [EN](docs/tasks.md) |
| Crews | [PT](docs/pt-BR/crews.md) | [EN](docs/crews.md) |
| Tools | [PT](docs/pt-BR/tools.md) | [EN](docs/tools.md) |
| LLMs | [PT](docs/pt-BR/llms.md) | [EN](docs/llms.md) |
| Memory | [PT](docs/pt-BR/memory.md) | [EN](docs/memory.md) |
| Plano / Roadmap | [PT](PLAN.pt-BR.md) | [EN](PLAN.md) |

### Novidades da v0.3.0

Todas as features são **backward compatible** — sem breaking changes.

| Recurso | Descrição | Docs (PT) | Docs (EN) |
|---------|-----------|-----------|-----------|
| **Native tool calling** | `Agent.ToolMode` (`"react"` \| `"native"`) alterna para function calling nativo do provedor via `ToolCallingLLM` (Ollama, OpenAI, Anthropic). Limites de segurança em args/output/profundidade JSON. `ToolTrace` em `TaskOutput`. Fallback automático para ReAct quando não suportado. | [docs/pt-BR/agents.md](docs/pt-BR/agents.md) | [docs/agents.md](docs/agents.md) |
| **Web search (agent-driven)** | Interface `WebSearcher` + `SearchWeb(ctx, llm, query, max)` para busca direta do código Go. Ollama, OpenAI, Anthropic, xAI. | [docs/pt-BR/llms.md](docs/pt-BR/llms.md) | [docs/llms.md](docs/llms.md) |
| **Web search (model-driven)** | `WebSearchTool` com 7 provedores plugáveis (Wikipedia, LangSearch, Serpstack, DuckDuckGo, Google, Brave). Proteção SSRF com prevenção de DNS rebinding. | [docs/pt-BR/tools.md](docs/pt-BR/tools.md) | [docs/tools.md](docs/tools.md) |
| **Logging estruturado** | `Logger` customizado substituído por `log/slog`. `Crew.WithLogger` e `Agent.WithLogger` injetam qualquer `*slog.Logger`. Campo `Verbose` preservado para retrocompatibilidade. | [docs/pt-BR/llms.md](docs/pt-BR/llms.md#logging) | [docs/llms.md](docs/llms.md#logging) |
| **Redação de segredos** | `redactError`/`redactString` mascara API keys, Bearer tokens e segredos em query-strings antes de logar erros de provedores. | [redact.go](redact.go) | [redact.go](redact.go) |

**Também na v0.2.0** (agora parte da v0.3.0): saída estruturada com loop de reparo JSON Schema, guardrails, facts & proveniência.

Veja o [CHANGELOG](CHANGELOG.pt-BR.md) para a lista completa de mudanças e o [release v0.3.0](https://github.com/rhgs/crewai-go/releases/tag/v0.3.0) para detalhes.

## Testes

```bash
go test ./...            # todos os testes
go test ./... -cover     # com cobertura
go vet ./...             # análise estática
```

Os testes são **hermeticos**: usam o LLM `mock` e `httptest`, sem chamadas de rede reais.

## Comparação com o CrewAI (Python)

### Mapeamento de API

| CrewAI (Python)        | crewai-go                         |
|------------------------|-----------------------------------|
| `Agent(role=...)`      | `crewai.NewAgent(role, ...)`      |
| `Task(description=...)`| `crewai.NewTask(desc, ...)`       |
| `Crew(agents, tasks)`  | `crewai.NewCrew(agents, tasks)`   |
| `crew.kickoff(inputs)` | `crew.Kickoff(ctx, inputs)`       |
| `Process.sequential`   | `crewai.Sequential`               |
| `Process.hierarchical` | `crewai.Hierarchical`             |
| `@tool` / `BaseTool`   | `crewai.NewTool` / `crewai.Tool`  |
| litellm                | interface `LLM` (openai/anthropic)|

### Recursos do crewai-go que o CrewAI original NÃO tem

| Recurso | crewai-go | CrewAI (Python) |
|---------|-----------|-----------------|
| **Zero dependências** | ✅ apenas stdlib — nenhum pacote externo | ❌ 50+ pacotes PyPI (litellm, langchain, pydantic, chromadb, etc.) |
| **Native tool calling com fallback** | ✅ `Agent.ToolMode` cai automaticamente para ReAct se o provedor não suportar `ToolCallingLLM` | ❌ sem fallback automático; exige provedor compatível |
| **Fatos & proveniência** | ✅ tipo `Fact` first-class com `source_org`, `source_url`, `payload_hash`, `collection_time` — populado apenas por ferramentas determinísticas, nunca pelo LLM | ❌ sem rastreamento de proveniência; o LLM pode alucinar "fatos" |
| **Guardrails** | ✅ validação pós-output a nível de crew (`Crew.Guardrails`) e de task (`Task.Guardrail`) que bloqueia a publicação de outputs inválidos | ❌ sem hooks de validação pós-output |
| **Output estruturado com loop de reparo** | ✅ validação JSON Schema com loop de reparo limitado (`RepairMax`, padrão 2) e `ErrRepairBudgetExceeded` | ⚠️ parcial — usa Pydantic, sem loop de reparo |
| **Web search (agent-driven)** | ✅ interface `WebSearcher` + `SearchWeb(ctx, llm, query, max)` — busca direta do código Go via Ollama, OpenAI, Anthropic, xAI | ❌ sem API de busca direta; exige ferramentas |
| **Web search (model-driven)** | ✅ `WebSearchTool` com 7 provedores plugáveis (Wikipedia, LangSearch, Serpstack, DuckDuckGo, Google, Brave) | ⚠️ exige SerperDev ou ferramenta externa similar |
| **Proteção SSRF** | ✅ bloqueia esquemas não-http(s), loopback, IPs privados, link-local, IPs não-especificados; prevenção de DNS rebinding via `net.LookupIP` (fail-closed) | ❌ sem filtragem de URLs em resultados de busca |
| **Redação de segredos em logs** | ✅ `redactError`/`redactString` mascara API keys, Bearer tokens, segredos em query-strings antes de logar | ❌ sem redação de logs |
| **Logging estruturado** | ✅ `log/slog` — injete qualquer `*slog.Logger` com handler, nível e output customizados | ❌ módulo `logging` do Python, injeção de handler menos flexível |
| **Limites de segurança em tool calls** | ✅ tamanho máximo de args, output, resposta, profundidade JSON, validação de args — todos configuráveis | ❌ sem limites de tamanho/profundidade em tool calls |
| **Tool traces** | ✅ `ToolTrace` em `TaskOutput` — observabilidade completa de cada chamada (nome, args, resultado, duração) | ❌ sem tipo de trace por chamada |
| **Propagação de contexto** | ✅ `context.Context` em toda parte — cancelamento, deadlines, tracing propagados para todas as chamadas LLM e tools | ❌ sem contexto/cancelamento nativo; async exige `asyncio` |
| **Execução thread-safe** | ✅ todos os provedores são seguros para uso concorrente; testado com `-race` | ❌ GIL do Python limita concorrência real |
| **Verificação em tempo de compilação** | ✅ `var _ crewai.WebSearcher = (*Client)(nil)` — detecta métodos faltantes em tempo de build | ❌ duck-typing em runtime, sem verificações em compilação |
| **Deploy de binário único** | ✅ compila para um binário estático único — sem runtime, sem VM, sem interpretador | ❌ exige runtime Python + virtualenv + dependências |
| **Cold start** | ✅ milissegundos (binário nativo) | ❌ segundos (import Python + carregamento de modelo) |
| **Consumo de memória** | ✅ ~10-20 MB típico | ❌ ~100-300 MB típico (Python + deps) |
| **Cross-compilation** | ✅ `GOOS=linux GOARCH=arm64 go build` — qualquer alvo a partir de qualquer host | ❌ exige Python da plataforma alvo ou container |

Este port cobre o núcleo do CrewAI (agentes, tarefas, crews, processos, ferramentas, memória) mais vários recursos originais não presentes na versão Python. Recursos avançados do projeto original (Flows event-driven, training, telemetria) não fazem parte desta versão.

## Licença

[MIT](LICENSE).

## Changelog

Veja [CHANGELOG.pt-BR.md](CHANGELOG.pt-BR.md) (PT) / [CHANGELOG.md](CHANGELOG.md) (EN).
