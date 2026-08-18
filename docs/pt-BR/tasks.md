# Tasks

> **Languages:** [English](../tasks.md) · **Português** (atual)

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
| `Structured`     | `*crewai.StructuredOutput` | Se definido, exige saida JSON validada contra um JSON Schema. |

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

## Saida estruturada

Quando uma tarefa precisa de dados tipados e confiaveis (ex. para
persistir em um banco de dados), defina o campo `Structured` com um JSON
Schema. O executor instrui o modelo a responder apenas com JSON, valida
a saida em Go, e tenta reparar ate `RepairMax` vezes se a validacao
falhar.

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

### Comportamento

- O modelo e instruido a responder **apenas** com JSON — sem markdown, sem texto adicional.
- A saida e validada contra o schema em Go (stdlib `encoding/json`).
- Se invalida, o executor reenvia ao modelo com os erros de validacao e
  pede que corrija o JSON. Isso se repete ate `RepairMax` vezes (padrao 2).
- Se o budget se esgotar, a tarefa falha com
  `crewai.ErrRepairBudgetExceeded`. **O executor nunca retorna JSON
  invalido ou inventa dados.**
- Em caso de sucesso, `Task.Output()` retorna o JSON **canonizado**
  (compactado, estavel).
- Quando `Structured` esta definido, as ferramentas e o loop ReAct sao
  ignorados; o executor segue direto para o caminho de saida estruturada.

### Palavras-chave de schema suportadas

O validador embutido suporta um subconjunto do JSON Schema: `type`,
`properties`, `required`, `enum`, `items`. Nao e uma implementacao
completa do JSON Schema e omite intencionalmente palavras-chave como
`additionalProperties`, `oneOf`/`anyOf`, `pattern`, `minimum`/`maximum`,
e `minItems`/`maxItems`.

### Erros

| Sentinela | Significado |
|---|---|
| `ErrInvalidOutput` | O modelo retornou JSON que nao valida contra o schema. |
| `ErrRepairBudgetExceeded` | Tentativas de reparo esgotadas; a tarefa falha. Envolve o ultimo erro de validacao. |
| `ErrToolCallStructuredUnsupported` | `ToolCall` e true mas a LLM do agente nao implementa `ToolCallingLLM`. |

As tres podem ser verificadas com `errors.Is`.

### Modo tool-call (`WithToolCall`)

Para provedores que nao suportam o parametro `format` com JSON Schema
(notavelmente o **Ollama Cloud**), o caminho mais confiavel e
declarar uma tool sintetica cujos parametros sao o schema e forcar o
modelo a chama-la. Os argumentos que o modelo passa ja chegam
parseados por toda implementacao de `ToolCallingLLM` em
`llm/openai.go`, `llm/ollama.go`, etc. — eles chegam como
`json.RawMessage`, nao como string.

```go
structured, _ := crewai.NewStructuredOutput(
    schema,
    crewai.WithToolCall(),
    crewai.WithRepairMax(3),
)
task.Structured = structured
```

O executor constroi `ToolSpec{Name: "emit_result", Parameters: schema}`,
pede ao modelo que chame exatamente uma vez e usa o campo `arguments`
da chamada como saida validada. Se o modelo retornar texto livre ou
os argumentos falharem na validacao, o loop de reparo existente
aciona (ate `RepairMax` tentativas). No esgotamento, a tarefa falha
com `ErrRepairBudgetExceeded`.

O modo tool-call exige que a LLM do agente implemente
`ToolCallingLLM`; caso contrario a tarefa falha com
`ErrToolCallStructuredUnsupported`. O modo JSON-only padrao e
preservado para backward compatibility — ative com `WithToolCall()`.

## Degradacao graciosa: warnings por tarefa

Uma tarefa que **sucedeu** ainda pode registrar diagnosticos nao
fatais quando uma fonte secundaria ficou indisponivel ou um passo nao
critico falhou. A tarefa em si sucede; o warning e preservado em
`TaskOutput.Warnings` e `CrewOutput.Warnings` para que o chamador
possa renderizar uma secao degradada (ex.: "secao indisponivel") em vez
de abortar a investigacao inteira.

Isso e distinto de `Stage.Optional` — estagios opcionais engolem
**falhas**; warnings representam **sucessos parciais**.

Registrando um warning em Go:

```go
task.AddWarning("servico de CNPJ retornou 503")
crewai.AddWarningFromCtx(ctx, "timeout de fonte secundaria")
```

`Task.AddWarning` e thread-safe (protegido por `sync.RWMutex`). Uma
ferramenta pode registrar warnings durante a execucao via contexto:

```go
func (t *myTool) Call(ctx context.Context, _ string) (string, error) {
    primary, err := t.primary(ctx)
    if err != nil { return "", err }
    secondary, err := t.secondary(ctx)
    if err != nil {
        // Fonte secundaria falhou — a tarefa ainda pode suceder.
        crewai.AddWarningFromCtx(ctx, "secundaria indisponivel: " + err.Error())
    }
    return primary + "\n" + secondary, nil
}
```

O executor (`Crew.execute`) injeta a tarefa como `WarningSink` no
`ctx` antes de invocar as ferramentas. `Agent.Execute` (standalone,
sem crew) nao injeta sink — chamadas a `AddWarningFromCtx` nesse modo
sao silenciosamente ignoradas.

### Expondo os warnings

Apos `Kickoff`:

```go
out, _ := crew.Kickoff(ctx, nil)
for _, to := range out.TasksOutput {
    for _, w := range to.Warnings {
        fmt.Printf("WARNING de %s: %s\n", to.Task, w)
    }
}
// Visao agregada (em ordem de execucao):
for _, w := range out.Warnings {
    fmt.Println("AGG:", w)
}
```
