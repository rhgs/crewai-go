# Crews

> **Languages:** [English](../crews.md) · **Português** (atual)

Uma **Crew** reúne agentes e tarefas e os orquestra segundo um **Process**.

## Criando e executando

```go
crew := crewai.NewCrew(
	[]*crewai.Agent{pesquisador, redator},
	[]*crewai.Task{pesquisa, artigo},
)
crew.Verbose = true

out, err := crew.Kickoff(context.Background(), nil)
```

## Campos

| Campo          | Tipo           | Descrição |
|----------------|----------------|-----------|
| `Agents`       | `[]*Agent`     | Membros da equipe. |
| `Tasks`        | `[]*Task`      | Tarefas a executar. |
| `Process`      | `Process`      | `Sequential` (padrão) ou `Hierarchical`. |
| `Verbose`      | `bool`         | Ativa logs detalhados. |
| `Memory`       | `bool`         | Ativa a memória compartilhada. |
| `ManagerLLM`   | `LLM`          | LLM do gerente (processo hierárquico). |
| `ManagerAgent` | `*Agent`       | Gerente explícito (tem prioridade sobre `ManagerLLM`). |

## O resultado: `CrewOutput`

```go
type CrewOutput struct {
	Final       string        // saída da última tarefa
	TasksOutput []TaskOutput  // saída de cada tarefa
	Duration    time.Duration // tempo total
}
```

## Processo sequencial

As tarefas rodam na ordem definida. Se uma tarefa não tem `Agent`, a crew usa
o agente na mesma posição da lista de agentes.

```go
crew.Process = crewai.Sequential
```

## Processo hierárquico

Um **gerente** decide qual agente executa cada tarefa sem `Agent` fixo. Defina
o gerente de uma destas formas:

```go
// A) A crew cria um gerente automático a partir de um LLM:
crew.Process = crewai.Hierarchical
crew.ManagerLLM = llm

// B) Você fornece um agente gerente customizado:
crew.ManagerAgent = crewai.NewAgent(
	"Diretor de Projeto", "Coordenar a equipe", "...", llm,
)
```

Para cada tarefa sem agente, o gerente recebe a lista de membros (papel +
objetivo) e a descrição da tarefa, e responde com o papel do membro escolhido.
Se a tarefa já tem `Agent`, ele é respeitado.

> Se houver apenas um agente, ele é escolhido automaticamente. Se a delegação
> falhar (erro do LLM), a crew usa o primeiro agente como _fallback_.

## Interpolação de inputs

O segundo argumento de `Kickoff` alimenta a interpolação `{chave}` de todas as
tarefas:

```go
crew.Kickoff(ctx, map[string]string{"cliente": "Acme"})
```

## Cancelamento e timeouts

`Kickoff` respeita o `context.Context`:

```go
ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
defer cancel()
out, err := crew.Kickoff(ctx, nil)
```
