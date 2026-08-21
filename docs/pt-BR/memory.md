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

## MemoryPolicy + barreira de commit (M2)

`Crew.MemoryPolicy` controla save/inject automáticos. `nil` significa os
padrões de `NewMemoryPolicy()` (`AutoSave=true`, `InjectWhenEmptyContext=true`,
orçamentos `DefaultLimit` / `DefaultMaxChars`). Um literal `MemoryPolicy{}`
**não** é esses padrões — use `NewMemoryPolicy` e sobrescreva campos.

```go
crew.Memory = true // garante store InMemory quando MemoryStore é nil
crew.Name = "minha-crew" // MemoryPolicy.Scope padrão (G3)
crew.MemoryPolicy = crewai.NewMemoryPolicy()
crew.MemoryPolicy.DefaultMaxChars = 2000
// ou forneça um store externo (app faz Close — D-M6):
// crew.MemoryStore = meuStore
```

**Invariante de visibilidade D-M7:** durante uma wave/stage paralela, os
writes de AutoSave vão para um buffer por tarefa. Eles só são commitados no
store **na barreira**, em ordem de declaração. O inject/`Query` da próxima
wave vê apenas o snapshot commitado — nunca writes de irmãos em voo. **Não**
use Memory como canal de merge entre irmãos paralelos; use `WithContext`.

Tarefas que falham nunca entram no AutoSave (G2). Erros de AutoSave geram
warn+capture; não abortam o Kickoff (G11).


## FileStore (JSONL, M3)

`OpenFileStore(dir)` abre um backend durável só-stdlib sob um root
**confiável pelo caller** (nunca passe paths controlados pelo modelo):

```go
store, err := crewai.OpenFileStore("/var/lib/myapp/crew-memory")
if err != nil { /* … */ }
defer store.Close() // app é dona do lifecycle (D-M6)

crew.Name = "finance-crew"       // MemoryPolicy.Scope padrão (G3)
crew.MemoryStore = store
crew.MemoryPolicy = crewai.NewMemoryPolicy()
```

Layout:

```
{root}/scopes/{urlsafeScope}/
  entries.jsonl   # linhas JSON de MemoryEntry append-only (+ tombstones)
  meta.json       # versão do schema, contador de linhas corrompidas
```

- Arquivos `0600`, diretórios `0700`.
- v1 é **single-writer** (G7): um processo por root; um `*FileStore` é
  seguro para Puts/Queries concorrentes dentro desse processo.
- Linhas JSONL corrompidas são **ignoradas** no Open e contadas em
  `meta.json` / `CorruptSkipped()` (D-M5).
- Sobrevive a restart: Put → Close → Open → Query.

Veja `examples/memory_file`.

## Interface de armazenamento de longo prazo

`*Memory` também implementa `crewai.MemoryStore`, o contrato de memória de
longo prazo plugável que os backends duráveis usarão:

```go
var store crewai.MemoryStore = crewai.NewMemory() // ou *Memory existente

e, _ := store.Put(ctx, crewai.MemoryEntry{Agent: "Analista", Content: "faturamento cresceu 12%"})
hits, _ := store.Query(ctx, crewai.MemoryQuery{Limit: 5})
_ = store.Delete(ctx, "", e.ID)
```

- `Put` atribui `ID` estável e `CreatedAt`; entradas acima de
  `MaxMemoryEntryBytes` são rejeitadas (`ErrMemoryEntryTooLarge`).
- `Query` respeita `Limit` (padrão `DefaultMemoryQueryLimit`, teto
  `MaxMemoryQueryLimit`) e `MaxChars` (padrão `DefaultMemoryMaxChars`,
  negativo = sem teto), buscando em `Content`/`Task` sem diferenciar
  maiúsculas. `Text` vazio devolve os registros mais recentes primeiro.
- `Delete` é idempotente; `Close` é no-op para o store em memória.
- Entradas são particionadas por `MemoryScope`; registros de `Save` vivem no
  escopo padrão (vazio).

`Crew.Memory` permanece o alias permanente v0.x que garante um store InMemory
quando `MemoryStore` é nil. Ranking por embeddings vem em M4.
