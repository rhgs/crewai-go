# Tasks

> **Languages:** [English](../tasks.md) · **Português** (atual)

> **Languages:** [English](../tasks.md) · [**Português**](../../tasks.md)


Uma **Task** é uma unidade de trabalho: o que fazer, qual saída esperar e quem
é o responsável.

## Criando uma tarefa

```go
tarefa := crewai.NewTask(
	"Escreva um resumo executivo do relatório trimestral.", // Description
	"Um resumo de até 200 palavras em tópicos.",            // ExpectedOutput
	redator,                                                 // Agent
)
```

## Campos

| Campo            | Tipo            | Descrição |
|------------------|-----------------|-----------|
| `Name`           | `string`        | Identificador curto (aparece em logs/memória). |
| `Description`    | `string`        | Instrução detalhada. Suporta `{variáveis}`. |
| `ExpectedOutput` | `string`        | Formato/qualidade esperados. |
| `Agent`          | `*crewai.Agent` | Responsável. Pode ser `nil` (a crew resolve). |
| `Tools`          | `[]crewai.Tool` | Sobrepõe as ferramentas do agente nesta tarefa. |
| `Context`        | `[]*crewai.Task`| Tarefas cujas saídas viram contexto desta. |
| `OutputFile`     | `string`        | Se definido, grava a saída neste arquivo. |

## Contexto entre tarefas

Use `WithContext` para passar a saída de tarefas anteriores:

```go
coleta  := crewai.NewTask("Colete os dados de vendas.", "tabela", analista)
analise := crewai.NewTask("Analise as tendências.", "insights", analista).
	WithContext(coleta) // recebe a saída de 'coleta' no prompt
```

No **processo sequencial**, a saída da tarefa anterior também é encadeada
automaticamente quando você usa `WithContext`. Sem `WithContext`, cada tarefa
recebe apenas a memória acumulada (se `crew.Memory = true`).

## Interpolação de variáveis

Use `{chave}` na descrição e na saída esperada; passe os valores no `Kickoff`:

```go
tarefa := crewai.NewTask("Pesquise sobre {tema} no setor de {setor}.", "...", agente)

crew.Kickoff(ctx, map[string]string{
	"tema":  "IA generativa",
	"setor": "saúde",
})
```

## Salvando a saída em arquivo

```go
tarefa.OutputFile = "relatorio.md"
```

Após a execução, a saída é gravada no arquivo (além de ficar em `tarefa.Output()`).

## Recuperando a saída

```go
crew.Kickoff(ctx, nil)
fmt.Println(tarefa.Output()) // saída desta tarefa específica
```

Ou pelo resultado consolidado:

```go
out, _ := crew.Kickoff(ctx, nil)
for _, to := range out.TasksOutput {
	fmt.Printf("%s (%s): %s\n", to.Task, to.Agent, to.Output)
}
```
