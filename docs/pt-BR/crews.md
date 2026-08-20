# Crews

> **Languages:** [English](../crews.md) · **Português** (atual)

Uma **Crew** reúne agentes e tarefas e os orquestra segundo um **Process**.

## Concorrencia do Kickoff

Apenas **um** `Kickoff` pode rodar por vez em um dado `*Crew`. Uma chamada concorrente retorna `ErrCrewRunning` imediatamente (fail fast — nao enfileira). Crie valores `Crew` separados para runs paralelos. Reuso sequencial do mesmo Crew e suportado.

## Criando e executando

```go
crew := crewai.NewCrew(
	[]*crewai.Agent{pesquisador, redator},
	[]*crewai.Task{pesquisa, artigo},
)
crew.Verbose = true

out, err := crew.Kickoff(context.Background(), nil)
```

Para logging estruturado, injete um `*slog.Logger` em vez de usar `Verbose`:

```go
import "log/slog"

log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
    Level: slog.LevelDebug,
}))
crew := crewai.NewCrew(agentes, tarefas).WithLogger(log)
```

Veja [LLMs > Logging](llms.md#logging) para a referência completa.

## Campos

| Campo          | Tipo           | Descrição |
|----------------|----------------|-----------|
| `Agents`       | `[]*Agent`     | Membros da equipe. |
| `Tasks`        | `[]*Task`      | Tarefas a executar. |
| `Process`      | `Process`      | `Sequential` (padrão), `Hierarchical` ou `Staged`. |
| `Stages`       | `[]Stage`      | Estágios do processo `Staged` (têm prioridade sobre `Tasks`). |
| `Verbose`      | `bool`         | Ativa logs detalhados (mapeia para `LevelDebug` quando nenhum logger é injetado via `WithLogger`). |
| `logger`       | `*slog.Logger` | Interno — definido via `WithLogger`. Quando nil, `Kickoff` cria um logger de texto padrão no stderr. |
| `Memory`       | `bool`         | Ativa a memória compartilhada. |
| `ManagerLLM`   | `LLM`          | LLM do gerente (processo hierárquico). |
| `ManagerAgent` | `*Agent`       | Gerente explícito (tem prioridade sobre `ManagerLLM`). |
| `Guardrails`   | `[]Guardrail`  | Hooks de validação pós-saída no nível da crew. |
| `OutputDir` | `string` | Jail opcional para `Task.OutputFile` quando a task nao define o seu. Symlink-aware; ver tasks. |
| `progress`     | `ProgressFunc` | Interno — definido via `WithProgress`. |

## O resultado: `CrewOutput`

```go
type CrewOutput struct {
	Final       string        // saída da última tarefa
	TasksOutput []TaskOutput  // saída de cada tarefa (incl. Facts, ToolTraces, Warnings)
	Duration    time.Duration // tempo total
	Facts       []Fact        // facts coletados de tools FactSource (deduplicados)
	Warnings    []string      // diagnósticos não-fatais de tarefas bem-sucedidas
}
```

`TaskOutput` também carrega `Facts`, `ToolTraces` (apenas native tool
calling) e `Warnings` por tarefa.

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

## Processo em estágios (Staged)

O processo `Staged` agrupa tarefas em **estágios**: os estágios rodam em
sequência, mas as tarefas dentro de um mesmo estágio rodam concorrentemente.
A saída de cada estágio fica disponível como contexto para as tarefas dos
estágios seguintes (via `Task.Context`).

```go
crew := crewai.NewCrew(agentes, nil)
crew.Process = crewai.Staged
crew.Stages = []crewai.Stage{
    {Name: "coleta", Tasks: []*crewai.Task{pesquisaA, pesquisaB}},
    {Name: "sintese", Tasks: []*crewai.Task{redacao}},
}
```

Cada `Stage` tem:

| Campo      | Tipo      | Descrição |
|------------|-----------|-----------|
| `Name`     | `string`  | Identificador curto usado em logs. |
| `Tasks`    | `[]*Task` | Tarefas que rodam concorrentemente neste estágio. |
| `Optional` | `bool`    | Se `true`, uma falha não aborta o `Kickoff` (vira aviso). |

- A saída de um estágio alimenta os seguintes: uma tarefa do estágio N pode
  listar, em `Context`, tarefas de estágios anteriores (não do mesmo estágio,
  que rodam em paralelo).
- `CrewOutput.Final` é a saída da última tarefa do último estágio.
- Se `Process == Staged` e `Stages` estiver vazio, `Kickoff` retorna
  `ErrNoStages`.

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

## Observabilidade de progresso

`Kickoff` e uma caixa-preta ate retornar. Para expor eventos em tempo
real a um frontend (ex.: via SSE), defina um callback de progresso com
`WithProgress`:

```go
crew := crewai.NewCrew(agentes, tarefas).
    WithProgress(func(p crewai.Progress) {
        // p carrega: Stage, Task, Agent, Event, Tool, Duration, Err
        // Event e um de: "stage_started", "stage_completed",
        //                "task_started",  "task_completed",
        //                "tool_invoked"
        w.Header().Set("Content-Type", "text/event-stream")
        fmt.Fprintf(w, "data: %s %s %s\n\n", p.Event, p.Task, p.Tool)
    })
```

O callback e invocado de multiplas goroutines quando estagios rodam em
paralelo — deve ser thread-safe, como um `slog.Handler`. Um panic
dentro do callback e recuperado e logado via `slog.Default()`; o
`Kickoff` roda ate o fim independentemente.

Eventos de progresso nunca contem corpo de prompt, saida do LLM nem
input de ferramenta — apenas metadados. Erros de falha de tarefa em
`task_completed` passam por `redactError` antes do callback (tokens
longos e credenciais Bearer sao mascarados). Se precisar expor
inputs ou saidas de ferramentas, logue explicitamente com as partes
sensiveis mascaradas.
