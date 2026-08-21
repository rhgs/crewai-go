# Getting Started

> **Languages:** [English](../getting-started.md) · **Português** (atual)

Este guia leva você do zero à primeira _crew_ funcionando.

## Pré-requisitos

- **Go 1.24+** — verifique com `go version`.
- Uma chave de API de algum provedor de LLM (ex.: `OPENAI_API_KEY`) — ou use um LLM offline/customizado.

## 1. Crie um projeto

```bash
mkdir minha-crew && cd minha-crew
go mod init exemplo.com/minha-crew
go get github.com/rhgs/crewai-go@latest
```

## 2. Escreva o programa

`main.go`:

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
	llm := openai.New("gpt-4o-mini")

	pesquisador := crewai.NewAgent(
		"Pesquisador",
		"Encontrar informações relevantes e confiáveis",
		"Você é um analista experiente e cético.",
		llm,
	)

	tarefa := crewai.NewTask(
		"Explique em 3 pontos por que Go é boa para back-end.",
		"Uma lista com 3 itens curtos.",
		pesquisador,
	)

	crew := crewai.NewCrew([]*crewai.Agent{pesquisador}, []*crewai.Task{tarefa})
	crew.Verbose = true // ou use WithLogger para logging estruturado (veja abaixo)

	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(out.Final)
}
```

> **Dica:** Para logging estruturado via `log/slog`, troque `crew.Verbose = true` por
> `crew.WithLogger(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})))`.
> Veja [Logging](llms.md#logging) para detalhes.

## 3. Execute

```bash
export OPENAI_API_KEY=sk-...
go run .
```

## 4. Sem chave de API? Rode offline

Você pode implementar a interface `crewai.LLM` você mesmo (veja
[llms.md](llms.md)) ou usar o LLM `mock` do pacote de testes. O exemplo
`examples/custom_llm` roda totalmente offline:

```bash
go run github.com/rhgs/crewai-go/examples/custom_llm
```

## Próximos passos

- [Agents](agents.md) — configurar papéis, objetivos, ferramentas e o agentic loop.
- [Tasks](tasks.md) — encadear tarefas com contexto, waves `Async`, saída estruturada e warnings.
- [Crews](crews.md) — processos sequencial, hierárquico, staged e agendamento async; callbacks de progresso.
- [Memory](memory.md) — bag de curto prazo, `MemoryStore`, FileStore, embeddings.
- [Tools](tools.md) — dar "mãos" aos seus agentes (incluindo web search).
- [LLMs](llms.md) — provedores, native tool calling e logging.
- [MCP](mcp.md) — conectar servidores externos do Model Context Protocol.
