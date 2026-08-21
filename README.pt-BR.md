# crewai-go

> **Languages:** [English](README.md) · **Português** (atual)

[![Go Reference](https://pkg.go.dev/badge/github.com/rhgs/crewai-go.svg)](https://pkg.go.dev/github.com/rhgs/crewai-go)
[![CI](https://github.com/rhgs/crewai-go/actions/workflows/ci.yml/badge.svg)](https://github.com/rhgs/crewai-go/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/rhgs/crewai-go/graph/badge.svg)](https://codecov.io/gh/rhgs/crewai-go)
[![Release](https://img.shields.io/github/v/release/rhgs/crewai-go?label=release)](https://github.com/rhgs/crewai-go/releases)
[![Go Version](https://img.shields.io/github/go-mod/go-version/rhgs/crewai-go)](https://github.com/rhgs/crewai-go/blob/main/go.mod)
[![Last Commit](https://img.shields.io/github/last-commit/rhgs/crewai-go)](https://github.com/rhgs/crewai-go/commits)
[![Zero Dependencies](https://img.shields.io/badge/dependencies-zero-success)](https://pkg.go.dev/github.com/rhgs/crewai-go)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](https://github.com/rhgs/crewai-go/blob/main/LICENSE)
[![Mentioned in Awesome Go](https://awesome.re/mentioned-badge.svg)](https://github.com/avelino/awesome-go)

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
- [Processos: sequencial, hierárquico e em estágios](#processos-sequencial-hierárquico-e-em-estágios)
- [Agentic loop](#agentic-loop)
- [Saida estruturada](#saida-estruturada)
- [Guardrails](#guardrails)
- [Facts e proveniência](#facts-e-proveniência)
- [MCP](#mcp-model-context-protocol)
- [Progresso e warnings](#progresso-e-warnings)
- [Memória](#memória)
- [Exemplos](#exemplos)
- [Documentação](#documentação)
- [Testes](#testes)
- [Comparação com o CrewAI (Python)](#comparação-com-o-crewai-python)
- [Contribuindo](#contribuindo)
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
- ⚡ **Waves assíncronas (v0.6)**: tarefas independentes com `Task.Async` rodam em
  paralelo sob Sequential/Hierarchical, respeitando dependências `Task.Context`
  e sempre agregando na ordem de declaração. `NewCrew` usa
  `AsyncMaxWorkers = 8` por padrão.
- 🧠 **Memória** entre tarefas e **contexto** encadeável — `MemoryStore`
  plugável, `FileStore` durável (JSONL), embeddings opcionais + recall por cosseno.
- 👔 **Processo hierárquico** com gerente que delega dinamicamente.
- 🪜 **Processo em estágios (Staged)** — estágios em sequência, tarefas de um estágio em paralelo.
- 🔁 **Agentic loop** — ciclo opcional Planejar-Executar-Avaliar-Refinar com autoavaliação e refinamento iterativo.
- 🔌 **MCP** — conecte a servidores Model Context Protocol e exponha as tools como `crewai.Tool` (schema preservado).
- 📡 **Progresso e warnings** — callbacks `WithProgress` em tempo real e warnings não-fatais por tarefa.
- 🌊 **Streaming** — `StreamingLLM` opcional + `Crew.WithStream` para deltas de tokens da resposta final (ReAct/native sem tools); demux Task/Agent em waves Async.
- 📊 **Eventos de lifecycle** — `WithEvents` / `CrewEvent` telemetria de metadados (llm_call, turns ReAct, repairs) junto com Progress.
- ✅ **Testável** — LLM mock incluído; ~90% de cobertura no núcleo.

## Conceitos

| Conceito     | O que é                                                                 |
|--------------|-------------------------------------------------------------------------|
| **Agent**    | Um trabalhador com papel, objetivo, história, um LLM e ferramentas.     |
| **Task**     | Uma unidade de trabalho com descrição, saída esperada e responsável.    |
| **Crew**     | A equipe: agrupa agentes e tarefas e as orquestra.                      |
| **Process**  | Estratégia de execução: `Sequential`, `Hierarchical` ou `Staged`.       |
| **Tool**     | Uma capacidade que o agente pode invocar (cálculo, busca, API…).        |
| **LLM**      | Abstração do modelo de linguagem. Vários provedores prontos.            |
| **Memory**   | Bag de curto prazo + `MemoryStore` plugável (FileStore, embeddings).   |
| **StructuredOutput** | Configura uma tarefa para exigir JSON validado por um JSON Schema. |
| **Guardrail** | Hook de validacao pos-saida que bloqueia publicacao de saidas invalidas. |
| **Fact** | Dado de um conector deterministico com proveniencia (fonte, hash). |
| **FactSource** | Interface opcional para tools que produzem Facts. |
| **ToolMode** | Estrategia de execucao de tools: `"react"` (padrao) ou `"native"`. |
| **ToolCallingLLM** | Interface LLM opcional para function calling nativo. |
| **StreamingLLM** | Interface opcional de LLM para streaming de tokens (`CallStream`). |
| **StreamChunk** | Unidade de stream: `Delta`, `Task`, `Agent`, `Done`, `Err`. |
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

Todos os resultados de busca tem **protecao SSRF**: URLs apontando para localhost, IPs privados/CGNAT/multicast, `0.0.0.0`, enderecos link-local (`169.254.x`), aliases de metadata, userinfo e enderecos nao especificados sao filtrados. Nomes de dominio sao resolvidos via DNS para prevenir ataques de DNS rebinding (fail-closed).

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

Para mascarar segredos provaveis em mensagens e atributos de log (best-effort, opt-in):

```go
log := slog.New(crewai.RedactHandler(
    slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}),
))
crew := crewai.NewCrew(agentes, tarefas).WithLogger(log)
```

Veja [`examples/logging/`](examples/logging/) para um exemplo completo. Prefira `LevelInfo` (ou mais alto) em producao; `LevelDebug` pode incluir output completo do LLM e args de tools.

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

## Processos: sequencial, hierárquico e em estágios

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

**Em estágios (Staged)** — os estágios rodam em sequência, mas as tarefas
dentro de um mesmo estágio rodam concorrentemente. A saída de cada estágio
fica disponível como contexto para as tarefas dos estágios seguintes. Um
estágio marcado como `Optional` não aborta o crew quando uma de suas tarefas
falha; caso contrário, a primeira falha aborta o `Kickoff`.

```go
crew := crewai.NewCrew(agentes, nil)
crew.Process = crewai.Staged
crew.Stages = []crewai.Stage{
    {Name: "coleta", Tasks: []*crewai.Task{pesquisaA, pesquisaB}},
    {Name: "sintese", Tasks: []*crewai.Task{redacao}},
}
```

Encadeie contexto explicitamente com `WithContext`:

```go
analise := crewai.NewTask("Analise os dados", "insights", analista).
	WithContext(coleta) // recebe a saída da tarefa 'coleta'
```

**Waves assíncronas (Sequential / Hierarchical)** — marque tarefas
independentes com `WithAsync()` para sobrepô-las sem migrar para Staged.
`Task.Context` é a DAG; cada wave agrega por **ordem de declaração** após a
barreira. `NewCrew` usa `AsyncMaxWorkers = 8` por padrão (`0` = ilimitado).
Ignorado em Staged (Warn único). Veja
[docs/pt-BR/crews.md](docs/pt-BR/crews.md) e `examples/async_tasks`.

```go
pesquisa := crewai.NewTask("pesquisar tema", "notas", agente).WithAsync()
esboco   := crewai.NewTask("rascunhar esboço", "bullets", agente).WithAsync()
redacao  := crewai.NewTask("escrever artigo", "markdown", agente).
	WithContext(pesquisa, esboco)
```

## Agentic loop

Por padrao um agente usa o executor ReAct de passagem unica. Para tarefas que
se beneficiam de autoavaliacao e refinamento iterativo, defina `Agent.Loop`
(ou `Task.Loop`) com um `AgenticLoop`:

```go
agent.Loop = crewai.NewAgenticLoop(
    crewai.WithMaxRefinements(3),
    crewai.WithPassThreshold(80),
    crewai.WithEvaluator(avaliador), // avaliador independente opcional
)
```

O loop segue o ciclo **Planejar → Executar → Avaliar → Refinar**. Se a saida
nunca passar na avaliacao, `Kickoff` retorna `ErrEvaluationFailed`. Veja
[docs/pt-BR/agents.md](docs/pt-BR/agents.md) e
[examples/agentic_loop](examples/agentic_loop).

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

O validador embutido e um subconjunto stdlib do JSON Schema: `type`,
`properties`, `required`, `enum`, `items`, `additionalProperties`,
`minLength`/`maxLength` (**bytes**), bounds numericos/de array, `pattern`
e `oneOf`/`anyOf`/`allOf`. Use `WithStrictSchema()` para rejeitar keywords
nao suportadas na construcao. `WithAllowTools()` opcional roda uma fase
gather de tools antes do capture JSON. Detalhes em
[`docs/pt-BR/tasks.md`](docs/pt-BR/tasks.md).

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

## MCP (Model Context Protocol)

Conecte a servidores MCP externos via Streamable HTTP e exponha as tools
como valores `crewai.Tool`. O `inputSchema` original e preservado
(`SchemaProvider`) para o native tool calling.

```go
import "github.com/rhgs/crewai-go/mcp"

clients, err := mcp.LoadConfig(ctx, "/etc/mcp.json", "meu-app", "1.0.0")
// ou: client := mcp.New(endpoint, mcp.WithHTTPTimeout(30*time.Second),
//     mcp.WithHeader("Authorization", "Bearer "+tok))
var tools []crewai.Tool
for _, c := range clients {
    ts, _ := c.ListTools(ctx)
    // Filtro opcional least-privilege (deny-by-default para nomes nao listados):
    // ts = mcp.FilterTools(ts, map[string]struct{}{"search_docs": {}})
    for _, t := range ts {
        tools = append(tools, mcp.NewToolAdapter(c, t, mcp.WithDescriptionLimit(500)))
    }
}
agent.WithTools(tools...)
```

Timeout HTTP default e 30s (`DefaultHTTPTimeout`); o JSON aceita
`"timeout"` por servidor. Veja [`docs/pt-BR/mcp.md`](docs/pt-BR/mcp.md) para
configuracao, modelo de ameaca, guards de catalogo e a API programatica.

## Progresso e warnings

**Progresso.** Exponha eventos de execucao em tempo real para um frontend
via `Crew.WithProgress`. Eventos: `stage_started`, `stage_completed`,
`task_started`, `task_completed`, `tool_invoked`. Os payloads carregam
apenas metadados (nunca prompts, saidas ou inputs de tools). O callback
deve ser seguro para goroutines; panics sao recuperados.

```go
crew.WithProgress(func(p crewai.Progress) {
    fmt.Printf("%s %s/%s tool=%s\n", p.Event, p.Stage, p.Task, p.Tool)
})
```

**Warnings.** Uma tarefa que sucede ainda pode registrar diagnosticos
nao-fatais (`Task.AddWarning` / `crewai.AddWarningFromCtx`). Eles se
agregam em `TaskOutput.Warnings` e `CrewOutput.Warnings` — distintos de
`Stage.Optional`, que engole *falhas* de tarefa.

```go
crewai.AddWarningFromCtx(ctx, "fonte secundaria em timeout")
```

Detalhes em [`docs/pt-BR/crews.md`](docs/pt-BR/crews.md) e
[`docs/pt-BR/tasks.md`](docs/pt-BR/tasks.md).

## Delegacao entre agents

```go
researcher.AllowDelegation = true // alvo elegivel
crew.EnableDelegationTool = true  // auto-anexa delegate_to_coworker
// ou: writer.WithTools(crewai.NewDelegationTool(crew))
```

Guardas de profundidade/ciclo/auto (`DefaultMaxDelegationDepth` = 2). Veja
[`docs/pt-BR/agents.md`](docs/pt-BR/agents.md) e
[`examples/delegation`](examples/delegation).

## Memória

```go
crew.Memory = true // alias permanente v0.x → garante store InMemory
// ...
crew.Kickoff(ctx, nil)

for _, r := range crew.MemorySnapshot().Records() {
	fmt.Printf("[%s] %s\n", r.Agent, r.Content)
}
```

`*Memory` também implementa o contrato plugável `crewai.MemoryStore`
(Put/Query/Delete/Close com tetos). Ligue backends de longo prazo com
`Crew.MemoryStore` + `Crew.MemoryPolicy` (`NewMemoryPolicy()` para os
padrões). Em waves/stages paralelas, o AutoSave só commit na barreira em
ordem de declaração (D-M7) — o inject da próxima wave não vê irmãos em voo.

```go
store, _ := crewai.OpenFileStore("/var/lib/myapp/crew-memory") // app faz Close
defer store.Close()
crew.MemoryStore = store
crew.MemoryPolicy = crewai.NewMemoryPolicy()
// Recall semântico opcional (embedder da app; serial na barreira):
// crew.Embed = meuEmbedder; crew.MemoryPolicy.AutoEmbed = true
```

Veja [docs/pt-BR/memory.md](docs/pt-BR/memory.md), `examples/memory_file` e
`examples/memory_embed`.

## Exemplos

Execute os exemplos incluídos:

```bash
go run ./examples/custom_llm     # offline, sem chave de API
go run ./examples/ollama         # Ollama local (ou OLLAMA_CLOUD=1)
go run ./examples/streaming     # deltas WithStream (mock offline)
go run ./examples/async_tasks    # waves Task.Async (mock offline)
go run ./examples/memory_file    # FileStore JSONL entre Kickoffs
go run ./examples/memory_embed   # AutoEmbed + Query por cosseno (mock)
go run ./examples/agentic_loop   # offline, mock LLM
go run ./examples/logging        # demo RedactHandler
go run ./examples/mcp            # wiring MCP (live com MCP_ENDPOINT)

export OPENAI_API_KEY=sk-...
go run ./examples/basic
go run ./examples/sequential
go run ./examples/hierarchical
go run ./examples/staged
go run ./examples/tools
go run ./examples/delegation

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
| MCP | [PT](docs/pt-BR/mcp.md) | [EN](docs/en/mcp.md) |
| Plano / Roadmap | [PT](Plan/PLAN.pt-BR.md) | [EN](Plan/PLAN.md) |
| Politica de seguranca | — | [EN](SECURITY.md) |

### Novidades da v0.7.0

Todas as features são **backward compatible** — sem breaking changes em
`LLM.Call` nem em providers que não implementam `StreamingLLM`.

| Recurso | Descrição | Docs (PT) | Docs (EN) |
|---------|-----------|-----------|-----------|
| **`StreamingLLM` + `WithStream`** | API opcional de stream via type assert; `Crew.WithStream` / sink no context; deltas de texto final (ReAct/native sem tools); demux Task/Agent em waves Async; teto de bytes do body. | [docs/pt-BR/llms.md](docs/pt-BR/llms.md#streaming) · [docs/pt-BR/crews.md](docs/pt-BR/crews.md) | [docs/llms.md](docs/llms.md#streaming) · [docs/crews.md](docs/crews.md) |
| **`CallStream` nos providers** | OpenAI (SSE), Ollama (NDJSON), Anthropic (SSE), xAI (delegate), `llm/mock`. | [docs/pt-BR/llms.md](docs/pt-BR/llms.md#streaming) | [docs/llms.md](docs/llms.md#streaming) |
| **Helpers** | `CollectStream` / `CallOrStream` públicos; sentinelas `ErrStreamIncomplete`, `ErrStreamResponseTooLarge`. | godoc | godoc |
| **Exemplo** | Offline `examples/streaming` (demux Async). | [examples/streaming](examples/streaming) | [examples/](examples/) |

**Também na v0.6.0**: waves `Task.Async`, `MemoryStore` / `MemoryPolicy` / D-M7, `FileStore`, embeddings + Query por cosseno.

**Também na v0.5.0**: MCP hardening, jail OutputFile, `RedactHandler`, single-flight Kickoff, keywords de schema, `delegate_to_coworker`.

**Também na v0.4.x**: processo staged, agentic loop, cliente MCP, progress callbacks, warnings por tarefa, structured `emit_result`.

**Também na v0.3.0**: native tool calling, web search, logging estruturado via `log/slog`, redação de segredos.

Veja o [CHANGELOG](CHANGELOG.pt-BR.md) para a lista completa de mudanças e o [release v0.7.0](https://github.com/rhgs/crewai-go/releases/tag/v0.7.0) para detalhes.


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
| `Process.staged`       | `crewai.Staged`                   |
| `@tool` / `BaseTool`   | `crewai.NewTool` / `crewai.Tool`  |
| litellm                | interface `LLM` (openai/anthropic)|

### Recursos do crewai-go que o CrewAI original NÃO tem

| Recurso | crewai-go | CrewAI (Python) |
|---------|-----------|-----------------|
| **Zero dependências** | ✅ apenas stdlib — nenhum pacote externo | ❌ 50+ pacotes PyPI (litellm, langchain, pydantic, chromadb, etc.) |
| **Processo Staged** | ✅ estágios em sequência, tarefas de um estágio em paralelo; estágios opcionais continuam em falha | ❌ sem processo staged/paralelo-dentro-do-estágio |
| **Streaming de deltas do LLM** | ✅ `StreamingLLM` opcional + `WithStream`; caminhos de texto final; demux Task/Agent | ⚠️ SDKs de provider streamam; sink no nível de orquestração é da app |
| **Waves async sob Sequential/Hierarchical** | ✅ `Task.Async` + DAG via `Task.Context`; agregação por ordem de declaração; teto de workers padrão 8 | ⚠️ async existe via asyncio / event loops, não como agendador de waves first-class com fold na barreira |
| **Memória de longo prazo plugável (stdlib)** | ✅ `MemoryStore` + `FileStore` JSONL + embeddings opcionais por cosseno; zero deps extras | ⚠️ tipicamente exige Chroma/stores vetoriais externos |
| **Agentic loop** | ✅ ciclo opt-in Planejar-Executar-Avaliar-Refinar com avaliador independente e refinamentos limitados | ⚠️ workflows agenticos existem, mas não como loop first-class Plan-Execute-Evaluate-Refine com limiar de score |
| **Native tool calling com fallback** | ✅ `Agent.ToolMode` cai automaticamente para ReAct se o provedor não suportar `ToolCallingLLM` | ❌ sem fallback automático; exige provedor compatível |
| **Fatos & proveniência** | ✅ tipo `Fact` first-class com `source_org`, `source_url`, `payload_hash`, `collection_time` — populado apenas por ferramentas determinísticas, nunca pelo LLM | ❌ sem rastreamento de proveniência; o LLM pode alucinar "fatos" |
| **Guardrails** | ✅ validação pós-output a nível de crew (`Crew.Guardrails`) e de task (`Task.Guardrail`) que bloqueia a publicação de outputs inválidos | ❌ sem hooks de validação pós-output |
| **Output estruturado com loop de reparo** | ✅ validação JSON Schema com loop de reparo limitado (`RepairMax`, padrão 2) e `ErrRepairBudgetExceeded` | ⚠️ parcial — usa Pydantic, sem loop de reparo |
| **Web search (agent-driven)** | ✅ interface `WebSearcher` + `SearchWeb(ctx, llm, query, max)` — busca direta do código Go via Ollama, OpenAI, Anthropic, xAI | ❌ sem API de busca direta; exige ferramentas |
| **Web search (model-driven)** | ✅ `WebSearchTool` com 7 provedores plugáveis (Wikipedia, LangSearch, Serpstack, DuckDuckGo, Google, Brave) | ⚠️ exige SerperDev ou ferramenta externa similar |
| **Proteção SSRF** | ✅ bloqueia não-http(s), userinfo, loopback, IPs privados/CGNAT/multicast, link-local, não-especificados, aliases de metadata; prevenção de DNS rebinding via `net.LookupIP` (fail-closed) | ❌ sem filtragem de URLs em resultados de busca |
| **Redação de segredos em logs** | ✅ `redactError`/`redactString` + `RedactHandler` opt-in para mensagens/attrs do `slog` | ❌ sem redação de logs |
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

## Contribuindo

Contribuicoes sao bem-vindas. Veja [CONTRIBUTING.pt-BR.md](CONTRIBUTING.pt-BR.md)
(PT) / [CONTRIBUTING.md](CONTRIBUTING.md) (EN) para setup, convencoes e o
checklist do PR. Leia tambem o nosso
[Codigo de Conduta](CODE_OF_CONDUCT.pt-BR.md) ([EN](CODE_OF_CONDUCT.md)).

## Licença

[MIT](LICENSE).

## Changelog

Veja [CHANGELOG.pt-BR.md](CHANGELOG.pt-BR.md) (PT) / [CHANGELOG.md](CHANGELOG.md) (EN).
