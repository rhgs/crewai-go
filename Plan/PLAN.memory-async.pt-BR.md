# Plano — Memória de longo prazo & Async além do Staged

> **Status:** Apenas plano — não implementar até agendar e fechar decisões abertas.  
> **Relacionado:** `Memory` em RAM (`memory.go`), `Crew.Memory` / `MemorySnapshot`, `Process=Staged` (`runStaged`), itens de roadmap em `PLAN.pt-BR.md` §6 P1/P2.  
> **Restrições:** zero dependências externas no módulo core (`go.mod` só stdlib). Quality gates da §6.1 de `PLAN.security-residuals.pt-BR.md` valem em todo PR (cobertura ≥ 90% nos pacotes tocados, race-clean, docs EN+PT-BR, CHANGELOG).

---

## 0. Por que os dois juntos

| Capacidade | Hoje | Lacuna |
|---|---|---|
| **Memória** | Saco in-RAM por Kickoff de `MemoryRecord`; `Search` por substring; injetada só se `Task.Context` vazio | Sem persistência entre runs, sem recall semântico, sem escopo (crew/agent/sessão), sem injeção orçada no prompt |
| **Async** | Paralelismo **só** dentro de um estágio Staged (`WaitGroup` + cancel-on-fail) | Sequential/Hierarchical sempre seriais; sem DAG de tasks independentes; sem `Task.Async` / scheduler em waves |

Eles interagem: waves async gravam memória em paralelo (store race-safe); stores de longo prazo não podem assumir Kickoff single-threaded. Um plano único evita APIs incompatíveis.

**Non-goals:**
- Streaming de tokens LLM (P1 separado).
- Vector DB / RAG cloud dentro do módulo core.
- Substituir o processo Staged (Staged permanece; async estende Sequential/Hierarchical).
- Cgo ou drivers de DB obrigatórios em `github.com/rhgs/crewai-go`.

---

## 1. Baseline (código na v0.5.0)

### 1.1 Memória hoje

```go
type Memory struct { /* RWMutex + []MemoryRecord */ }
type MemoryRecord struct { Agent, Task, Content string }

// Crew
Memory bool          // se true, Kickoff faz c.mem = NewMemory()
mem    *Memory
MemorySnapshot() *Memory

// Crew.execute
contextText := task.contextText()
if c.mem != nil && contextText == "" {
    contextText = c.mem.String()   // despeja TODOS os records no prompt
}
// após sucesso:
c.mem.Save(MemoryRecord{Agent, Task: task.Name, Content: result})
```

Implicações:
- Memória é **escopo Kickoff** e some com o `Crew`.
- Injeção é **tudo-ou-nada** e só sem cadeia `WithContext` — crews grandes estouram a janela de contexto.
- Sem timestamps, IDs, namespaces, importância ou embeddings.
- Docs já dizem que embeddings/persistência são “lado da aplicação”.

### 1.2 Concorrência hoje

| Processo | Paralelismo |
|---|---|
| `Sequential` | Nenhum |
| `Hierarchical` | Nenhum — manager escolhe agent e executa serial |
| `Staged` | Tasks do estágio concorrentes; estágios seriais; optional continua em falha; recover de panic |

`Kickoff` é single-flight (`ErrCrewRunning`). Staged já prova o padrão: `stageCtx` derivado, `WaitGroup`, agregação ordenada, first-error.

### 1.3 Sinal de dependência já existente

`Task.Context []*Task` é a lista explícita de dependências usada em `contextText()`. O scheduler async deve **reutilizar** isso como arestas do DAG (sem segunda DSL de deps na v1).

---

## 2. Parte A — Memória de longo prazo

### 2.1 Objetivos

1. **Store plugável** para persistir/recarregar memória entre restarts.
2. **Recall consultável** além de substring: keyword/`Search` e, opcionalmente, similaridade por embedding **sem** forçar vector dep no core.
3. **Injeção orçada no prompt** (top-K, max chars) para memória não fazer DoS na janela de contexto.
4. **Backward compatible**: `Crew.Memory bool` e tipo `Memory` atuais continuam válidos.
5. **Facts permanecem separados** — `Fact`/`FactSource` são dados com proveniência; long-term memory é contexto episódico/soft, não substituto de Facts.

### 2.2 Design em camadas

```
┌─────────────────────────────────────────────────────────┐
│ Crew / executor                                         │
│  - MemoryPolicy (quando/como injetar e salvar)          │
│  - MemorySession (handle de run no Kickoff)             │
└──────────────────────────┬──────────────────────────────┘
                           │ usa
┌──────────────────────────▼──────────────────────────────┐
│ MemoryStore (interface)                                 │
│  Put / Query / Delete / Close                           │
└────────────┬─────────────────────────────┬──────────────┘
             │                             │
   ┌─────────▼─────────┐         ┌─────────▼──────────────┐
   │ InMemoryStore     │         │ FileStore (stdlib)     │
   │ (envolve/estende  │         │ JSONL append + índice  │
   │  Memory atual)    │         │ em diretório           │
   └───────────────────┘         └────────────────────────┘
             │                             │
             └────────── opcional ─────────┘
                    EmbeddingFunc (app)
                    para Query semântico
```

### 2.3 Esboço de API pública (core só stdlib)

```go
type MemoryScope string

type MemoryEntry struct {
    ID        string
    Scope     MemoryScope
    Agent     string
    Task      string
    Content   string
    CreatedAt time.Time
    Metadata  map[string]string
    Embedding []float32 // opcional
}

type MemoryQuery struct {
    Scope     MemoryScope
    Text      string
    Embedding []float32
    Limit     int
    MaxChars  int
}

// MemoryStore — seguro para uso concorrente.
type MemoryStore interface {
    Put(ctx context.Context, e MemoryEntry) (MemoryEntry, error)
    Query(ctx context.Context, q MemoryQuery) ([]MemoryEntry, error)
    Delete(ctx context.Context, scope MemoryScope, id string) error
    Close() error
}

// EmbeddingFunc fornecida pela aplicação (HTTP OpenAI/Ollama/etc.).
// Core nunca embute modelo de embedding.
type EmbeddingFunc func(ctx context.Context, texts []string) ([][]float32, error)

type MemoryPolicy struct {
    AutoSave               bool
    AutoEmbed              bool
    InjectWhenEmptyContext bool // default true = comportamento atual
    DefaultLimit           int
    DefaultMaxChars        int
    Scope                  MemoryScope
}

// Em Crew:
//   Store MemoryStore
//   MemoryPolicy *MemoryPolicy
//   Embed EmbeddingFunc
//   Memory bool   // alias: true ⇒ garante InMemoryStore neste Kickoff
```

**Ponte de compatibilidade:** fazer `*Memory` implementar `MemoryStore` (`Put`/`Query` ↔ `Save`/`Search`, `Close` no-op) para testes atuais continuarem verdes.

### 2.4 Stores embutidos

| Store | Onde | Persistência | Busca |
|---|---|---|---|
| **InMemoryStore** | root | Vida do processo | substring (+ cosseno se houver embedding) |
| **FileStore** | root (ou subpacote sem ciclo) | Diretório JSONL + índice | substring; cosseno se embeddings |

**Formato FileStore (v1):**

```
{root}/
  scopes/{urlsafeScope}/
    entries.jsonl
    meta.json
```

- Índice em RAM no `Open` (ok para dezenas de milhares de entradas curtas).
- `Put` append + update sob mutex.
- Modo arquivo `0600`, dirs `0700`.
- Path **trusted** do caller (mesma classe de `OutputDir`).

**Adiado de propósito:** SQLite **no core** (política zero-deps). Exemplo externo ou módulo opcional futuro — fora dos PRs core deste plano.

### 2.5 Embeddings / busca semântica (opcional)

1. App seta `Crew.Embed`.
2. Com `AutoEmbed`, `Put` gera embedding.
3. `Query` com embedding (ou texto embutido na hora) ranqueia por cosseno; aplica `Limit`/`MaxChars`.
4. Sem embeddings → fallback substring.

### 2.6 Política de injeção no prompt

Substituir `c.mem.String()` cego por `store.Query` + bloco formatado e limitado.

Constantes:
- `MaxMemoryQueryLimit = 32`
- `MaxMemoryEntryBytes = 32 KiB`
- `DefaultMemoryMaxChars = 4000`

### 2.7 Memória vs Facts

| | Memory | Facts |
|---|---|---|
| Fonte | Em geral outputs de tasks (soft) | Só tools `FactSource` |
| Confiança | Não serve para compliance sozinha | Proveniência |
| Uso | Contexto de prompt / recall | Guardrails, auditoria, relatório |

Não copiar Facts automaticamente para MemoryStore na v1.

### 2.8 Breakdown de PRs — Memória

| PR | Entrega |
|---|---|
| **M1** | `MemoryEntry`, `MemoryStore`, `MemoryQuery`; `*Memory` como store; testes |
| **M2** | `MemoryPolicy` + wiring no Crew; `Memory bool` compatível |
| **M3** | `FileStore` JSONL + docs EN/PT + `examples/memory_file` |
| **M4** | `EmbeddingFunc` + query cosseno + exemplo com embedder fake |
| **M5** (opt) | Tools `recall_memory` / `remember` para o agent |

### 2.9 Testes e gates — Memória

- Put/Query/Delete com `-race`, limites, escopos, content oversized.
- FileStore: durabilidade restart; linha corrompida (**D-M5**).
- Integração Crew: `Memory bool`; inject respeita MaxChars; async+store sem race.
- Cobertura ≥ 90%; docs bilíngues; CHANGELOG.

### 2.10 Decisões abertas — Memória

| ID | Pergunta | Opções | Rec |
|---|---|---|---|
| **D-M1** | Store default se `Memory=true` e `Store==nil`? | (A) InMemory (B) exigir Store | **A** |
| **D-M2** | FileStore no root vs subpacote? | (A) root (B) subpacote | **A** se sem ciclo |
| **D-M3** | Injetar quando já há `Context`? | (A) nunca (B) append (C) flag | **C** default A |
| **D-M4** | Texto da query de injeção | (A) latest N (B) keywords da task (C) embed | **A** v1 |
| **D-M5** | Linha JSONL corrompida | (A) falha Open (B) skip + aviso | **B** |
| **D-M6** | Quem dá Close no FileStore? | (A) app (B) Crew.Close | **A** (+ flag owner opcional) |

---

## 3. Parte B — Async além do Staged

### 3.1 Objetivos

1. Rodar tasks **independentes** em paralelo fora do Staged, com opt-in.
2. Honrar **dependências** via `Task.Context` (DAG).
3. Agregação **determinística** (ordem de declaração), como Staged.
4. Política de falha clara (fail-fast vs coletar).
5. Staged inalterado; **compartilhar primitiva** de paralelismo.

### 3.2 Modelo mental

```
Aresta t → d em t.Context significa: t depende de d (d antes de t).

Wave 0: tasks sem deps pendentes
Wave 1: desbloqueadas após wave 0
…
```

Ciclo → erro antes de executar (`ErrTaskDependencyCycle`).

### 3.3 Esboço de API

```go
// Task.Async — elegível a rodar junto com outras elegíveis quando deps ok.
// Default false ⇒ comportamento atual.
Async bool

func (t *Task) WithAsync() *Task

// Crew:
// AsyncMaxWorkers int // 0 = ilimitado (até o nº de ready tasks)
// AsyncFailFast bool  // default true
```

**Interação com Process:**

| Process | Comportamento |
|---|---|
| `Sequential` | DAG sobre `c.Tasks`; waves com regra Async |
| `Hierarchical` | Mesmo DAG; resolve agent (manager) antes do execute |
| `Staged` | **Inalterado.** `Task.Async` ignorado (documentar). Reusa `runTaskGroup` por baixo |

### 3.4 Regra recomendada para Sequential (D-A1)

**Waves para todas; `Async` só controla se várias ready podem iniciar juntas:**

- R = tasks com deps satisfeitas.
- P = ready com `Async`; S = ready sem `Async`.
- Se P ≠ ∅: roda P em paralelo (até MaxWorkers).
- Senão: roda uma de S (ordem estável por índice).
- Non-Async nunca correm em paralelo entre si; deps sempre respeitadas.

### 3.5 Primitiva compartilhada

Extrair de `runStaged`:

```go
func (c *Crew) runTaskGroup(
    ctx context.Context,
    label string,
    tasks []*Task,
    agents []*Agent,
    failFast bool,
) (results []groupResult, firstErr error)
```

- `runStaged` → um `runTaskGroup` por estágio.
- Sequential/Hierarchical → waves do DAG chamam `runTaskGroup` no subconjunto paralelo.

### 3.6 Falha e cancel

- **FailFast true (default):** cancela ctx da wave; espera WG; devolve first error real (ignora `context.Canceled` de siblings quando possível).
- **FailFast false:** termina a wave; **D-A3** — recomendação: pular só dependentes da task que falhou; ramos independentes seguem.

### 3.7 Segurança memória/output sob async

- Store/`Memory` com mutex — OK.
- Um goroutine por task no output — OK.
- Agregar `CrewOutput` só após join.
- Progress já exige callback concorrente-safe.
- Interpolação de inputs continua no Kickoff (single-thread).

### 3.8 Hierarchical + async

Recomendação: **pré-resolver agents em série** (chamadas ao manager), depois execute async na wave.

### 3.9 Breakdown de PRs — Async

| PR | Entrega |
|---|---|
| **A1** | Extrair `runTaskGroup`; testes golden do Staged iguais |
| **A2** | DAG + detecção de ciclo + planejador de waves |
| **A3** | Sequential (+ Hierarchical) com `Task.Async` + flags do Crew |
| **A4** | Docs EN/PT, `examples/async_tasks`, CHANGELOG |
| **A5** (opt) | Açúcar `Process=DAG` se Sequential+Async confundir |

### 3.10 Testes e gates — Async

- Ciclo, waves, ordem estável.
- Duas Async independentes com overlap temporal (rendezvous).
- Dependente só após upstream `done`.
- FailFast cancel; panic recovery.
- `-race` com saves de memória.
- Cobertura ≥ 90%.

### 3.11 Decisões abertas — Async

| ID | Pergunta | Opções | Rec |
|---|---|---|---|
| **D-A1** | Regra mixed Async/sync | (A) wave acima (B) serial global exceto subgrafo Async | **A** |
| **D-A2** | Resolve hierarchical | (A) pré-serial (B) lazy paralelo | **A** |
| **D-A3** | FailFast false | (A) skip dependentes (B) abort crew | **A** |
| **D-A4** | Default MaxWorkers | (A) 0 unlimited (B) GOMAXPROCS (C) 8 | **A** + docs |
| **D-A5** | Staged × Async | (A) ignora flag (B) erro se set | **A** |
| **D-A6** | Novo Process vs flag | (A) Sequential+Async (B) Process=Async | **A** |

---

## 4. Transversal

### 4.1 Zero dependencies

| Necessidade | Abordagem |
|---|---|
| IDs | `crypto/rand` hex |
| Persistência | JSON/JSONL + files |
| Embeddings | `EmbeddingFunc` da app |
| Vector DB / SQLite no core | Fora |

### 4.2 Segurança

- Roots de FileStore trusted.
- Caps de tamanho/entrada/query.
- Append atômico o suficiente sob lock (linha completa + flush).
- `ctx` em I/O e embedder.

### 4.3 Observabilidade

- Reusar `task_started` / `task_completed`.
- `wave_*` opcional (default não na v1).
- Debug: wave index, hits de memória (sem content completo em Info).

### 4.4 Versionamento

| Fatia | Release sugerida |
|---|---|
| M1–M2 + A1–A4 | **v0.6.0** (ideal conjunto) |
| M3 FileStore | v0.6.0 ou patch |
| M4 embeddings | v0.6.x |

### 4.5 Quality gates (todo PR)

1. Code review (trust, caps, ctx, race).  
2. `go test` + `-race`.  
3. Cobertura ≥ 90%.  
4. Docs EN+PT-BR + godoc + CHANGELOG.  
5. Sem novos `require` no `go.mod`.  
6. `gofmt` / `go vet`.

---

## 5. Ordem sugerida de implementação

```
Fase 0  Fechar D-M* e D-A* (RFC curto no PR)
Fase 1  A1 runTaskGroup  ||  M1 MemoryStore    (arquivos disjuntos)
Fase 2  A2 DAG           ||  M2 MemoryPolicy
Fase 3  A3 wire Async    ||  M3 FileStore
Fase 4  A4 + M4 docs/examples/embeddings
Fase 5  Opcionais M5/A5
```

Partição de arquivos:
- Async: `crew.go`, `schedule.go` (novo), testes de crew/staged.
- Memory: `memory.go`, `memory_store.go`, `memory_file.go`, testes, `docs/memory.md`.

---

## 6. Documentação

| Doc | Atualizações |
|---|---|
| `docs/memory.md` + pt-BR | Store, FileStore, policy, embeddings, vs Facts |
| `docs/crews.md` + pt-BR | Waves async, FailFast, MaxWorkers, Staged igual |
| `docs/tasks.md` + pt-BR | `Task.Async`, Context como deps do DAG |
| README EN/PT | Bullets na release |
| `examples/memory_file` | Dois Kickoffs com o mesmo dir |
| `examples/async_tasks` | Duas researches paralelas → merge |
| `Plan/PLAN.md` | Checkboxes ao mergear |

---

## 7. Critérios de aceite (épico)

### Memória de longo prazo
- [ ] Interface `MemoryStore` + in-memory; testes de `Memory bool` passam.
- [ ] Inject com caps; sem dump ilimitado quando policy usa limites.
- [ ] FileStore sobrevive restart nos testes.
- [ ] Path de embedding com fake embedder.
- [ ] Docs bilíngues + example.

### Async além do staged
- [ ] `Task.Async` independentes com overlap temporal no Sequential.
- [ ] `Task.Context` enforced; ciclos com erro claro.
- [ ] Staged golden inalterado.
- [ ] FailFast + panic recovery.
- [ ] `-race` limpo com memory saves.
- [ ] Docs bilíngues + example.

### Global
- [ ] `go.mod` sem novas deps.
- [ ] Gates de cobertura.
- [ ] CHANGELOG EN/PT até a tag de release.

---

## 8. Riscos

| Risco | Mitigação |
|---|---|
| Prompt bloat | MaxChars + Limit default |
| Confusão Async vs Staged | Tabela comparativa nas docs |
| Crescimento FileStore | Cap de entry; compactação depois |
| Gargalo do manager | Pré-resolve serial |
| Scope creep RAG | Só hook de embedding; sem pipeline de chunk no core |
| Ciclo de import | Interfaces no root |

---

## 9. Decision log (preencher antes de codar)

| ID | Decisão | Data | Notas |
|---|---|---|---|
| D-M1…D-M6 | _TBD_ | | ver §2.10 |
| D-A1…D-A6 | _TBD_ | | ver §3.11 |

---

## 10. Status tracking

| Workstream | Fase | PRs | Status |
|---|---|---|---|
| Memory interfaces + policy | P2 | M1–M2 | Planejado |
| FileStore | P2 | M3 | Planejado |
| Embeddings hook | P2 | M4 | Planejado |
| runTaskGroup | P1 | A1 | Planejado |
| DAG + Async | P1 | A2–A4 | Planejado |
| Tools / sugar opcional | P3 | M5/A5 | Adiado |

