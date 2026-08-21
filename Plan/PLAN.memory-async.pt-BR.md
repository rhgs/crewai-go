# Plano — Memória de longo prazo & Async além do Staged

> **Status:** Apenas plano — não implementar até agendar e fechar decisões abertas.  
> **Relacionado:** `Memory` em RAM (`memory.go`), `Crew.Memory` / `MemorySnapshot`, `Process=Staged` (`runStaged`), itens de roadmap em `PLAN.pt-BR.md` §6 P1/P2.  
> **Restrições:** zero dependências externas no módulo core (`go.mod` só stdlib). Quality gates da §6.1 de `PLAN.security-residuals.pt-BR.md` valem em todo PR (cobertura ≥ 90% nos pacotes tocados, race-clean, docs EN+PT-BR, CHANGELOG).  
> **Feedback externo:** thread no DEV.to sobre race **semântica** vs `-race` ([artigo](https://dev.to/rhgs/from-python-to-go-rewriting-a-crewai-workflow-in-pure-stdlib-47nm) — freerave): merge é contrato de orquestração (fold por ordem de declaração após barreira). Caveat de Memory por completion order → **D-M7** + reforço **D-M3**/**D-M4**. Reforçados **D-A1**/**D-A5**/**D-A6**.

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

**Visibilidade (ver D-M7):** inject/`Query` usados pelo orquestrador SÓ veem entries **commitadas** em barreira de wave/stage anterior (ou Kickoffs anteriores). Writes de siblings em voo são invisíveis na montagem do prompt. “Latest N” (**D-M4-A**) = últimos N do **snapshot commitado**, ordem estável `(seq de commit / índice de declaração, ID)` — nunca append/completion order da wave aberta.

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
| **M2** | `MemoryPolicy` + wiring no Crew; **Save bufferizado + commit na barreira (D-M7)**; deprecar dump-all | `Memory bool` compat; sem visibilidade mid-wave |
| **M3** | `FileStore` JSONL + docs EN/PT + `examples/memory_file` |
| **M4** | `EmbeddingFunc` + query cosseno + exemplo com embedder fake |
| **M5** (opt) | Tools `recall_memory` / `remember` para o agent |

### 2.9 Testes e gates — Memória

- Unit: Put/Query/Delete com `-race`, limit/maxchars, scope, content oversized.
- FileStore: durabilidade restart; linha corrompida (**D-M5**).
- Integração Crew: `Memory bool`; caps de inject; async sem data-race no store.
- **Ordem semântica:** `TestMemoryCommitOrderMatchesDeclaration` — slow-first / fast-second → sequência commitada = **índice de declaração**, não tempo de término (espelha `TestStagedDeterministicOrder`). Inject da próxima wave não vê entries de siblings não commitadas.
- Cobertura ≥ 90%; docs `docs/memory.md` + pt-BR; elevar caveat do DEV a **invariante de API** (Memory não é canal de merge de siblings paralelos — use `WithContext`).

### 2.10 Decisões abertas — Memória

| ID | Pergunta | Opções | Rec |
|---|---|---|---|
| **D-M1** | Store default se `Memory=true` e `Store==nil`? | (A) InMemory (B) exigir Store | **A** — zero surpresa *(thread DEV não muda)* |
| **D-M2** | FileStore no root vs subpacote? | (A) root (B) subpacote | **A** se sem ciclo (v1 rápido); **B** `memoryfile` se crescer/ciclo *(ainda escolha de produto)* |
| **D-M3** | Injetar quando já há `Context`? | (A) nunca (B) append (C) flag | **C** default **A**. **Reforço:** mesmo com inject, nunca expor entries **in-wave / não commitadas** (D-M7). Auto-inject não pode ser merge oculto de siblings. |
| **D-M4** | Texto da query de injeção | (A) latest N (B) keywords da task (C) embed | **A** v1; **B** depois. **Reforço:** latest N só no **snapshot commitado**, ordem estável — não completion order ao vivo. |
| **D-M5** | Linha JSONL corrompida | (A) falha Open (B) skip + aviso | **B** + contador em meta *(igual)* |
| **D-M6** | Quem dá Close no FileStore? | (A) app (B) Crew.Close | **A** + docs; Close automático só com `StoreOwner` opcional *(igual)* |
| **D-M7** | Quando `Save`/`Put` da task fica visível ao inject/`Query` da orquestração? | (A) event-log ao vivo (completion order) (B) buffer por task + fold na barreira em ordem de declaração (C) híbrido: Put cru imediato, orquestrador só lê commitado | **B** (preferido). Alinha ao contrato Staged de Facts/TasksOutput e responde o follow-up do DEV. **C** se app precisar de tail wall-clock. **A** rejeitado no caminho de prompt. |

#### 2.10.1 Invariantes de orquestração (API deve dificultar violar)

| Canal | Merge determinístico? | Como impor |
|---|---|---|
| `Task.Context` / `WithContext` | Sim | Só deps já **done**; mesma wave / ciclo → erro |
| Facts / `TasksOutput` / `Final` | Sim | Slot por task + fold por **índice de declaração** após barreira |
| Memory auto-save → auto-inject (mesmo Kickoff, waves paralelas) | **Sim (D-M7=B)** | Buffer por task; commit na barreira em ordem de declaração; inject só vê commitado |
| Memory como log cross-Kickoff / debug | Pode ser cronológico | Fora do caminho quente do prompt; documentar à parte |

Docs públicas elevam o caveat do DEV a **invariante**: *não use Memory como canal de merge de siblings paralelos — use `WithContext`.*

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

### 3.7 Segurança memória/output sob async (data races **e** races semânticas)

`-race` é necessário, mas não suficiente (DEV.to / freerave). Merge faz parte do **contrato de orquestração**, não é acidente de scheduling.

**Plano de dados (mutex / ownership):**
- Store/`Memory` com mutex para `Save`/`Put` concurrentes.
- Um goroutine por task no output.
- Agregar `CrewOutput` só após join da wave.
- Progress callback concorrente-safe.
- Interpolação de inputs no Kickoff (single-thread).

**Plano semântico (visibilidade / ordem) — D-M7 + D-A1:**
- Tasks na mesma wave são **independentes**: não leem outputs, facts nem memory **commitada** uns dos outros enquanto a wave está aberta.
- Arestas `Task.Context` na mesma wave são inválidas (dep not-yet-done / ciclo → erro), no mesmo espírito do Staged (“sem Context same-stage”).
- Após `wg.Wait()`, fold por **índice de declaração** (não completion order) em `TasksOutput`, Facts, Warnings, Final.
- Memory: workers escrevem em **buffer por task**; a barreira faz commit em ordem de declaração. Inject/`Query` da próxima wave só vê o snapshot pós-commit.
- **Testes de invariante:** slow-first/fast-second também para ordem de commit da Memory.

Não documentar completion order de Memory como “by design” no caminho de prompt. Não-determinismo residual (traces, métricas brutas) fica fora da montagem do prompt.

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
- Semântica: commit de Memory == ordem de declaração após wave paralela; inject da próxima wave não vê saves in-flight de siblings.
- Cobertura ≥ 90%.

### 3.11 Decisões abertas — Async

| ID | Pergunta | Opções | Rec |
|---|---|---|---|
| **D-A1** | Regra mixed Async/sync | (A) wave acima (B) serial global exceto subgrafo Async | **A** — **reforçado pelo thread DEV**: wave + barreira + agregação por índice de declaração (mesmo contrato do Staged). Explicitar nas docs públicas. |
| **D-A2** | Resolve hierarchical | (A) pré-serial (B) lazy paralelo | **A** *(thread não muda)* |
| **D-A3** | FailFast false | (A) skip dependentes (B) abort crew | **A** *(igual)* |
| **D-A4** | Default MaxWorkers | (A) 0 unlimited (B) GOMAXPROCS (C) 8 | **A** + aviso de custo de fan-out LLM nas docs *(igual)* |
| **D-A5** | Staged × Async | (A) ignora flag (B) erro se set | **A** — **reforçado**: Staged já define paralelismo por stage; não criar segunda regra. Opcional: `slog` Warn se `Async=true` sob Staged. |
| **D-A6** | Novo Process vs flag | (A) Sequential+Async (B) Process=Async | **A** no v1 — menos conceitos. **Reforço:** contrato real é “DAG + barreira + fold”; se “Sequential com Async” confundir, **A5** opcional (`Process=DAG`) depois sem quebrar A. |

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
Fase 0  Fechar D-M1–D-M7 e D-A1–D-A6 (RFC curto no PR)
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
| `docs/memory.md` + pt-BR | Store, FileStore, policy, embeddings, vs Facts; **visibilidade D-M7 / barreira de commit**; invariante: Memory ≠ canal de merge de siblings — use `WithContext` (elevar caveat do DEV a contrato de API) |
| `docs/crews.md` + pt-BR | Waves async, barreira + fold por declaração, FailFast, MaxWorkers, Staged igual |
| `docs/tasks.md` + pt-BR | `Task.Async`, Context como deps do DAG; Context same-wave proibido |
| README EN/PT | Bullets na release |
| `examples/memory_file` | Dois Kickoffs com o mesmo dir |
| `examples/async_tasks` | Duas researches paralelas → merge (`WithContext` explícito, não Memory) |
| `Plan/PLAN.md` | Checkboxes ao mergear |
| Resposta no DEV.to (opc.) | Apontar invariante / D-M7 quando implementar |

---

## 7. Critérios de aceite (épico)

### Memória de longo prazo
- [ ] Interface `MemoryStore` + in-memory; testes de `Memory bool` passam.
- [ ] Inject com caps; sem dump ilimitado quando policy usa limites.
- [ ] **D-M7:** wave paralela comita Memory em ordem de declaração; inject da próxima wave não vê writes in-flight de siblings.
- [ ] FileStore sobrevive restart nos testes.
- [ ] Path de embedding com fake embedder.
- [ ] Docs bilíngues + example; invariante Memory/`WithContext` documentada (não só na resposta do blog).

### Async além do staged
- [ ] `Task.Async` independentes com overlap temporal no Sequential.
- [ ] `Task.Context` enforced; ciclos / arestas same-wave com erro claro.
- [ ] Staged golden inalterado; agregação continua por declaração após barreira.
- [ ] FailFast + panic recovery.
- [ ] `-race` limpo com memory saves **e** teste de ordem semântica de Memory verde.
- [ ] Docs bilíngues + example (contrato wave/barreira/fold explícito).

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
| Race semântica via completion order da Memory | **D-M7=B** buffer+fold; inject só commitado; testes como ordem determinística do Staged |
| Users usando Memory como bus de merge de siblings | Invariante nas docs + preferir `WithContext`; policy de inject conservadora (D-M3) |

---

## 9. Decision log (preencher antes de codar)

Recomendações abaixo já incorporam o plano + feedback de race semântica no DEV.to. Células ficam `_TBD_` até você fechar explicitamente (mesmo processo dos residuals D1–D7).

| ID | Decisão | Data | Notas |
|---|---|---|---|
| D-M1 | _TBD_ | | Rec **A** — InMemory default |
| D-M2 | _TBD_ | | Rec **A** se sem ciclo senão **B** — layout |
| D-M3 | _TBD_ | | Rec **C** default=A — inject vs Context; sem inject não commitado |
| D-M4 | _TBD_ | | Rec **A** — latest N no snapshot **commitado**, ordem estável |
| D-M5 | _TBD_ | | Rec **B** — skip JSONL corrupto + contador |
| D-M6 | _TBD_ | | Rec **A** — app dona do Close |
| D-M7 | _TBD_ | | Rec **B** — buffer + fold na barreira (thread DEV) |
| D-A1 | _TBD_ | | Rec **A** — wave + fold por declaração |
| D-A2 | _TBD_ | | Rec **A** — pré-resolve hierarchical serial |
| D-A3 | _TBD_ | | Rec **A** — skip só dependentes |
| D-A4 | _TBD_ | | Rec **A** — MaxWorkers 0 + aviso docs |
| D-A5 | _TBD_ | | Rec **A** — Staged ignora Task.Async |
| D-A6 | _TBD_ | | Rec **A** — sem novo Process; alias DAG opcional depois |

---

## 10. Status tracking

| Workstream | Fase | PRs | Status |
|---|---|---|---|
| Memory interfaces + policy + **barreira D-M7** | P2 | M1–M2 | Planejado |
| FileStore | P2 | M3 | Planejado |
| Embeddings hook | P2 | M4 | Planejado |
| runTaskGroup | P1 | A1 | Planejado |
| DAG + Async | P1 | A2–A4 | Planejado |
| Tools / sugar opcional | P3 | M5/A5 | Adiado |

