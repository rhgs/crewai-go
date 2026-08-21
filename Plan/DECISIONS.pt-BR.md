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

**Famílias de ID:** D1–D7 (security v0.5), D-M\* (memory), D-A\* (async), G\* (gaps memory-async), D-S\* (streaming), D-C\* (events), D-J\* (JSON Schema), P-\* (produto/roadmap).

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

Nada em D1–D7 / D-M\* / D-A\* / G\* / D-S\* / D-C\* / D-J\* está aberto para relitigar sem **novo ID**.

| ID | Tópico | Status |
|----|--------|--------|
| **P-XAI-OAUTH** | client_id/endpoints oficiais xAI | open |
| **P-M5** / **P-A5** | memory tools / Process=DAG | deferred |
| **D-S10** follow-up | stream parcial de tool-call | deferred |
| **D-J9** follow-up | unevaluated* | deferred |
| **O-J2** | format: time | open spike |

---

## 10. Histórico deste documento

| Data | Mudança |
|------|---------|
| 2026-08-21 | Log vivo inicial (espelho de DECISIONS.md) |

## 11. Relacionados

| Doc | Papel |
|------|-------|
| [`PLAN.md`](PLAN.md) / [`PLAN.pt-BR.md`](PLAN.pt-BR.md) | Roadmap |
| [`docs/concurrency.md`](../docs/concurrency.md) | Contrato runtime (D-M7, D-A1, …) |
| `PLAN.*.md` | Rationale completo |
