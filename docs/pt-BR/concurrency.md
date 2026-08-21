# Modelo de concorrência

> **Idiomas:** [English](../concurrency.md) · **Português** (atual)

Esta página é o contrato da biblioteca entre **data races** e **semantic
races** quando tarefas rodam em paralelo (stages Staged e waves
`Task.Async` em Sequential/Hierarchical). Expande as respostas do
[fio de concorrência no dev.to](https://dev.to/rhgs/from-python-to-go-rewriting-a-crewai-workflow-in-pure-stdlib-47nm#comments)
e reflete a implementação a partir de **v0.6+** (waves async + barreira de
memória D-M7) e **v0.7+** (demux de streaming).

## Dois problemas diferentes

| Preocupação | O que o `-race` pega | O que a orquestração deve garantir |
|---|---|---|
| **Data race em memória** | Leitura/escrita concorrente da mesma variável Go sem sync | Mutexes, channels, Kickoff single-flight, testes race-clean |
| **Semantic race** | *Nada* — o programa pode estar limpo no race detector e ainda ser não-determinístico | Se a **ordem de conclusão** pode mudar prompts, `CrewOutput` ou memória lida depois |

`go test -race` é **necessário, mas não suficiente**. O crewai-go trata a
ordem de merge como **contrato de orquestração**, não como acidente de
scheduling.

## Independência dentro de um grupo paralelo

**Tarefas intra-stage / intra-wave são independentes enquanto o grupo roda.**

- Elas **não** leem outputs, facts ou warnings umas das outras em voo.
- Arestas `Task.Context` no **mesmo** grupo **não** são canal de merge para
  irmãos. Em waves Async, a dependência precisa terminar em uma wave
  **estritamente anterior**, senão o Kickoff falha com
  `ErrTaskDependencyCycle` (G12).
- No Staged, o grafo pretendido é: **stage N pode depender de stages &lt; N**,
  não de pares no mesmo stage (pares rodam em paralelo).

Se dois especialistas alimentam um sintetizador, coloque o sintetizador em
um **stage/wave posterior** e ligue com `WithContext(...)`.

## Barreira e fold determinístico

Todo grupo paralelo é uma **barreira**, depois um **fold por ordem de
declaração**:

1. Cada tarefa escreve em um **slot indexado pela declaração** (não por quem
   terminou primeiro).
2. Após a barreira, a biblioteca agrega na **ordem do slice**:
   `TasksOutput`, `Facts` / `Warnings` / `ToolTraces`, e `Final` conforme o
   caminho.
3. Testes como `TestStagedDeterministicOrder` cobrem slow-first / fast-second
   para a ordem de conclusão não embaralhar o fold.

## Como o próximo stage/wave vê o trabalho anterior

| Canal | Ordenação | Notas |
|---|---|---|
| **`Task.WithContext`** | Lista explícita, percorrida na **ordem de declaração**; lê outputs write-once com mutex | Canal preferido de merge entre irmãos paralelos |
| **Pipeline de stages** | Stages seguintes só começam após a barreira do anterior | Pares não se injetam no meio do stage |
| **Facts em `CrewOutput`** | Fold em ordem de declaração; `dedupFacts` por `PayloadHash` (primeira ocorrência vence) | Facts **não** são auto-injetados no próximo prompt |
| **Inject de Memory** | Vê só o **snapshot commitado** após a barreira do grupo (D-M7) | Não use Memory como barramento de merge entre irmãos |

## Memory: o caveat histórico e o D-M7

### A pergunta

Se `Memory.Save` de curto prazo seguisse a **ordem de conclusão**, uma tarefa
posterior com `Context` vazio e `InjectWhenEmptyContext` poderia observar
irmãos na ordem de wall-clock — semantic race que o `-race` não marca.

### O contrato hoje (D-M7)

Em **waves paralelas e stages Staged**, o AutoSave **não** publica no store
ao vivo no meio do grupo:

1. Tarefas bem-sucedidas **guardam** entradas em um buffer por grupo.
2. Na barreira, `commitMemoryBuffer` grava em **ordem de declaração**.
3. O inject da próxima wave/stage (`queryCommitted`) lê só esse snapshot
   commitado — **nunca** writes de irmãos em voo.

Tarefas que falham/cancelam **não** entram no buffer (G2). Abort fail-fast
**descarta** o buffer.

Memory deixa de ser um “event log implícito da ordem de conclusão” em grupos
paralelos. Continua **não** substituindo `WithContext` quando você precisa de
dependência dirigida e precisa: use Context na aresta de merge; use Memory
para recall orçado do passado **commitado**.

`Memory.Save` avulso fora do Kickoff permanece ordem de inserção (problema de
quem chama). FileStore é single-writer por root na v1.

### Linha entre “não-determinismo de propósito” e “invariante de API”

| Não-determinismo permitido | Proibido / prevenido |
|---|---|
| Ordem de término wall-clock no grupo | Ordem de fold de `TasksOutput` / texto de Context / commit D-M7 |
| Conteúdo de tokens do LLM | Ciclos de Context na mesma wave (fail fast) |
| Qual ramo paralelo toma rate limit primeiro | Kickoff single-flight no mesmo `*Crew` |
| Interleaving de deltas de stream no sink | Demux via `StreamChunk.Task` / `Agent` (app ainda precisa de mutex no sink) |

Estados ilegais ficam **difíceis no Kickoff** (ciclos) ou **impossíveis por
construção** (fold por índice, buffer D-M7). Não fingimos que I/O de LLM é
determinístico.

## Streaming e callbacks sob concorrência

- **`WithStream`**: providers podem rodar em paralelo em waves Async; chunks
  carregam `Task` / `Agent` para demux. O sink **deve** ser concurrency-safe.
- **`WithProgress` / `WithEvents`**: podem disparar de várias goroutines.
  Eventos só metadados; Progress nunca carrega corpos.
- **`Progress` + `Events`**: dual-emit para lifecycle no formato Progress;
  evite tratar duas vezes se combinar `ProgressAsEvents` **e** `WithEvents`.

## Kickoff single-flight

Só **um** `Kickoff` por vez em um `*Crew`. Chamadas concorrentes retornam
`ErrCrewRunning`. Crews em paralelo ⇒ valores `Crew` separados.

## Receitas práticas

```go
// Bom: research paralelo, merge determinístico
a := NewTask("research A", "...", agentA).WithAsync()
b := NewTask("research B", "...", agentB).WithAsync()
merge := NewTask("merge", "...", writer).WithContext(a, b)

// Bom: Staged collect → synthesize
crew.Process = Staged
crew.Stages = []Stage{
    {Name: "collect", Tasks: []*Task{a, b}},
    {Name: "synthesize", Tasks: []*Task{merge}},
}

// Evite: depender só de Memory para ordenar outputs de irmãos
// Prefira WithContext ou stage/wave posterior.
```

## Relacionado

- [Crews](crews.md) — waves Async, Staged, single-flight  
- [Memory](memory.md) — barreira D-M7, Context vs Memory  
- [Tasks](tasks.md) — `WithContext`, `WithAsync`  
- Arquivos de design: `Plan/PLAN.memory-async.md`, `Plan/PLAN.streaming.md`

## Âncoras de teste

| Teste / área | Protege |
|---|---|
| `TestStagedDeterministicOrder` | Fold ≠ ordem de conclusão |
| Testes Async + Context | Deps só em waves anteriores |
| Testes D-M7 de memory policy | Ordem de commit / snapshot de inject |
| Testes de demux de stream Async | Labels Task sob concorrência |
| `go test -race` | Data races em estruturas compartilhadas |
