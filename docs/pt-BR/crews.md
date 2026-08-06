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

## Guardrails

Guardrails sao hooks de validacao pos-output aplicados em codigo. Eles rodam
depois que a crew (ou tarefa) produz a saida e BLOQUEIAM a publicacao se uma
invariante de negocio for violada. Diferente de instrucoes no prompt,
guardrails sao uma garantia em codigo: a saida nunca e retornada se um
guardrail falhar.

### Guardrails de crew

Defina `Crew.Guardrails` para validar o `CrewOutput` completo apos todas as
tarefas:

```go
crew.Guardrails = []crewai.Guardrail{
    func(_ context.Context, out *crewai.CrewOutput) error {
        if len(out.Final) < 50 {
            return fmt.Errorf("saida muito curta: %d chars", len(out.Final))
        }
        return nil
    },
}
```

### Guardrails de task

Defina `Task.Guardrail` para validar a saida de uma unica tarefa assim que ela
termina (antes da proxima):

```go
tarefa := crewai.NewTask("...", "...", agente).
    WithGuardrail(func(_ context.Context, out *crewai.CrewOutput) error {
        if !strings.Contains(out.Final, "http") {
            return fmt.Errorf("faltando URL de origem")
        }
        return nil
    })
```

### Semantica de bloqueio

- Se qualquer guardrail retornar um erro nao-nil, `Kickoff` retorna
  `crewai.ErrBlockedByGuardrail` envolvendo o erro do guardrail.
- A saida parcialmente validada NAO e retornada.
- Guardrails de task rodam na conclusao da tarefa; guardrails de crew rodam
  apos todas as tarefas. Primeira falha interrompe (short-circuit).
- Guardrails NAO DEVEM mutar a saida.

### Guardrails vs. saida estruturada

| Recurso | O que checa | Quando | Falha |
|---|---|---|---|
| Saida estruturada | Forma JSON (schema) | Durante chamada LLM | Loop de reparo, depois `ErrRepairBudgetExceeded` |
| Guardrails | Significado de negocio (invariantes) | Apos saida | `ErrBlockedByGuardrail` (sem retry) |

A validacao de schema checa a **forma**; guardrails checam o **significado**.
Use ambos para maxima seguranca.
