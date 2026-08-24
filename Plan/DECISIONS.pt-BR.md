# Registro de decisões (crewai-go)

> **Idiomas:** [English](DECISIONS.md) · **Português** (atual)  
> **Status:** **Documento vivo** — atualizar sempre que uma decisão de produto/design for tomada ou alterada.  
> **Autoridade:** Planos de epic (`PLAN.*.md`) guardam o raciocínio longo; **este arquivo é o índice de IDs fechados e abertos**.  
> **Como adicionar:** anexar na família certa com ID, data, pergunta, opções, escolha, status e ponteiro para o plano/PR. Não renumerar IDs fechados.

O inglês [`DECISIONS.md`](DECISIONS.md) é a fonte autoritativa se houver divergência pontual de redação.

---

## 0. Convenções

| Campo | Significado |
|-------|-------------|
| **ID** | Código estável (`D-M7`, `D-S1`, …). Nunca reutilizar para outra pergunta. |
| **Status** | `closed` · `open` · `deferred` · `superseded` |
| **Escolha** | Letra ou rótulo da opção (ex.: **B**). |
| **Opções** | Alternativas consideradas. |
| **Entregue** | Release/PR que implementou (se houver). |

**Famílias de ID:** D1–D7 (security v0.5), D-M\* (memory), D-A\* (async), G\* (gaps memory-async), D-S\* (streaming), D-C\* (events), D-J\* (JSON Schema), D-JT\* (spikes de format), D-MT\* (tools de memória / M5), D-ST\* (stream parcial de tool-call), D-JE\* (unevaluated\*), D-XA\* (OAuth xAI), D-F/T/Y/X\* (trens P3), P-\* (produto/roadmap).

---

## 1. Security residuals (D1–D7) — fechadas 2026-08-20 · v0.5.0

Fonte: [`PLAN.security-residuals.md`](PLAN.security-residuals.md).

| ID | Pergunta | Opções | Escolha | Status | Entregue |
|----|----------|--------|---------|--------|----------|
| **D1** | Timeout HTTP default do MCP | (A) 30s (B) outro (C) sem default | **A** — 30s; override prog+JSON; `0` desliga | closed | v0.5.0 |
| **D2** | Jail OutputFile + symlinks | (A) EvalSymlinks no jail (B) só Clean | **A** — fail closed em erro de eval | closed | v0.5.0 |
| **D3** | Onde fica RedactHandler | (A) root crewai (B) subpacote | **A** | closed | v0.5.0 |
| **D4** | Kickoff concorrente no mesmo `*Crew` | (A) TryLock+erro (B) fila | **A** — `ErrCrewRunning` | closed | v0.5.0 |
| **D5** | Anexo da tool de delegação | (A) sempre (B) só manual (C) flag opt-in | **C** — `EnableDelegationTool` default false | closed | v0.5.0 |
| **D6** | Gather structured esgotado | (A) falha dura (B) capture silencioso (C) capture+warning | **C** | closed | v0.5.0 |
| **D7** | Unidade minLength/maxLength | (A) bytes (B) runes | **A** | closed | v0.5.0 |

---

## 2. Memory (D-M1–D-M7) — fechadas 2026-08-21 · v0.6.0

Fonte: [`PLAN.memory-async.md`](PLAN.memory-async.md). PR #31.

| ID | Pergunta | Opções | Escolha | Status | Entregue |
|----|----------|--------|---------|--------|----------|
| **D-M1** | Store default com `Memory=true` e Store nil | (A) InMemory (B) exigir Store | **A** | closed | v0.6.0 |
| **D-M2** | Onde vive FileStore | (A) root (B) subpacote | **A** | closed | v0.6.0 |
| **D-M3** | Inject com Context já setado | (A) nunca (B) sempre (C) flag de policy | **C** — `InjectWhenEmptyContext` default true | closed | v0.6.0 |
| **D-M4** | Texto da query de inject automático | (A) vazio → latest N commitado (B) keywords (C) embed | **A** | closed | v0.6.0 |
| **D-M5** | Linha JSONL corrupta | (A) falha Open (B) skip+warn | **B** | closed | v0.6.0 |
| **D-M6** | Quem faz Close do store externo | (A) app (B) sempre Crew | **A** | closed | v0.6.0 |
| **D-M7** | Quando AutoSave fica visível ao inject do orquestrador? | (A) event-log ao vivo / ordem de conclusão (B) buffer por task + commit na barreira em **ordem de declaração** (C) híbrido Put imediato / leitura commitada | **B** | closed | v0.6.0 |

---

## 3. Async além do Staged (D-A1–D-A6) — fechadas 2026-08-21 · v0.6.0

| ID | Pergunta | Opções | Escolha | Status | Entregue |
|----|----------|--------|---------|--------|----------|
| **D-A1** | Schedule misto Async/sync | (A) waves + fold por declaração (B) serial global exceto subgrafo Async | **A** | closed | v0.6.0 |
| **D-A2** | Resolve de agent no Hierarchical | (A) pré-resolve serial (B) lazy paralelo | **A** | closed | v0.6.0 |
| **D-A3** | FailFast=false após falha | (A) só skip dependents (B) aborta crew | **A** | closed | v0.6.0 |
| **D-A4** | Default AsyncMaxWorkers | (A) 0 ilimitado (B) GOMAXPROCS (C) 8 | **C** default 8; **0 = ilimitado** | closed | v0.6.0 |
| **D-A5** | Task.Async sob Staged | (A) ignora (B) erro | **A** | closed | v0.6.0 |
| **D-A6** | Novo Process vs flags | (A) só Sequential/Hierarchical+Async (B) Process=Async | **A** | closed | v0.6.0 |

---

## 4. Gaps memory-async (G1–G12) — fechadas 2026-08-21 · v0.6.0

| ID | Pergunta | Opções | Escolha | Status | Entregue |
|----|----------|--------|---------|--------|----------|
| **G1** | Chave de ordem de fold/commit | (A) índice em Crew.Tasks (B) Task.ID obrigatório | **A** | closed | v0.6.0 |
| **G2** | AutoSave em falha | (A) só sucesso (B) também falhas | **A** | closed | v0.6.0 |
| **G3** | Scope default da policy | (A) vazio (B) Crew.Name se setado | **B** | closed | v0.6.0 |
| **G4** | Destino de Memory bool | (A) alias permanente v0.x (B) deprecar | **A** | closed | v0.6.0 |
| **G5** | Log Async ignorado no Staged | (A) silêncio (B) Warn único | **B** | closed | v0.6.0 |
| **G6** | CreatedAt vs ordem de commit | (A) CreatedAt=fim; ordem=declaração (B) ordem=wall-clock | **A** | closed | v0.6.0 |
| **G7** | FileStore multi-processo | (A) single-writer v1 (B) flock | **A** | closed | v0.6.0 |
| **G8** | Quando roda AutoEmbed | (A) serial na barreira (B) paralelo nos workers | **A** | closed | v0.6.0 |
| **G9** | Barreira D-M7 no Staged | (A) sim (B) Save ao vivo no Staged | **A** | closed | v0.6.0 |
| **G10** | Empacotamento de release | (A) um minor amplo (B) FileStore atrás | **A** (+M3/M4 em v0.6.0) | closed | v0.6.0 |
| **G11** | Entrada de memória grande demais | (A) reject Put; AutoSave warn (B) truncate (C) abort Kickoff | **A** | closed | v0.6.0 |
| **G12** | Validação do DAG Context | (A) no Kickoff vs waves planejadas (B) mid-wave | **A** | closed | v0.6.0 |

---

## 5. Streaming (D-S1–D-S14) — fechadas 2026-08-21 · v0.7.0

Fonte: [`PLAN.streaming.md`](PLAN.streaming.md). PRs #35–#36.

| ID | Pergunta | Opções | Escolha | Status | Entregue |
|----|----------|--------|---------|--------|----------|
| **D-S1** | Forma da API | (A) CallStream em LLM (B) StreamingLLM opcional | **B** | closed | v0.7.0 |
| **D-S2** | Chunk terminal | (A) delta final pode ir com Done (B) Done vazio depois | **A** | closed | v0.7.0 |
| **D-S3** | Anexo do sink | (A) ctx+WithStream (B) campo Agent (C) no LLM | **A** | closed | v0.7.0 |
| **D-S4** | Quais paths streamam na v1 | (A) todos (B) texto final / sem tools (C) exceto structured | **B** | closed | v0.7.0 |
| **D-S5** | Eventos Progress de stream | (A) nenhum na v1 (B) started/completed | **A** | closed | v0.7.0 |
| **D-S6** | Ordem de PRs de provider | (A) todos juntos (B) mock+openai primeiro | **B** | closed | v0.7.0 |
| **D-S7** | LLM sem stream + sink set | (A) sem deltas (B) um Delta+Done | **B** | closed | v0.7.0 |
| **D-S8** | Teto de tamanho | (A) teto maior (B) só texto (C) texto e body | **C** | closed | v0.7.0 |
| **D-S9** | sink nil | (A) ainda CallStream (B) sempre Call | **B** | closed | v0.7.0 |
| **D-S10** | Stream parcial de tool-call | (A) v1 (B) adiar | **B** | closed | — |
| **D-S11** | Buffer do canal | (A) 0 (B) 16 (C) 64 | **B** | closed | v0.7.0 |
| **D-S12** | Panic no StreamFunc | (A) derruba Kickoff (B) recover | **B** | closed | v0.7.0 |
| **D-S13** | Demux multi-task | (A) só deltas (B) Task/Agent no chunk (C) sinks por task | **B** | closed | v0.7.0 |
| **D-S14** | Contrato de erro CallStream | (A) (chan, error) (B) chan never-nil; setup como Err; close nu → incomplete | **B** | closed | v0.7.0 |

---

## 6. Eventos de lifecycle (D-C1–D-C10) — fechadas 2026-08-21 · v0.8.0

Fonte: [`PLAN.p2-callbacks-schema.md`](PLAN.p2-callbacks-schema.md). PR #39.

| ID | Pergunta | Opções | Escolha | Status | Entregue |
|----|----------|--------|---------|--------|----------|
| **D-C1** | API nova vs Progress | (A) só expandir Progress (B) CrewEvent+EventFunc | **B** | closed | v0.8.0 |
| **D-C2** | Destino do Progress | (A) dual-emit (B) soft-deprecar | **A** | closed | v0.8.0 |
| **D-C3** | Hook ReAct | (A) react_iteration + par llm_call (B) só start/end | **A** | closed | v0.8.0 |
| **D-C4** | Corpos nos events | (A) nunca na v1 (B) tiers | **A** | closed | v0.8.0 |
| **D-C5** | OTEL no core | (A) dependência (B) bridge no example | **B** | closed | v0.8.0 |
| **D-C6** | ID de correlação | (A) nenhum (B) auto no ctx | **B** | closed | v0.8.0 |
| **D-C7** | Vários EventFuncs | (A) um slot (B) slice | **A** | closed | v0.8.0 |
| **D-C8** | Eventos wave_* | (A) pular (B) emitir | **B** | closed | v0.8.0 |
| **D-C9** | llm_call no path stream | (A) um par (B) por delta | **A** | closed | v0.8.0 |
| **D-C10** | Panic no EventFunc | (A) crash (B) recover | **B** | closed | v0.8.0 |

---

## 7. Remainder JSON Schema (D-J1–D-J12) — fechadas 2026-08-21 · v0.8.0

| ID | Pergunta | Opções | Escolha | Status | Entregue |
|----|----------|--------|---------|--------|----------|
| **D-J1** | Escopo $ref | (A) só local (B) arquivo (C) http | **A** | closed | v0.8.0 |
| **D-J2** | $ref + siblings | (A) ignora (B) interseção | **B** | closed | v0.8.0 |
| **D-J3** | Set de format | (A) nenhum (B) allowlist (C) grande | **B** | closed | v0.8.0 |
| **D-J4** | format desconhecido | (A) ignora (B) erro | **A** | closed | v0.8.0 |
| **D-J5** | const | (A) adiar (B) ship | **B** | closed | v0.8.0 |
| **D-J6** | if/then/else | (A) adiar (B) ship básico | **B** | closed | v0.8.0 |
| **D-J7** | not | (A) adiar (B) ship | **B** | closed | v0.8.0 |
| **D-J8** | min/maxProperties, uniqueItems | (A) adiar (B) ship | **B** | closed | v0.8.0 |
| **D-J9** | unevaluated* | (A) ship (B) adiar | **B** | closed | — |
| **D-J10** | schemas booleanos | (A) frouxo (B) draft-accurate | **B** | closed | v0.8.0 |
| **D-J11** | unidade de length | bytes vs runes | **bytes** | closed | v0.8.0 |
| **D-J12** | caps de ref | 32 / 256 | **sim** | closed | v0.8.0 |

---

## 7A. Follow-ups abertos/fechados 2026-08-21 (ack de produto)

| ID | Pergunta | Opções | Escolha | Status | Entregue | Notas |
|----|----------|--------|---------|--------|----------|-------|
| **D-JT1** | `format: time` (spike O-J2) | (A) ship RFC 3339 full-time (B) documentar adiamento | **A** — `HH:MM:SS[.fff][Z\|±offset]` via `time.Parse("15:04:05.999999999Z07:00")` e fallback sem offset | closed | próximo patch ≥ v0.8.x | Offset opcional; componentes de data rejeitados |

### 7A.1 M5 tools de memória — design fechado (aguardando demanda)

Fechado pelo ack de produto (A3) — código só quando pedido:

| ID | Pergunta | Opções | Escolha | Status |
|----|----------|--------|---------|--------|
| **D-MT1** | Uma tool vs duas | (A) duas: recall_memory + remember (B) uma tool com action | **A** | closed (unscheduled) |
| **D-MT2** | Anexação | (A) flag explícita opt-in (B) auto quando Memory=true | **A** | closed (unscheduled) |
| **D-MT3** | Query de recall | (A) só texto (B) só embedding (C) texto + embedding | **C** | closed (unscheduled) |
| **D-MT4** | visibilidade de remember vs D-M7 | (A) Put imediato, fora do buffer da wave (B) bufferizado como AutoSave | **A** | closed (unscheduled) |
| **D-MT5** | Budget de recall | (A) limites de MemoryPolicy (B) arg da tool (C) herda `MaxToolOutputBytes` | **C** | closed (unscheduled) |

### 7A.2 Follow-up D-S10 — design fechado (aguardando demanda, ack A4)

| ID | Pergunta | Opções | Escolha | Status |
|----|----------|--------|---------|--------|
| **D-ST1** | Shape do chunk parcial de tool-call | (A) estender StreamChunk (B) tipo `ToolCallDelta` separado | **B** | closed (unscheduled) |
| **D-ST2** | JSON parcial | (A) provider monta, emite ao completar (B) passa parciais | **A** | closed (unscheduled) |
| **D-ST3** | Turno final de texto com tools faz stream | (A) sim (B) bufferizado | **A** | closed (unscheduled) |
| **D-ST4** | Escopo de provider | (A) todos de uma vez (B) OpenAI primeiro | **B** | closed (unscheduled) |

### 7A.3 Follow-up D-J9 `unevaluated*` — design fechado (aguardando demanda, ack A5)

| ID | Pergunta | Opções | Escolha | Status |
|----|----------|--------|---------|--------|
| **D-JE1** | Escopo do modelo de anotação | (A) vocabulário 2020-12 completo (B) "evaluated set" mínimo por objeto/array | **B** | closed (unscheduled) |
| **D-JE2** | Interação com $ref/allOf | (A) só por nó (B) eval-set faz merge entre aplicadores resolvidos | **B** | closed (unscheduled) |
| **D-JE3** | Booleanos como unevaluated* | (A) permitir bool só em properties (B) só maps de schema no v1 | **B** | closed (unscheduled) |

## 7B. P3 Flows Fase 0 (ack de produto 2026-08-24)

Fonte: [`PLAN.p3-flows-tools-yaml-training.pt-BR.md`](PLAN.p3-flows-tools-yaml-training.pt-BR.md) §9. Fecha só D-F1–D-F10; **D-F11 / D-F12 e todas D-T\*/D-Y\*/D-X\* seguem abertas**. Sem código ainda.

| ID | Pergunta | Opções | Escolha | Status | Entregue | Notas |
|----|----------|--------|---------|--------|----------|-------|
| **D-F1** | Estilo do runner | (A) novo `Process` (B) tipo `Flow[S]` separado | **B** | closed | — | Mesmo padrão de `AgenticLoop`; `Process.valid()` intacto (alinha D-A6 / P-A5) |
| **D-F2** | Registro de step | (A) tags/reflection (B) builder `Listen(name, fn, deps...)` | **B** | closed | — | Explícito, tipado, testável; sem reflection |
| **D-F3** | Estado genérico | (A) `any`/map (B) `Flow[S any]` | **B** | closed | — | Generics go1.24; estado tipado, não um bag de Context |
| **D-F4** | Starts | (A) zero-dep implícito (B) só `Start` explícito (C) ambos | **C** | closed | — | Zero-dep é start; `Start` é marcador opcional; `Start` **com** deps é erro |
| **D-F5** | Lock do estado | (A) lib trava por step (B) sync do usuário | **B** | closed | — | Ownership documentada; a lib não trava `S` (mesmo contrato de StreamFunc/EventFunc) |
| **D-F6** | Resultado do router | (A) nomes exatos registrados (B) prefixo | **A** | closed | — | Nome desconhecido → `ErrFlowUnknownStep` |
| **D-F7** | Events | (A) tipos novos `flow_step_*` (B) reusar `Phase` | **A** | closed | — | Constantes tipadas; só metadados (D-C4) |
| **D-F8** | Onde guardar continue-on-error | (A) mutar estado do usuário (B) `FlowResult.Errors` | **B** | closed | — | Não poluir `S` |
| **D-F9** | `Run` concorrente no mesmo flow | (A) permitir (B) single-flight | **B** | closed | — | `ErrFlowRunning` (igual `ErrCrewRunning`) |
| **D-F10** | Join multi-dep | (A) todos os deps (B) qualquer dep | **A** | closed | — | “Qualquer” é papel do Router |

Ainda abertos nesta família: **D-F11** (cancel / fail-fast default), **D-F12** (ordem de fold do trace paralelo).

## 8. Escolhas de produto permanentes (P-*)

| ID | Pergunta | Opções | Escolha | Status |
|----|----------|--------|---------|--------|
| **P-DEPS** | Módulos externos no core | (A) permitir (B) só stdlib | **B** | closed |
| **P-FC-REACT** | Native FC vs ReAct | (A) só ReAct (B) só native (C) ambos | **C** | closed |
| **P-MODULE** | Module path | forks vs rhgs/crewai-go | **github.com/rhgs/crewai-go** | closed |
| **P-XAI-OAUTH** | Defaults OAuth xAI | (A) inventar (B) configurável até docs oficiais | **B** | open |
| **P-STREAM-SHAPE** | Forma da API de stream | (A) CallStream em LLM (B) StreamingLLM | **B** | closed (D-S1) |
| **P-M5** | Tools recall_memory/remember | (A) ship (B) adiar | **B** | deferred |
| **P-A5** | Alias Process=DAG | (A) ship (B) adiar | **B** | deferred |

---

## 9. Decisões abertas

Itens adiados/abertos com condições de desbloqueio e IDs pré-alocados: [`PLAN.deferred-backlog.pt-BR.md`](PLAN.deferred-backlog.pt-BR.md) ([EN](PLAN.deferred-backlog.md)).

Nada em D1–D7 / D-M\* / D-A\* / G\* / D-S\* / D-C\* / D-J\* / **D-F1–D-F10** está aberto para relitigar sem **novo ID**.

| ID | Tópico | Status | Próximo passo |
|----|--------|--------|---------------|
| **P-XAI-OAUTH** | client_id/endpoints oficiais xAI | open | Atualizar defaults quando a xAI documentar (D-XA*) |
| **P-M5** | Tools de memória | deferred — design fechado (D-MT1–D-MT5) | Implementar só sob pedido do produto |
| **P-A5** | Alias `Process=DAG` | deferred | Não fechar; revisitar só com sinal de confusão (A2) |
| **D-S10** follow-up | Stream parcial de tool-call nativo | deferred — design fechado (D-ST1–D-ST4) | Implementar só sob demanda |
| **D-J9** follow-up | unevaluatedProperties/Items | deferred — design fechado (D-JE1–D-JE3) | Implementar só sob demanda |
| **D-F11** | Cancel / fail-fast default do Flow | open (P3 Fase 0) | Ack de produto; rec: ctx aborta na próxima barreira, fail-fast default true |
| **D-F12** | Ordem de trace de steps paralelos | open (P3 Fase 0) | Ack de produto; rec: fold por ordem de registro |
| **D-T1–D-T10** | Tools HTTP/files/RAG do P3 | open (P3 Fase 0) | Ack das recs do §9 |
| **D-Y1–D-Y10** | YAML subconjunto JSON do P3 | open (P3 Fase 0) | Ack das recs do §9 |
| **D-X1–D-X8** | TraceRecorder do P3 | open (P3 Fase 0) | Ack das recs do §9 |
| ~~**O-J2**~~ | `format: time` | **fechado → D-JT1 (ship)** | Implementado em `schema.go` |

---

## 10. Histórico deste documento

| Data | Mudança |
|------|---------|
| 2026-08-21 | Log vivo inicial (espelho de DECISIONS.md) |
| 2026-08-21 | Marcar D-C\* / D-J\* entregues na **v0.8.0** |
| 2026-08-21 | Ack de produto (A1–A5): O-J2 → **D-JT1** (ship time); M5 → D-MT1–D-MT5 fechados sem agenda; D-S10 → D-ST1–D-ST4; D-J9 → D-JE1–D-JE3; A5 permanece adiado |
| 2026-08-24 | P3 Fase 0 parcial: **D-F1–D-F10** fechadas (B/B/B/C/B/A/A/B/B/A). D-F11/D-F12 e D-T\*/D-Y\*/D-X\* seguem abertas. |

## 11. Relacionados

| Doc | Papel |
|------|-------|
| [`PLAN.md`](PLAN.md) / [`PLAN.pt-BR.md`](PLAN.pt-BR.md) | Roadmap |
| [`docs/concurrency.md`](../docs/concurrency.md) | Contrato runtime (D-M7, D-A1, …) |
| `PLAN.*.md` | Rationale completo |
