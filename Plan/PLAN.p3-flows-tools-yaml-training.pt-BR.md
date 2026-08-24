# Plano — P3: Flows, tools embutidas, YAML declarativo, training/export de traces

> **Status:** **Design** (não iniciado). Último tier do roadmap após v0.8.0.  
> **Decisões:** D-F1–D-F12 (Flows), D-T1–D-T10 (tools), D-Y1–D-Y10 (YAML), D-X1–D-X8 (training) propostas no §9; fechar antes de qualquer código.  
> **Relacionado:** [`DECISIONS.md`](DECISIONS.md), `process.go`, `crew.go`, `tool.go`, `tools/websearch.go` (guards SSRF), `task.go` (jail OutputDir), `memory_embed.go`, `schema.go`, `loop.go`.  
> **Restrições:** zero deps externas no core (stdlib não tem parser YAML); gates: ≥90% cobertura por pacote tocado **e** agregado, race-clean, gofmt+vet, docs EN+PT, CHANGELOG, SECURITY quando aplicável.  
> **Não-objetivos:** backlog adiado (xAI OAuth, M5, A5, D-S10, D-J9, O-J2 — ver `PLAN.deferred-backlog.pt-BR.md`); breaking changes; vector DB no core.

Fonte autoritativa: [`PLAN.p3-flows-tools-yaml-training.md`](PLAN.p3-flows-tools-yaml-training.md) (inglês). Este espelho segue a mesma estrutura.

---

## 0. Por que estes quatro juntos

| Item | Hoje (v0.8.0) | Lacuna que o P3 fecha |
|------|----------------|--------------------------|
| **Flows** | Process = Sequential/Hierarchical/Staged; waves Async via Task.Async | Sem routing event-driven com estado explícito (`@listen`, `@router`) — paridade CrewAI |
| **Tools** | Tool interface + ReAct/native FC; web search; Calculator/CurrentTime/WordCount | Sem HTTP fetch ou file read/write embutidos e seguros; RAG só como padrão de docs |
| **YAML** | Crews montadas programaticamente | Sem carga declarativa (paridade `agents.yaml`/`tasks.yaml`) |
| **Training/export** | ToolTrace, Progress, CrewEvent existem | Sem captura/export curada de runs bem-sucedidas para few-shot |

**Trens independentes** (F/T/Y/X): qualquer subconjunto pode entrar sem os demais.

**Non-goals reafirmados:** sem avaliador YAML no core (v1 = **subconjunto JSON** do YAML); sem vector DB, sem treino de modelo, sem OTEL SDK no core; Flows não substituem Process existentes.

---

## 1. Âncoras do baseline (verdade do código)

- **Dispatch:** `Kickoff` despacha por `Process` com `valid()` de guarda.
- **Hooks telemetria:** kickoff/task/stage/wave/tool já cobertos por Progress + CrewEvent.
- **Tool:** `Call(ctx, string) (string, error)`; native usa JSON Schema (`ToolSpec`).
- **SSRF:** `isBlockedURL` (websearch.go) bloqueia não-http(s), userinfo, IPs privados/loopback/link-local/CGNAT, DNS resolve + fail-closed.
- **Jail:** `resolveOutputPathInJail` (Clean + EvalSymlinks, fail closed).
- **Loop:** `AgenticLoop` é o precedente de estratégia “valor” — Flows segue esse shape, não vira 4º Process na v1.
- **Validator:** P2 (`validateSchema`) reutilizável para validar configs YAML com uma JSON Schema gerada.
- **Traces:** `ToolTrace` em `TasksOutput`; `CrewEvent`; `slog`.

---

## 2. Trem F — Flows

### 2.1 Objetivos

Flow estilo CrewAI: estado + steps + regras `@start`/`@listen`/`@router`, em Go idiomático — métodos `func(ctx, *S) error` registrados por builder, sem mágica de reflection pesada na v1. Mesma doutrina de concorrência: barreira + fold por ordem de declaração.

### 2.2 API (v1)

```go
type Flow[S any] struct{ /* steps, edges, logger, events */ }
type FlowStep[S any] func(ctx context.Context, state *S) error

f := crewai.NewFlow[MyState]().
    Listen("research", stepResearch).            // step sem deps = start
    Listen("news", stepNews).
    Router("choose", stepChoose, "research", "news").
    Listen("write", stepWrite, "choose")

res, err := f.Run(ctx, initial) // res.State, res.Steps (ordem de fold), res.Errors, res.Duration
```

Regras:
- **DAG:** listeners esperam **todas** as deps; ciclo = `ErrFlowCycle` no início de Run.
- **Router:** deve retornar nomes registrados; desconhecido = `ErrFlowUnknownStep`.
- **Concorrência:** listeners independentes rodam em paralelo; **estado é do usuário** (D-F5: lib não bloqueia `*S`; docs ditam ownership).
- **Join:** step multi-dep roda **uma vez** após todos os pais; ordem de traces = fold por ordem de registro (não conclusão).
- **Terminal:** retorna estado final; fail-fast por padrão; opção `WithFlowContinueOnError` acumula em `FlowResult.Errors`.
- Um step pode chamar `crew.Kickoff` — composição; Process intocado (consistente com D-A6).
- Eventos `flow_step_started/completed/error` via EventFunc (D-F7).

### 2.3 Exemplo

`examples/flows_research` — mock offline: bootstrap → research ∥ news → router → write → done.

---

## 3. Trem T — Tools embutidas

### 3.1 `tools.HTTPFetch` (SSRF-safe)

Allowlist de hosts, **deny-by-default** (D-T2), reaproveita o guard SSRF do websearch (extração compartilhada), redirects ≤ 3 revalidados (D-T4), GET por padrão (D-T8), teto de bytes (D-T6), timeout/HTTPClient configuráveis.

### 3.2 `tools.FileRead` / `tools.FileWrite`

Jail por `resolveOutputPathInJail` (helper compartilhado, D-T3); escrita **desligada por padrão** (D-T5); binário rejeitado por padrão (D-T7); fail closed em `../` e symlink fora do jail.

### 3.3 Padrão RAG (docs v1)

`docs/rag.md` (+PT): compor `MemoryStore` + `Embed` + `Query` como pattern; `examples/rag_file` com embedder mock. Sem vector DB nem helper core na v1 (D-T9 = A).

### 3.4 Exemplos

`tools_http` (httptest), `tools_files` (tempdir jail), `rag_file`.

---

## 4. Trem Y — YAML declarativo (subconjunto JSON na v1)

### 4.1 Restrição

Stdlib não tem YAML; adicionar dep quebra zero-deps. **D-Y1 = A:** aceitar **YAML em subconjunto JSON** via `encoding/json`, documentado de forma explícita; YAML completo só como possível submódulo futuro (`yamlprep`, decisão própria — fora deste epic).

### 4.2 API (v1)

```go
cfg, err := crewai.LoadCrewFile("crew.json.yaml")
// ou crewai.LoadCrew(io.Reader)
crew, err := cfg.Build(
    crewai.WithLLMMap(map[string]crewai.LLM{...}),
    crewai.WithToolMap(...),
    crewai.WithGuardrailMap(...),
)
```

- Config validada por JSON Schema interno (P2 validator) com erro por caminho (D-Y4/D-Y10).
- `llm`/`tools`/`guardrail` são **referências por string** nos mapas da app; ref desconhecida = erro no Build (D-Y5 fail closed).
- `Task.Context` por **nome** (fallback índice — D-Y6); DAG validada com `planWaves` pós-build.
- **Sem interpolação de env/segredos** na v1 (D-Y8); teto de 1 MiB no reader (D-Y9).

### 4.3 Exemplo

`examples/declarative` — carrega config, constrói com mock LLM, Kickoff offline.

---

## 5. Trem X — Training / export de traces

### 5.1 Objetivos

Capturar runs **bem-sucedidas** em formato portável (JSONL) para few-shot/fine-tune offline. A lib só captura e grava; treino fica fora.

### 5.2 API (v1)

```go
rec := crewai.NewTraceRecorder(crewai.WithTraceFilter(func(r crewai.TaskTraceRecord) bool { return true }))
crew.Tracer = rec
out, err := crew.Kickoff(ctx, nil)
rec.Save("traces/success.jsonl") // 0600; path caller-trusted; opção de jail

type TaskTraceRecord struct {
    KickoffID, Task, Agent string
    Prompt  PromptSnapshot `json:"prompt,omitempty"`   // só com WithTraceBodies(true) — D-X4
    Output  string         `json:"output,omitempty"`   // idem
    Facts   []Fact         `json:"facts,omitempty"`
    Tools   []ToolTrace    `json:"tools,omitempty"`
    Wave    string         `json:"wave,omitempty"`
    DurationMs int64
}
```

- **D-X4:** corpos **opt-in** (`WithTraceBodies(true)`); default só metadados (alinhado a D-C4).
- Ordem no JSONL = **fold por declaração** (não conclusão) + `KickoffID` + label de wave (D-X8); recorder serializa com mutex (D-X7).
- Sem OTEL / sinks remotos (D-X3 JSONL).

### 5.3 Exemplo

`examples/trace_export` — mock crew grava JSONL e imprime resumo.

---

## 6. Gates por trem (review + coverage + docs embutidos)

Obrigatórios **por PR**, não só na entrega final.

### 6.1 Checklist de code review

- [ ] Contrato de concorrência inalterado (barreira + fold por declaração); novo estado compartilhado tem ownership documentado.
- [ ] Flows: single-flight por `*Flow[S]` (`ErrFlowRunning`, D-F9); ciclos e router steps validados no início.
- [ ] Tools: outputs com teto; helpers SSRF/jail **reutilizados**, não bifurcados.
- [ ] YAML: validação reusa planWaves; nada de execução a partir de arquivo; testes adversariais no parser JSON-subset.
- [ ] Training: default metadata-only; flag de corpos explicitamente `WithTraceBodies`.
- [ ] Sem vazamento de goroutines (join/cancel documentado); canais fechados por contrato.
- [ ] Sentinelas novas em `errors.go`, wrap `%w`, godoc exportado.
- [ ] API pública só aditiva.

### 6.2 Checklist de security review

- [ ] HTTP: matriz SSRF (privado/loopback/CGNAT/DNS-rebind/userinfo/non-http; hop de redirect; allowlist vazia nega).
- [ ] Arquivos: `../`, symlink fora do jail, absoluto fora → fail closed; write off por padrão.
- [ ] YAML loader: sem interpolação de env; refs desconhecidas falham; teto 1 MiB; sem includes remotos.
- [ ] Trace: corpos off por padrão; errors redigidos; perms `0600`.
- [ ] Flows: ownership de estado documentado; sem segredos implícitos em eventos.
- [ ] SECURITY.md por trem.

### 6.3 Gate de cobertura

- [ ] Agregado `go list ./... | grep -v /examples` permanece **≥ 90%** (hoje 92.8%).
- [ ] Cada pacote **tocado** ≥ 90% (testes junto com o código).
- [ ] `-race` limpo nos pacotes tocados.

### 6.4 Checklist de revisão de documentação (por trem)

- [ ] Guias novos: `docs/flows.md`, expansão `docs/tools.md`, `docs/declarative.md`, `docs/training.md`, `docs/rag.md` (+ PT).
- [ ] README Why + Concepts + What's new na tag de release.
- [ ] `doc.go`, examples READMEs, CHANGELOG EN+PT, checkbox PLAN, IDs novos em DECISIONS.
- [ ] Revisão de docs: snippets dos guias compilam/executam via examples.

---

## 7. Arquivos esperados

| Trem | Novos/alterados |
|---|---|
| **F** | `flow.go`(+test), sentinels, `examples/flows_research`, `docs/flows.md`(+PT), doc.go, README, PLAN |
| **T** | `tools/http.go`, `tools/files.go`, extração SSRF/jail compartilhada, `docs/tools.md`/`docs/rag.md`(+PT), 3 examples, SECURITY |
| **Y** | `load.go`(+test), schema const, `examples/declarative`, `docs/declarative.md`(+PT), errors, README |
| **X** | `trace.go`(+test), campo `Crew.Tracer` + hooks no executor, `examples/trace_export`, `docs/training.md`(+PT), SECURITY |

`process.go` intocado na v1 (Flows é runner separado).

---

## 8. Ordem de implementação (trens de PR)

```
Fase 0   Fechar D-F*, D-T*, D-Y*, D-X* neste doc → mover para DECISIONS.md
── T1    extração urlguard + HTTPFetch + testes + docs
── T2    extração pathJail + File tools + testes + docs
── F1    Flow core (runner + builder) + validação de DAG
── F2    concorrência do Flow (listeners paralelos, join/fold) + eventos
── Y1    loader JSON-subset + validação schema + Build maps
── X1    TraceRecorder metadata default + Save
── RAG   doc/exemplo (pode entrar a qualquer momento; M3 já existe)
── Sync release (CHANGELOG/PLAN/DECISIONS) → tag(s)
```

Empacotamento: um minor por trem (T→v0.9.0, F→v0.10.0, …) **ou** bundle v0.9.0 — decidir no release; o plano aceita ambos (default: bundle, split se algum trem atrasar).

---

## 9. Decisões a fechar (Fase 0)

### Flows (D-F*)

| ID | Pergunta | Opções | Recomendação |
|---|---|---|---|
| **D-F1** | Estilo do runner | (A) novo Process (B) tipo `Flow[S]` separado | **B** |
| **D-F2** | Registro de steps | (A) tags/reflection (B) builder `Listen/Router` | **B** |
| **D-F3** | Estado genérico | generics `Flow[S any]` | **sim** |
| **D-F4** | Starts | (A) zero-dep implícito (B) `Start` explícito | **A** (+doc) |
| **D-F5** | Lock de estado | (A) lib bloqueia (B) sync do usuário | **B** |
| **D-F6** | Resultado de router | nomes exatos | **sim** |
| **D-F7** | Eventos | tipos `flow_step_*` novos | **sim** |
| **D-F8** | ContinueOnError | erros em `FlowResult.Errors` | **sim** |
| **D-F9** | Runs concorrentes | single-flight | **sim** |
| **D-F10** | Join multi-dep | (A) todos (B) qualquer | **A** |
| **D-F11** | Cancelamento | ctx + fail-fast default | padrão |
| **D-F12** | Ordem de trace paralela | fold por ordem de registro | **sim** |

### Tools (D-T*)

| ID | Pergunta | Decisão |
|---|---|---|
| **D-T1** | HTTP tool no pacote tools | sim |
| **D-T2** | Allowlist padrão | deny-by-default |
| **D-T3** | Compartilhar helper de jail | sim |
| **D-T4** | Redirects | máx 3, revalidados |
| **D-T5** | Escrita de arquivo | off por padrão |
| **D-T6** | Tetos de bytes | reuso `MaxToolOutputBytes` v1 |
| **D-T7** | Conteúdo binário | rejeitar por padrão |
| **D-T8** | Métodos HTTP | GET padrão |
| **D-T9** | Helper RAG | (A) só docs na v1 |
| **D-T10** | Nomes | `tools.HTTPFetch`, `FileRead`, `FileWrite` |

### YAML (D-Y*)

| ID | Pergunta | Decisão |
|---|---|---|
| **D-Y1** | Parser | **A** subconjunto JSON no core |
| **D-Y2** | Entrada | `LoadCrew` + `LoadCrewFile` |
| **D-Y3** | Wiring | mapas de referência (llm/tools/guardrails) |
| **D-Y4** | Validação | JSON Schema interno (validator P2) |
| **D-Y5** | Refs desconhecidas | fail closed |
| **D-Y6** | Context | nome primeiro |
| **D-Y7** | Paridade de campos | subconjunto exportado + tabela doc |
| **D-Y8** | Interpolação | nenhuma v1 |
| **D-Y9** | Teto | 1 MiB |
| **D-Y10** | Erros | `ValidationError` com pointer |

### Training (D-X*)

| ID | Pergunta | Decisão |
|---|---|---|
| **D-X1** | Anexo do recorder | campo `Crew.Tracer` |
| **D-X2** | Ponto de emissão | pós-execute, filtrável |
| **D-X3** | Formato | JSONL |
| **D-X4** | Corpos | opt-in `WithTraceBodies(true)` |
| **D-X5** | Path | caller-trusted + 0600 |
| **D-X6** | Filtro | `WithTraceFilter` |
| **D-X7** | Concorrência | mutex no recorder |
| **D-X8** | Ondas Async | ordem de fold + KickoffID + wave |

---

## 10. Registro de riscos

| Risco | Mitigação |
|---|---|
| Corridas de estado no Flow culpadas na lib | D-F5 doc + exemplo race-tested com mutex |
| SSRF por re-resolução DNS entre guard e fetch | resolve-once + pin de IP se necessário (rev na review) |
| Subset JSON surpreende usuários | docs chamam “subconjunto YAML compatível com JSON” + erro com offset |
| Corpos de trace vazam segredos | footgun em SECURITY + redaction sempre ligada |
| Cobertura cair abaixo de 90 com 4 trens | gate por PR |
| Churn de API Flow vs expectativa CrewAI | tabela de mapeamento em docs/flows.md |

---

## 11. Aceite (epic concluído)

- [ ] D-F1–D-F12, D-T1–D-T10, D-Y1–D-Y10, D-X1–D-X8 fechadas em `DECISIONS.md`
- [ ] Runner `Flow[S]` com DAG, fold em barreira, eventos, `examples/flows_research`
- [ ] `tools.HTTPFetch`, `FileRead`/`FileWrite` (jail, SSRF, tetos) + 3 examples
- [ ] `docs/rag.md` (+ exemplo opcional)
- [ ] `LoadCrew` subconjunto JSON + validação + `examples/declarative`
- [ ] `TraceRecorder` metadata-default JSONL + `examples/trace_export`
- [ ] Docs EN+PT dos quatro trens; What's new; notas SECURITY
- [ ] Cobertura ≥90% (agregado + pacotes tocados), `-race` limpo
- [ ] CHANGELOG EN+PT; tags conforme §8

**Janela-alvo:** pós v0.8.0; **não** empacotar com backlog adiado.

---

## 12. Resumo

Quatro trens P3 independentes com a mesma doutrina: APIs aditivas, só stdlib, concorrência determinística, telemetria metadata-safe, tools deny-by-default. Fase 0 fecha decisões; depois os trens F/T/Y/X entram na ordem do §8, com code review, security review, cobertura e revisão de docs por PR já enumerados no §6.
