# Agents

> **Languages:** [English](../agents.md) · **Português** (atual)

Um **Agent** é um trabalhador autônomo. Ele combina uma _persona_ (papel,
objetivo, história), um **LLM** para raciocinar e, opcionalmente, **ferramentas**
para agir.

## Criando um agente

```go
agente := crewai.NewAgent(
	"Pesquisador Sênior",              // Role
	"Descobrir insights acionáveis",   // Goal
	"Você trabalha há 20 anos com...", // Backstory
	llm,                                // LLM
)
```

Ou preenchendo a struct diretamente para mais controle:

```go
agente := &crewai.Agent{
	Role:          "Pesquisador Sênior",
	Goal:          "Descobrir insights acionáveis",
	Backstory:     "Você trabalha há 20 anos com análise de dados.",
	LLM:           llm,
	MaxIterations: 10,   // limite de ciclos de raciocínio por tarefa
	Tools:         []crewai.Tool{ /* ... */ },
}
```

## Campos

| Campo             | Tipo          | Descrição |
|-------------------|---------------|-----------|
| `Role`            | `string`      | Papel/função do agente. **Obrigatório.** |
| `Goal`            | `string`      | Objetivo pessoal que guia as decisões. |
| `Backstory`       | `string`      | Contexto e personalidade. |
| `LLM`             | `crewai.LLM`  | Modelo de linguagem. **Obrigatório.** |
| `Tools`           | `[]crewai.Tool` | Ferramentas disponíveis em qualquer tarefa. |
| `MaxIterations`   | `int`         | Máx. de ciclos de raciocínio/ferramenta (padrão 15). |
| `AllowDelegation` | `bool`        | Marca o agente como apto a gerenciar/delegar. |
| `ToolMode`         | `ToolMode`    | `"react"` (padrão) ou `"native"` — seleciona entre ReAct baseado em texto ou function calling nativo. |
| `Loop`             | `crewai.Loop` | Estratégia de execução opcional (ex. `AgenticLoop`) que substitui o executor ReAct padrão. |

## Logging

Para `Agent.Execute` isolado (sem crew), injete um `*slog.Logger` via
`WithLogger`. Se não definido, `slog.Default()` é usado.

```go
agente := crewai.NewAgent("a", "g", "b", llm).
    WithLogger(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
        Level: slog.LevelDebug,
    })))
```

Quando o agente roda dentro de uma `Crew`, o logger da crew é usado.
Veja [LLMs > Logging](llms.md#logging) para detalhes.

## Adicionando ferramentas

```go
agente.WithTools(tools.Calculator(), minhaFerramenta)
```

`WithTools` é encadeável e devolve o próprio agente:

```go
agente := crewai.NewAgent("Analista", "...", "...", llm).
	WithTools(tools.Calculator())
```

## Executando um agente isoladamente

Fora de uma crew (útil em testes ou fluxos simples):

```go
saida, err := agente.Execute(context.Background(), tarefa)
```

## Como o agente pensa

- **Sem ferramentas:** o agente faz uma única chamada ao LLM e devolve a resposta.
- **Com ferramentas:** o agente entra em um laço ReAct — pensa, escolhe uma
  ferramenta, observa o resultado e repete até chegar a uma `Final Answer` ou
  atingir `MaxIterations`. Veja [tools.md](tools.md).

## Agentic loop

Por padrão, um agente usa o executor ReAct de passagem única. Para tarefas que
se beneficiam de autoavaliação e refinamento iterativo, defina `Agent.Loop` (ou
`Task.Loop`) como um `AgenticLoop`, que segue um ciclo
**Planejar-Executar-Avaliar-Refinar**:

1. **Planejar** — o agente produz um plano numerado (omitido quando o agente
   não tem ferramentas, ou quando `WithSkipPlan` é definido).
2. **Executar** — o agente roda o executor ReAct/estruturado com o plano como
   contexto.
3. **Avaliar** — um avaliador (o mesmo agente, ou um separado via
   `WithEvaluator`) pontua a saída (0-100) contra a saída esperada.
4. **Refinar** — se a pontuação ficar abaixo do limiar, o feedback é injetado
   e o agente reexecuta, até `MaxRefinements` vezes.

```go
agente.Loop = crewai.NewAgenticLoop(
    crewai.WithMaxRefinements(3),
    crewai.WithPassThreshold(80),
    crewai.WithEvaluator(agenteAvaliador), // avaliador independente opcional
)
```

Se a saída nunca passar na avaliação, `Kickoff` retorna
`crewai.ErrEvaluationFailed`. Se o avaliador retornar uma resposta não
parseável, retorna `crewai.ErrInvalidEvaluation`.

Veja [examples/agentic_loop](../examples/agentic_loop) para um exemplo executável.

## Boas práticas

- Dê um **papel específico** ("Redator Técnico de APIs" em vez de "Escritor").
- O **objetivo** deve ser mensurável e orientado ao resultado.
- Use a **história** para calibrar tom e nível de detalhe.
- Ajuste `MaxIterations` para tarefas que usam muitas ferramentas.
