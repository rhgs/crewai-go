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

## Boas práticas

- Dê um **papel específico** ("Redator Técnico de APIs" em vez de "Escritor").
- O **objetivo** deve ser mensurável e orientado ao resultado.
- Use a **história** para calibrar tom e nível de detalhe.
- Ajuste `MaxIterations` para tarefas que usam muitas ferramentas.
