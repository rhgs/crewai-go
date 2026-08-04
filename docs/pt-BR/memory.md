# Memory

> **Languages:** [English](../memory.md) · **Português** (atual)

A **memória** guarda as saídas das tarefas ao longo da execução de uma crew,
permitindo que tarefas posteriores tenham acesso ao que já foi produzido —
mesmo sem um `WithContext` explícito.

## Ativando

```go
crew := crewai.NewCrew(agentes, tarefas)
crew.Memory = true
crew.Kickoff(ctx, nil)
```

Com `Memory = true`, cada tarefa sem contexto explícito recebe, no prompt, um
resumo da memória acumulada até ali.

## Lendo a memória

```go
mem := crew.MemorySnapshot() // *crewai.Memory (nil se Memory == false)
for _, r := range mem.Records() {
	fmt.Printf("[%s] tarefa=%q → %s\n", r.Agent, r.Task, r.Content)
}
```

Cada registro é um `MemoryRecord`:

```go
type MemoryRecord struct {
	Agent   string // papel do agente que produziu
	Task    string // nome da tarefa
	Content string // a saída memorizada
}
```

## Buscando

Busca textual simples (substring, _case-insensitive_):

```go
for _, r := range mem.Search("vendas") {
	fmt.Println(r.Content)
}
```

Uma busca vazia devolve todos os registros.

## Usando a memória avulsa

`Memory` também pode ser usada isoladamente:

```go
m := crewai.NewMemory()
m.Save(crewai.MemoryRecord{Agent: "Analista", Content: "faturamento cresceu 12%"})
fmt.Println(m.String())
```

## Contexto x Memória

- **Contexto** (`WithContext`) é **explícito e direcionado**: você diz
  exatamente quais saídas alimentam uma tarefa.
- **Memória** é **implícita e cumulativa**: fica disponível a todas as tarefas
  seguintes que não definiram contexto próprio.

Use contexto para dependências precisas; use memória para dar à equipe uma
"consciência" geral do que já foi feito.

## Implementações customizadas

A `Memory` embutida é em RAM e segura para concorrência. Para busca semântica
(embeddings) ou persistência, você pode envolver/ substituir essa lógica na sua
aplicação — a estrutura de `MemoryRecord` é intencionalmente simples.
