# Plano — P2: Callbacks / Telemetria e remainder de JSON Schema

> **Status:** **Design** (não iniciado). Próximo epic de produto após v0.7.0 (Streaming).  
> **Decisões:** D-C1–D-C10 (callbacks) e D-J1–D-J12 (JSON Schema) propostas abaixo (§9); fechar antes do código da Fase 1.  
> **Relacionado:** `progress.go`, `stream.go`, `executor.go` / `toolcall.go` / `loop.go` / `crew.go`, `schema.go` / `structured.go`, roadmap `PLAN.pt-BR.md` §6 P2.  
> **Restrições:** zero dependências externas de módulo no core (`go.mod` só-stdlib). Gates do §6.1 de `PLAN.security-residuals.md` em todo PR (cobertura ≥ 90%, race-clean, docs EN+PT-BR, CHANGELOG).  
> **Fora de escopo de epics irmãos:** Flows (P3), YAML (P3), tools HTTP/files/RAG (P3), training (P3), M5/A5, SDK OpenTelemetry no core.

---

## 0. Por que estes dois juntos

| Capacidade | Hoje (v0.7.0) | Lacuna |
|---|---|---|
| **Observabilidade de lifecycle** | `WithProgress` só metadados stage/task/tool; `slog` livre; `WithStream` = deltas de texto | Sem hooks first-class para iteração ReAct, limites de call LLM, rounds de repair structured, ou registros exportáveis |
| **JSON Schema** | Subconjunto stdlib (type/properties/required/enum/items/additionalProperties/bounds/pattern/oneOf\|anyOf\|allOf); `WithStrictSchema` rejeita `$ref`, `format`, `if`/`then`/`else`, `const`, `unevaluated*`, … | Schemas reais (OpenAPI, params MCP, docs públicos) pedem `$ref` local + alguns formats; repair sofre quando keywords são ignoradas em silêncio |

São **workstreams independentes** num epic P2 para fechar o checkbox do roadmap, com **trens de PR separados** que não se bloqueiam.

**Não-objetivos (este plano):**
- Embutir OpenTelemetry, Prometheus ou outro SDK de telemetria no core.
- Colocar corpos de prompt / outputs LLM completos em eventos default (corpos só opt-in e redigidos — D-C4).
- JSON Schema draft 2020-12 completo / `$ref` remoto na rede.
- Substituir APIs `Progress` ou `StreamFunc` (só aditivo).
- Codegen a partir de schema ou cliente OpenAPI.

---

## 1. Baseline (verdade do código em v0.7.0)

### 1.1 Progress hoje

```go
type ProgressFunc func(Progress)
type Progress struct {
    Stage, Task, Agent, Event, Tool string
    Duration time.Duration
    Err error // redigido em falha de task_completed
}
// Eventos: stage_started|completed, task_started|completed, tool_invoked
```

- Anexo via `Crew.WithProgress` → `ContextWithProgress` no Kickoff.
- `emitProgress` recupera panics; callback concurrency-safe obrigatório.
- **Nunca** carrega corpos de prompt/LLM/tool (contrato SECURITY).

### 1.2 Stream hoje (adjacente, não é este epic)

`WithStream` / `StreamChunk` entregam **texto cru do modelo** em caminhos sem tools. Telemetria **não** deve reenviar deltas de stream via Progress (D-S5 já travado).

### 1.3 Validador de schema hoje

| Suportado | Explicitamente não suportado (StrictSchema falha) |
|---|---|
| `type`, `properties`, `required`, `enum`, `items` | `$ref` |
| `additionalProperties` (bool ou schema) | `if` / `then` / `else` |
| bounds string/number/array, `pattern` (teto 512) | `format`, `const` |
| `oneOf` / `anyOf` / `allOf` | `unevaluatedProperties` / `unevaluatedItems` |
| Meta ignorado: `$schema`, `$id`, `title`, `description`, `default`, `examples`, `definitions`, `$defs` | `not`, `dependent*`, `prefixItems`, `contains`, `propertyNames`, `min/maxProperties`, `uniqueItems`, … |

`definitions` / `$defs` são **walked** mas **`$ref` não resolve** — `$defs` só ganha vida com D-J1.

### 1.4 Onde os hooks disparariam

| Site | Arquivo | Eventos candidatos |
|---|---|---|
| Início/fim de task | `crew.go` execute | já Progress `task_*` |
| Stage/wave | `crew.go` | Progress `stage_*`; opcional `wave_*` |
| Iteração ReAct | `executor.go` | **falta** `react_iteration` / `llm_call` |
| Round native tool | `toolcall.go` | Progress `tool_invoked`; falta índice de round |
| Repair structured | `structured.go` | **falta** `structured_repair` |
| AgenticLoop plan/eval/refine | `loop.go` | **falta** `loop_phase` |
| Bloqueio de guardrail | `guardrail.go` | **falta** `guardrail_blocked` |

---

## 2. Objetivos

### Parte C — Callbacks / telemetria

1. **Eventos de lifecycle mais ricos** sem quebrar `Progress` / `WithProgress`.
2. **Registros estruturados exportáveis** (JSON-serializable) para logs/métricas da app.
3. **Metadata-safe por default**; tiers de detalhe opcionais sem vazar segredos.
4. **Mesmos contratos de concorrência / panic** de Progress e Stream.
5. **Zero dependência OTEL** no core; documentar bridge events → OTEL no example.

### Parte J — Remainder de JSON Schema

1. **Resolução local de `$ref`** no documento raiz (`#/...`, `#/$defs/...`, `#/definitions/...`).
2. **Subconjunto prático de `format`** com semântica clara.
3. **`const`** e fatia mínima útil de keywords restantes priorizadas por schemas reais.
4. **`if`/`then`/`else`** quando tratável sem modelo completo de anotações.
5. **`unevaluated*`** só se couber com correção suficiente — senão Strict-fail + follow-up.
6. Manter **StrictSchema fail-closed** para o que ainda não for suportado.
7. Sem `$ref` de rede; sem novas deps.

---

## 3. Parte C — Esboço da API pública

### 3.1 Modelo de evento (aditivo)

```go
// CrewEvent é um registro de lifecycle exportável. Seguro para encoding JSON.
// Campos default são só metadados (D-C4).
type CrewEvent struct {
    Type      string         `json:"type"`
    Time      time.Time      `json:"time"`
    KickoffID string         `json:"kickoff_id,omitempty"`
    Stage     string         `json:"stage,omitempty"`
    Task      string         `json:"task,omitempty"`
    Agent     string         `json:"agent,omitempty"`
    Iteration int            `json:"iteration,omitempty"`
    Phase     string         `json:"phase,omitempty"`
    Tool      string         `json:"tool,omitempty"`
    DurationMs int64         `json:"duration_ms,omitempty"`
    Err       string         `json:"err,omitempty"`
    Attrs     map[string]any `json:"attrs,omitempty"`
}

const (
    EventKickoffStarted   = "kickoff_started"
    EventKickoffCompleted = "kickoff_completed"
    EventStageStarted     = "stage_started"
    EventStageCompleted   = "stage_completed"
    EventWaveStarted      = "wave_started"
    EventWaveCompleted    = "wave_completed"
    EventTaskStarted      = "task_started"
    EventTaskCompleted    = "task_completed"
    EventToolInvoked      = "tool_invoked"
    EventLLMCallStarted   = "llm_call_started"
    EventLLMCallCompleted = "llm_call_completed"
    EventReactIteration   = "react_iteration"
    EventStructuredRepair = "structured_repair"
    EventLoopPhase        = "loop_phase"
    EventGuardrailBlocked = "guardrail_blocked"
)

type EventFunc func(CrewEvent)

func (c *Crew) WithEvents(fn EventFunc) *Crew
func ContextWithEvents(ctx context.Context, fn EventFunc) context.Context
```

### 3.2 Ponte Progress → Events (D-C2)

**Recomendação:** continuar emitindo `Progress` inalterado.  
Com `EventFunc` setado, emitir o `CrewEvent` correspondente.  
Helper opcional:

```go
func ProgressAsEvents(fn EventFunc) ProgressFunc
```

Não remover nem deprecar Progress em v0.x.

### 3.3 O que NÃO entra nos eventos default

| Excluído por default | Por quê |
|---|---|
| Corpos system/user/assistant | Segredo / PII |
| Args/observações completas de tool | Idem; só nome + duration (como Progress) |
| Deltas de stream | Pertencem a `WithStream` |
| Payloads HTTP crus do provider | Cap/redação já em outro lugar |

**Detalhe opt-in (D-C4):** v1 só **metadata**; tiers `summary`/`full` adiados ou `summary` se barato; `full` só com nota SECURITY alta.

### 3.4 KickoffID (D-C6)

ID aleatório por Kickoff (16 bytes hex) no ctx. Todos os eventos da run compartilham. App não precisa setar; override via context para testes ok.

### 3.5 Bridge slog (helper opcional)

```go
func EventLogger(logger *slog.Logger) EventFunc
```

---

## 4. Parte C — Matriz de integração

| Site | Emitir com EventFunc |
|---|---|
| Entrada/saída do Kickoff | `kickoff_started` / `kickoff_completed` |
| Sites atuais de Progress | dual-emit dos tipos CrewEvent |
| Antes/depois de cada `Call` / `callLLMText` | `llm_call_started` / `llm_call_completed` (**sem** messages) |
| Cada iteração ReAct | `react_iteration` |
| Cada tentativa de repair structured | `structured_repair` |
| Fases do AgenticLoop | `loop_phase` |
| Falha de guardrail | `guardrail_blocked` |
| Waves async | `wave_started` / `wave_completed` (D-C8 **on**) |

**Fan-out:** um helper `emitEvent` (recover de panic + redaction de Err).

---

## 5. Parte J — Design JSON Schema

### 5.1 `$ref` (D-J1) — só local

```go
const MaxSchemaRefDepth = 32
const MaxSchemaRefExpansions = 256
// Formas: "#/$defs/Foo", "#/definitions/Foo", ponteiros "#/..."
// Rejeitar: URLs, arquivos externos, ciclos, profundidade
```

**D-J2-B:** `$ref` como base + **keywords irmãs aplicadas** como interseção (estilo OpenAPI prático).  
Detecção de ciclo; alvo ausente = fail closed.

### 5.2 `format` (D-J3) — allowlist

| format | Regra (stdlib) |
|---|---|
| `date-time` | RFC3339 / RFC3339Nano |
| `date` | `2006-01-02` |
| `email` | regexp pragmática + teto de tamanho |
| `uri` / `uri-reference` | `url.Parse` (+ scheme em `uri`) |
| `uuid` | 8-4-4-4-12 hex |
| `ipv4` / `ipv6` | `net.ParseIP` |

**format desconhecido em runtime:** ignorar (D-J4-A).  
Remover `format` de `unsupportedStrictKeywords`; entrar em `knownSchemaKeywords`.

### 5.3 Demais keywords v1

| Keyword | Decisão |
|---|---|
| `const` | **Ship** (DeepEqual) |
| `if`/`then`/`else` | **Ship** forma básica |
| `not` | **Ship** |
| `minProperties` / `maxProperties` / `uniqueItems` | **Ship** |
| `unevaluated*` | **Adiar** (D-J9-B) — continua Strict-fail |
| boolean schema `true`/`false` | **B** draft-accurate se baixo risco |
| comprimento de string | manter **bytes** (`len`) — D-J11 |

### 5.4 Ainda não suportado

`dependentRequired`, `dependentSchemas`, `prefixItems`, `contains`, `propertyNames`, content*, `$ref` remoto.

### 5.5 Erros

```go
ErrSchemaRefCycle / ErrSchemaRefInvalid / ErrSchemaRefNotFound / ErrSchemaRefDepth
```

Falhas de instância continuam `ValidationError(s)`.

---

## 6. Arquivos esperados

**Callbacks:** `event.go`, wiring em `crew.go` / `executor.go` / `toolcall.go` / `structured.go` / `loop.go` / `guardrail.go`, `event_test.go`, `examples/events`, docs crews EN+PT, SECURITY.

**Schema:** `schema.go` (+ testes ref/format), `errors.go`, docs tasks EN+PT, CHANGELOG.

Sem pacotes novos.

---

## 7. Segurança

| Tópico | Regra |
|---|---|
| Eventos default | Só metadados (como Progress) |
| Campo Err | Sempre redigido |
| Panic | recover em `emitEvent` |
| Concorrência | EventFunc concurrency-safe |
| `$ref` | Só fragmento local — **sem fetch** (SSRF) |
| `format: uri` | Só valida; não busca |
| Bombas de expansão | Cap de profundidade + expansões |
| Regex | `MaxSchemaPatternLen`; email linear |

---

## 8. Testes e gates

### 8.1 Callbacks

Dual-emit Progress+Events; par llm_call no mock; contagem react_iteration; structured_repair; loop_phase; guardrail_blocked; panic não derruba Kickoff; wave Async com Task+KickoffID; EventLogger smoke.

### 8.2 Schema

`$ref` defs/definitions; ref externa inválida; ciclo; profundidade; tabela format; const/not/if-then-else; min/maxProperties/uniqueItems; golden não-regressão; StrictSchema atualizado.

### 8.3 Gates

≥90% cobertura, `-race`, docs EN+PT, CHANGELOG, zero deps novas, gofmt/vet.

### 8.4 Aceite

**Callbacks:** API pública, novos tipos de evento, Progress intacto, example, docs+SECURITY.  
**Schema:** `$ref` local, format allowlist, const/not/if/props/uniqueItems, Strict set, golden, docs.  
**Release:** CHANGELOG; alvo **v0.8.0**.

---

## 9. Decisões

### 9.1 Callbacks (D-C*)

| ID | Pergunta | Decisão |
|---|---|---|
| **D-C1** | API nova vs sobrecarregar Progress | **B** CrewEvent + EventFunc |
| **D-C2** | Destino do Progress | **A** dual-emit em v0.x |
| **D-C3** | Forma do hook ReAct | **A** `react_iteration` + par llm_call |
| **D-C4** | Corpos nos eventos | **A** nunca na v1 |
| **D-C5** | OTEL no core | **B** só bridge no example |
| **D-C6** | ID de correlação | **B** auto KickoffID |
| **D-C7** | Vários EventFuncs | **A** um slot (app compõe) |
| **D-C8** | Eventos wave_* | **B** emitir |
| **D-C9** | llm_call no path stream | **A** um par em volta de CallOrStream |
| **D-C10** | Panic no EventFunc | **B** recover |

### 9.2 JSON Schema (D-J*)

| ID | Pergunta | Decisão |
|---|---|---|
| **D-J1** | Escopo `$ref` | **A** só local |
| **D-J2** | `$ref` + siblings | **B** interseção |
| **D-J3** | Set de `format` | **B** allowlist |
| **D-J4** | format desconhecido | **A** ignorar em runtime |
| **D-J5** | `const` | **B** ship |
| **D-J6** | `if`/`then`/`else` | **B** ship básico |
| **D-J7** | `not` | **B** ship |
| **D-J8** | min/maxProperties, uniqueItems | **B** ship |
| **D-J9** | unevaluated* | **B** adiar |
| **D-J10** | boolean schemas | **B** draft-accurate se barato |
| **D-J11** | unidade de length | **manter bytes** |
| **D-J12** | caps ref | 32 / 256 |

### 9.3 Abertas até spikes

| ID | Tópico |
|---|---|
| **O-C1** | Incluir `LLM.Model()` nos Attrs de llm_call (lean **sim**) |
| **O-J1** | Escapes de JSON Pointer / ref `#` raiz |
| **O-J2** | Incluir ou adiar `format: time` |

---

## 10. Ordem de PRs / implementação

```
Fase 0    Fechar D-C* / D-J* neste documento
── Trem Callbacks ──
Fase C1   event.go API + emitEvent + KickoffID + testes
Fase C2   dual-emit dos sites Progress + kickoff/wave
Fase C3   executor llm_call + react_iteration
Fase C4   structured_repair + loop_phase + guardrail_blocked
Fase C5   examples/events + docs EN/PT + SECURITY
── Trem Schema (paralelo após Fase 0) ──
Fase J1   resolvedor $ref + caps + testes
Fase J2   const, not, min/maxProperties, uniqueItems, boolean schemas
Fase J3   allowlist format
Fase J4   if/then/else
Fase J5   set StrictSchema + docs + golden
── Release ──
Fase R    CHANGELOG + checkboxes PLAN + tag v0.8.0
```

**Paralelismo:** C1–C5 ∥ J1–J5 após Fase 0 (`event.go` vs `schema.go`).

---

## 11. Plano de documentação

| Doc | Atualização |
|---|---|
| `docs/crews.md` + PT | WithEvents, tabela de tipos, concorrência, vs Progress vs Stream |
| `docs/tasks.md` + PT | Tabela de keywords schema (suportado vs strict-fail) |
| `doc.go` | Bullets Events + schema |
| `README` EN+PT | Why + What's new na release |
| `SECURITY.md` | Events só metadados; `$ref` só local |
| `examples/events` | Dump JSONL offline |
| `PLAN.md` / PT | Link deste plano; checkbox ao shippar |
| `CHANGELOG` | EN+PT |

---

## 12. Versionamento

- APIs aditivas → **v0.8.0** minor em v0.x.
- Sem breaking de `Progress`, `StreamFunc`, ou resultados de validate de schemas já suportados.
- StrictSchema fica **menos** rejeitador (mais keywords) — compatível para quem falhava no construct.

---

## 13. Fora de escopo / depois

| Item | Onde |
|---|---|
| SDK OpenTelemetry | Só example bridge |
| Tiers de detalhe com corpos | Follow-up + security review |
| unevaluated* completo | Plano J-extra |
| `$ref` remoto | Nunca no core sem epic de allowlist |
| Histogramas de métricas no core | App a partir dos events |

---

## 14. Resumo

Shippar **dois trens P2 paralelos**: (1) **`CrewEvent` + `WithEvents`** ampliando observabilidade de lifecycle além do Progress, metadata-safe e amigável a slog/OTEL-bridge; (2) **remainder de JSON Schema** centrado em **`$ref` local**, **allowlist de format**, **const/not/if-then-else** e keywords pequenas de object/array, mantendo **unevaluated*** e refs remotos de fora. Zero deps novas; alvo **v0.8.0**.

Quando D-C1–D-C10 e D-J1–D-J12 forem reconhecidas, C1 / J1 podem começar em branches como `feat/p2-events-c1` e `feat/p2-schema-j1`.

---

> **Idioma:** espelho fiel de [`PLAN.p2-callbacks-schema.md`](PLAN.p2-callbacks-schema.md) (inglês é a fonte autoritativa).
