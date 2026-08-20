# Plan — Residuais de Security & Code Review (pós v0.4.x / PR #27)

> **Languages:** [English](PLAN.security-residuals.md) · **Português** (atual)
>
> **Status:** Rascunho para implementação (apenas plano — não implementar até ser agendado)
> **Escopo:** Fechar os oito itens residuais do code/security review pós-merge
> da branch `feature/mcp-tool-call-progress-warnings` (mergeada como PR #27 /
> `a8f01b7`). Os itens vão de defaults de hardening a features maiores
> (delegação real entre agents, híbrido structured+tools).
>
> **Relacionados:** `SECURITY.md`, `docs/pt-BR/mcp.md`, `Plan/PLAN.pt-BR.md`,
> `Plan/PLAN.structured-output.md`, `Plan/PLAN.guardrails.md`.

---

## 1. Objetivos e restrições

### 1.1 Objetivos

1. Transformar os oito residuais do review em um **backlog faseado e
   priorizado**, com APIs, testes, docs e non-goals claros.
2. Preferir **defaults seguros** e **expansão opt-in** a breaking changes.
3. Manter tudo **somente stdlib**, testável de forma hermética, com docs
   bilíngues (EN + PT-BR).
4. Separar **hardening** (timeouts, política de path, logging, contrato de
   concorrência) de **features de produto** (tool de delegação, híbrido
   structured+tools, keywords de schema).

### 1.2 Restrições (devem ser preservadas)

| Restrição | Como garantir |
|---|---|
| Zero dependências externas | `go.mod` intacto; sem libs de JSON Schema. |
| LLM-agnóstico | Sem hacks por provedor fora de `llm/*`. |
| Comentários em inglês, sem acentos | godoc, erros, prompts, identificadores. |
| Testes herméticos | `llm/mock` + `httptest`; sem rede real. |
| Erros sentinela + `context.Context` | Novos sentinelas em `errors.go`; `ctx` em todo I/O. |
| Backward compatible por padrão | Defaults só mudam onde o comportamento atual é inseguro ou indefinido; documentar opt-out. |
| Não enfraquecer trust boundaries | MCP continua modelo de **servidor confiável**, salvo modo untrusted explícito no futuro (fora do P0). |

### 1.3 Inventário dos residuais

| # | Residual | Severidade | Natureza | Fase sugerida |
|---|---|---|---|---|
| R1 | Cliente MCP padrão **sem timeout** (`http.DefaultClient`) | Média | Default de hardening | **P0** |
| R2 | Modelo MCP **trusted-server** / superfície de prompt injection | Média (ops) | Política + mitigações leves | **P0** (docs) / **P1** (guards opcionais) |
| R3 | `Task.OutputFile` é **path livre** (só modo `0600`) | Média | Política de path | **P0** |
| R4 | Validador JSON Schema é um **subconjunto** | Baixa–Média | Completude de feature | **P2** |
| R5 | Logs debug incluem output LLM / args de tools; redactor só no example | Média (ops) | Higiene de logging | **P1** |
| R6 | `Crew.Kickoff` **não é safe** para reuso concorrente do mesmo `Crew` | Média | Contrato de concorrência | **P1** |
| R7 | `AllowDelegation` ainda **informativo** (sem tool de coworker) | Baixa (gap de feature) | Feature de produto | **P2** |
| R8 | Path structured **ignora tools** (exceto `WithToolCall` emit) | Média (capacidade) | Feature de produto | **P2** |

---

## 2. Estado atual (baseline pós PR #27)

| Área | Hoje |
|---|---|
| Cliente MCP | `mcp.New` usa `http.DefaultClient` (sem timeout). `WithHTTPClient` existe. Docs já avisam. |
| Trust MCP | Docs/`SECURITY.md` dizem "trate como confiável". Sem sanitizer de description, sem allowlist além do que o caller anexa. |
| OutputFile | `task.setOutput` → `os.WriteFile(path, data, 0o600)` **sem** clean/jail. Documentado como trusted. |
| Schema | `schema.go` suporta só `type`, `properties`, `required`, `enum`, `items`. Documentado. |
| Logging | Core usa `log/slog`. `redactError`/`redactString` só para **erros**. `slog.Handler` redator vive em `examples/logging/`. Debug pode logar output LLM e args. |
| Concorrência Kickoff | Logger/progress "set before Kickoff" documentado. `interpolate` muta tasks; `mem` criado no Kickoff; **sem** single-flight no mesmo `*Crew`. Parallelismo **dentro** do staged é intencional e OK. |
| Delegação | Manager hierárquico escolhe um agent via LLM (`crew.delegate`). `AllowDelegation` é informativo. Sem tool "ask coworker" no raciocínio. |
| Structured | `executeTaskDefault`: se `t.Structured != nil` → `executeStructured` e **return** (sem ReAct/native tools). `WithToolCall()` só usa tool sintética `emit_result`. |

---

## 3. Fase P0 — Defaults de hardening (pequeno, ship rápido)

**Tema:** tornar o caminho seguro o caminho padrão, sem quebrar callers cuidadosos.

### 3.1 R1 — Timeout HTTP padrão no MCP

#### Problema
`http.DefaultClient` não tem timeout. MCP server travado segura
`Initialize` / `ListTools` / `CallTool` até matar o processo ou cancelar
`ctx` **e** o transport respeitar. Quem esquece `WithHTTPClient` herda isso.

#### Proposta

```go
// mcp/client.go
const DefaultHTTPTimeout = 30 * time.Second

// New: se WithHTTPClient não foi passado:
httpClient: &http.Client{Timeout: DefaultHTTPTimeout}

// WithHTTPClient(hc):
//   - nil → ignora (já é assim)
//   - non-nil → usa como está, mesmo Timeout == 0 (opt-out explícito)
```

`LoadConfig` passa por `New`, então uma mudança cobre os dois.

#### Compatibilidade
- **Mudança de comportamento** para quem dependia de hang infinito.
  Aceitável: CHANGELOG *Changed*; escape hatch
  (`WithHTTPClient` com `Timeout: 0`, `WithHTTPTimeout(0)`, ou JSON
  `"timeout": "0s"`).
- Testes que assumem `== http.DefaultClient` precisam assertar o timeout
  padrão (`TestNew_Defaults`, `TestWithHTTPClient_NilIgnored`).

#### Configuração (decidido — D1)
- **Default:** 30s.
- **Programático:** `WithHTTPClient` e/ou `WithHTTPTimeout(d)`.
- **JSON (`LoadConfig`):** campo opcional por servidor `timeout` (duration string Go).
  Ver §12 adendo D1 para schema e precedência.

#### Testes
- Client padrão com `Timeout == DefaultHTTPTimeout` (30s).
- `WithHTTPClient` preserva client custom (incl. `Timeout: 0`).
- `WithHTTPTimeout` só no path do client default.
- JSON: omit → 30s; `"45s"` → 45s; `"0s"` → 0; inválido → erro nomeando o server.
- Handler que não responde + timeout curto → erro.
- Coverage do pacote `mcp` permanece ≥ 90%.

#### Docs
- `docs/pt-BR/mcp.md`, `docs/en/mcp.md`, `SECURITY.md`, CHANGELOG EN/PT:
  default 30s; config programática + JSON; como desabilitar; deadline no ctx.

#### Non-goals (R1)
- Timeout por RPC separado do client.
- Retry/backoff.
- `RoundTripper` custom no default.

---

### 3.2 R3 — Política de path do OutputFile

#### Problema
`OutputFile` só é influenciado por atacante se a **aplicação** deixar input
não confiável configurá-lo. Ainda assim, app mal configurado pode sobrescrever
paths sensíveis. Modo `0600` não restringe localização.

#### Proposta (defesa em profundidade: higiene sempre + jail opt-in)

**Sempre ativo:**
1. Rejeitar path vazio após trim.
2. `path = filepath.Clean(path)`.
3. Rejeitar escapes quando jail estiver setada (abaixo).
4. Manter modo `0600`.
5. Criar parent dirs só com opt-in (`WithOutputFileMkdir` — default **off**).

**Jail opt-in (recomendado se config vem de fora):**

```go
// WithOutputDir restringe OutputFile a arquivos sob dir (após Clean +
// EvalSymlinks do dir — D2 decidido: sempre quando jail setada).
func WithOutputDir(dir string) /* opção de task ou default na Crew */

// Algoritmo (setOutput) — D2 = A:
//   clean := filepath.Clean(t.OutputFile)
//   if jail != "" {
//       absFile := Abs(clean); absJail := Abs(jail)
//       // EvalSymlinks dos dois lados; erro → ErrOutputPathRejected (fail closed)
//       // exigir absFile dentro de absJail (+ sep)
//   }
//   os.WriteFile(clean, data, 0o600)
```

**Default na Crew (opcional):** `Crew.OutputDir` quando a task tem
`OutputFile` e jail da task está vazia.

Sentinela nova:

```go
var ErrOutputPathRejected = errors.New("crewai: output path rejected")
```

#### Compatibilidade
- Sem jail → mesmo write após `Clean` (documentar).
- Com jail → fora da árvore falha fechado com `ErrOutputPathRejected`.

#### Testes
- Clean de `foo/../bar` → `bar`.
- Jail `/tmp/out`: permite `/tmp/out/a.txt`; rejeita escape e
  `/etc/passwd`; symlink escape se `EvalSymlinks` on.
- Modo `0600` (stat após write).
- `setOutput` continua mutex-safe.

#### Docs
- `docs/pt-BR/tasks.md`, EN, `SECURITY.md`: trusted por padrão;
  recomendar `OutputDir` com config externa; nunca passar output do modelo
  para `OutputFile` sem checagem.

#### Non-goals (R3)
- Sandbox/chroot de OS.
- Append/streaming.
- Criptografia at rest.

---

### 3.3 R2 (fatia P0) — MCP confiável: só documentação + escopo no caller

#### Problema
MCP server comprometido pode devolver **descriptions** que jailbreakam o
modelo, ou **results** que exfiltram contexto. Inerente a "tools = conteúdo
não confiável no prompt".

#### Proposta P0 (sem quebrar API)
1. Seção **Threat model** em `docs/pt-BR/mcp.md` + EN + `SECURITY.md`:
   - Server ≡ código confiável.
   - Escopo mínimo de tools por agent.
   - Headers MCP não logados (já verdade).
   - Allowlist de rede / endpoint privado no deploy.
2. **Checklist** operacional (timeout, TLS, token `0600`, least privilege).
3. Cross-link na seção MCP do README.

#### Non-goals P0
- Sanitização automática de descriptions (P1).
- Allowlist de server dentro da lib (trabalho do caller no P0).

---

## 4. Fase P1 — Contratos e segurança operacional

### 4.1 R5 — Redação de logs no core

#### Problema
`redactError` cobre strings de erro. Atributos debug (`args`, texto do
modelo) ainda vão ao handler. Produção precisa copiar o redactor de
`examples/logging` manualmente.

#### Proposta

Promover helper opt-in ao **pacote raiz** (D3 decidido):

```go
// RedactHandler envolve inner e aplica redactString a atributos string
// e à mensagem do record. Não remove campos.
func RedactHandler(inner slog.Handler) slog.Handler
```

Notas:
- Reusar padrões de `redactString` (API keys, Bearer, secrets em query).
- **Não** mudar o logger default (sem redaction) — opt-in.
- Redaction é **best-effort**, não boundary de confidencialidade.
- Orientar Verbose: `LevelInfo` em prod; `LevelDebug` só local.

#### Stretch P1
- Cap de tamanho de tool-args em linhas debug (`truncateForLog`).

#### Testes
- Mascara `sk-...`, `Bearer ...`, `access_key=` em attrs e message.
- `WithAttrs` / `WithGroup` preservam o wrapper.
- `Enabled()` delega ao inner.

#### Docs
- README Logging: one-liner com `RedactHandler`.
- `examples/logging` vira wrapper fino do helper core.
- `SECURITY.md` atualizado.

#### Non-goals (R5)
- Allowlist de campos / tipos secretos estruturados.
- Criptografar logs.
- Redigir payloads de `Progress` (já só metadados).

---

### 4.2 R6 — Single-flight / contrato de reuso do Kickoff

#### Problema
Reusar o mesmo `*Crew` em `Kickoff` concorrentes raceia logger, memory,
interpolate e agregação. Reuso sequencial também remuta descriptions.

#### Proposta

**Documentar** (P1.a) e **enforcer single-flight** (P1.b) —
**D4 decidido: TryLock + ErrCrewRunning** (fail fast, sem fila):

```go
type Crew struct {
    // ...
    runMu sync.Mutex // segurado durante Kickoff
}

func (c *Crew) Kickoff(...) (*CrewOutput, error) {
    if !c.runMu.TryLock() {
        return nil, ErrCrewRunning
    }
    defer c.runMu.Unlock()
    // ...
}
```

`TryLock` existe em Go 1.24 — OK.

```go
var ErrCrewRunning = errors.New("crewai: crew already running")
```

**Interpolate (P1.c — follow-up opcional):** snapshot ou `rawDescription`
— **non-goal no P1** salvo quebra observada.

#### Testes
- Duas goroutines: uma OK, outra `ErrCrewRunning`.
- Kickoff sequencial OK.
- Tasks paralelas no staged dentro de um Kickoff ainda `-race` clean.

#### Docs
- `docs/pt-BR/crews.md`: um Kickoff por vez por valor de Crew; Crews
  separados para runs paralelos.
- godoc em `Kickoff`.

#### Non-goals (R6)
- Fan-out de múltiplos Kickoffs isolados no mesmo Crew (redesign).
- Deep copy de Agents/Tools.

---

### 4.3 R2 (fatia P1) — Guards opcionais de catálogo MCP

#### Proposta (opt-in)

```go
type AdapterOption func(*ToolAdapter)

func WithDescriptionLimit(n int) AdapterOption
func FilterTools(tools []Tool, allow map[string]struct{}) []Tool
```

Também: strip de controles ASCII nas descriptions quando o limit estiver on.

#### Testes
- Description truncada; filtro por nome.
- Sem options → comportamento idêntico ao de hoje.

#### Docs
- Quando usar allowlist; não substitui confiar no endpoint.

---

## 5. Fase P2 — Residuais de capacidade (features)

### 5.1 R4 — Expansão do validador JSON Schema

#### Problema
Subconjunto bloqueia schemas reais (`additionalProperties: false`, bounds,
`pattern`, `oneOf`).

#### Proposta (keywords incrementais, ainda stdlib)

Ordem sugerida:

| Keyword | Comportamento |
|---|---|
| `additionalProperties` | `false` rejeita keys extras; objeto → valida extras; ausente = allow. |
| `minLength` / `maxLength` | strings em **bytes** (`len(s)` — **D7 decidido**); documentar no godoc + tasks. |
| `minimum` / `maximum` (+ exclusive*) | números via `float64` com limites documentados. |
| `minItems` / `maxItems` | arrays. |
| `pattern` | `regexp.Compile` com cap de tamanho; erro de schema no load se Strict. |
| `oneOf` / `anyOf` / `allOf` | composição. |

Continuar lista explícita de **não suportados**: `$ref`, `if/then/else`,
`unevaluatedProperties`, etc.

#### API
- Sem break: `validateSchema` ganha keywords.
- `StrictSchema bool` opcional em `StructuredOutput` para falhar cedo se
  houver keyword desconhecida.

#### Testes
- Table-driven por keyword; repair loop com multi-erro.
- `pattern` inválido com `StrictSchema`.

#### Docs
- Tabela de keywords em `docs/pt-BR/tasks.md` + EN.
- CHANGELOG *Added*.

#### Non-goals (R4)
- Compliance total draft 2020-12.
- Lib externa.
- Validação de content binário.

---

### 5.2 R8 — Structured output **com** uso prévio de tools

#### Problema
`executeTaskDefault` short-circuita para `executeStructured` se
`Structured != nil`, então o agent não coleta facts via tools e emite JSON
validado na mesma task (a menos que se divida em tasks).

#### Proposta

```go
type StructuredOutput struct {
    // ...
    // AllowTools: loop de tools limitado ANTES da fase de captura JSON.
    AllowTools bool
}

func WithAllowTools() func(*StructuredOutput)
```

**Pipeline com `AllowTools`:**

1. **Gather** — ReAct ou native conforme `ToolMode`, cap de iterações.
   Final answer não precisa ser JSON. Se esgotar iterações → **D6:** `AddWarning` + seguir para capture.
2. **Capture** — `executeStructuredJSON` ou `executeStructuredToolCall`
   com transcript truncado + schema.
3. Facts do gather **mesclados** (hoje structured devolve `nil` facts —
   corrigir).

**Default:** `AllowTools == false` = comportamento atual.

#### Interação com agentic loop
- Loop continua estratégia externa; `AllowTools` vive no execute interno.

#### Testes
- Mock: tool depois JSON → facts + schema OK.
- `AllowTools false` → tool spy nunca chamada.
- Repair após gather.
- Matriz native + `WithToolCall` + `AllowTools`.

#### Docs
- Diagrama two-phase em `docs/pt-BR/tasks.md`.
- Example opcional depois.

#### Non-goals (R8)
- Tools **durante** repair JSON.
- Multi-schema routing.
- Streaming de tokens structured.

---

### 5.3 R7 — Tool real de delegação entre agents

#### Problema
`AllowDelegation` não expõe tool para pedir ajuda a coworker no meio do
raciocínio. Hierárquico só atribui a task inteira uma vez.

#### Proposta

```go
func NewDelegationTool(roster DelegationRoster) Tool

type DelegationRoster interface {
    Agents() []*Agent
}
```

Contrato da tool:
- Nome: `delegate_to_coworker`
- Input: `{"coworker":"<role>","request":"<text>","context":"<opt>"}`
- Resolve por `Role` exato; **exigir** `AllowDelegation` no alvo.
- Execução aninhada com orçamento de profundidade.
- Observation truncada.

**Guardas:**

```go
const DefaultMaxDelegationDepth = 2
// ctx: depth++; ciclo A→B→A bloqueado
```

**Anexar a tool (D5 decidido = C):**
- Explícito sempre funciona: `agent.WithTools(NewDelegationTool(crew))`.
- Auto opt-in via `Crew.EnableDelegationTool bool` (**default false**).
  Quando true, anexa `delegate_to_coworker` aos agents elegíveis.
  Alvos ainda exigem `AllowDelegation == true`.

#### Testes
- Happy path com mocks.
- Coworker desconhecido → erro de observation.
- Depth / ciclo.
- Alvo sem `AllowDelegation` rejeitado.
- `-race` com staged + delegation.

#### Docs
- `AllowDelegation` deixa de ser "informativo" → "alvo elegível".
- Example `examples/delegation`.

#### Non-goals (R7)
- Bus de conversa multi-agent completo.
- Budget financeiro além de depth.
- Substituir assignment do manager hierárquico.

---

## 6. Regras transversais de implementação

1. **Ordem:** P0 → P1 → P2. Não começar R7/R8 com P0 aberto.
2. **Um PR por residual** (ou par bem justificado).
3. **CHANGELOG** EN + PT-BR (*Security* / *Added* / *Changed*).
4. **Docs bilíngues** no mesmo PR do código (ver §6.1).
5. **`go test -race ./...`**, `gofmt`, `go vet`; govulncheck no pre-commit local.
6. **Sem `//nolint`** salvo falso positivo documentado (política OAuth).
7. **Code review + cobertura** em todo PR (ver §6.1) — não negociável.

### 6.1 Quality gates permanentes (todo PR A–J)

Estes gates valem para **todo** PR de residual. PR que falhar qualquer gate
não é mergeável.

| Gate | Requisito | Como verificar |
|---|---|---|
| **Code review** | Checklist de self-review no corpo do PR (template abaixo). Preferir segundo review humano em mudanças de trust boundary (MCP, paths, delegação, schema). | Seção "Code review" do PR |
| **Cobertura ≥ 90%** | Pacote(s) tocados devem permanecer com **≥ 90%** de coverage de statements. Não reduzir pacote que já está ≥ 90%. Pacotes novos começam ≥ 90%. | `go test ./<pkg>/... -cover` e `go test ./... -cover` |
| **Race-clean** | `go test -race` nos pacotes tocados (e `./...` antes do merge). | CI + local |
| **Docs EN + PT-BR** | Comportamento, APIs novas, notas de segurança e CHANGELOG nos **dois** idiomas no mesmo PR. Godoc só em inglês. | Diff inclui `docs/**`, `docs/pt-BR/**`, changelogs conforme necessário |
| **gofmt / go vet** | Limpos. | CI + pre-commit |
| **Sem deps novas** | Bloco require de `go.mod` intacto. | `git diff go.mod` |

**Baseline (pós PR #27, aprox.):** root ~96.7%, `mcp` ~90.5%, `tools` ~91.2%,
`llm/openai` ~92.6%, `llm/ollama`/`xai` ~90.7%, `llm/mock` ~93.3%,
`llm/anthropic` ~89.2% ⚠️ — PR que tocar anthropic deve subir para ≥ 90%.

**Regras de cobertura (estritas):**
1. Tocou P e P ≥ 90% → PR deixa P ≥ 90%.
2. Tocou P e P < 90% → PR **deve** levar P a ≥ 90% (ou split com fix antes).
3. `examples/**` isentos do gate de 90% (sem testes por design), mas devem
   compilar (`go build ./examples/...`).
4. Preferir testes table-driven + `httptest`; sem rede real.

**Checklist de code review (copiar em todo PR):**

```markdown
## Code review

- [ ] Trust boundary revisada (paths, rede, resultados de tools, logs)
- [ ] Erros são sentinelas / wrapped; sem segredos em strings de erro
- [ ] ctx cancel/deadline respeitado em I/O novo
- [ ] Sem reads/writes ilimitados (LimitReader / caps)
- [ ] Concorrência: sem data races; single-flight / mutex documentado
- [ ] Backward compatible ou CHANGELOG *Changed* + nota de migração
- [ ] Testes: happy path, falha e edge/escape
- [ ] Cobertura dos pacotes tocados ≥ 90% (`go test -cover`)
- [ ] Docs EN + PT-BR + godoc atualizados
- [ ] `gofmt` / `go vet` / `go test -race` limpos
```

---

## 7. Mapa de arquivos

| Residual | Arquivos principais | Testes | Docs |
|---|---|---|---|
| R1 | `mcp/client.go`, `mcp/config.go` | `mcp/client_test.go` | mcp EN/PT, SECURITY, CHANGELOG |
| R2 | docs (+ depois `mcp/adapter.go`) | adapter se options | MCP + SECURITY |
| R3 | `task.go`, talvez `crew.go`, `errors.go` | testes de task | tasks EN/PT, SECURITY |
| R4 | `schema.go` | `schema_*_test.go` | tasks EN/PT |
| R5 | `redact.go` / `log.go` | `redact_test.go` | README logging, example |
| R6 | `crew.go`, `errors.go` | `crew_*_test.go` | crews EN/PT |
| R7 | `delegation.go` (novo), `agent.go` | `delegation_test.go` | agents/crews, example |
| R8 | `structured.go`, `executor.go` | `structured_*_test.go` | tasks EN/PT |

---

## 8. Critérios de aceite

### P0
- [ ] R1: timeout MCP default 30s; escape hatch documentado; testes OK.
- [x] R3: `Clean` sempre; `OutputDir` opt-in; `ErrOutputPathRejected`; testes de escape.
- [x] R2-P0: threat model + checklist em MCP docs EN/PT + SECURITY.

### P1
- [x] R5: `RedactHandler` no core; example delega; snippet no README.
- [x] R6: Kickoff concorrente → `ErrCrewRunning`; docs single-flight.
- [x] R2-P1 (opcional): limit de description + filter de nomes.

### P2
- [ ] R4: ao menos `additionalProperties`, bounds, `pattern`, `oneOf`/`anyOf` testados.
- [ ] R8: `WithAllowTools` two-phase; facts preservados; default igual.
- [ ] R7: tool `delegate_to_coworker` + depth/ciclo; semântica de `AllowDelegation` atualizada.

### Global
- [ ] `go test -race ./...`; docs bilíngues; CHANGELOG; zero deps novas.
- [ ] Checklist de code review (§6.1) preenchido no PR.
- [ ] Pacotes tocados com **≥ 90%** de coverage (`go test -cover`); `llm/anthropic` ≥ 90% se tocado.

---

## 9. Non-goals (plano inteiro)

- Quebrar a regra de zero dependências.
- Compliance total de JSON Schema.
- Marketplace MCP multi-tenant "untrusted" dentro da lib.
- Sandbox OS para tools ou `OutputFile`.
- Streaming de LLM (item separado no `PLAN.md`).
- Memória de longo prazo / embeddings (item separado).
- Secret scanning automático de outputs do modelo além de log redaction.

---

## 10. Rollout e versionamento (esboço)

| Fatia | Versão sugerida | Notas |
|---|---|---|
| P0 (R1, R3, R2 docs) | `v0.5.x` | Timeout default = *Changed* leve; jail opt-in. |
| P1 (R5, R6, R2 guards) | `v0.5.x` / `v0.6.0` | APIs aditivas + erro single-flight. |
| P2 (R4, R8, R7) | `v0.6.0+` | Minor de feature; README. |

Números exatos ficam com o processo de release (inclui higiene
"tag Unreleased → v0.5.0" fora deste plano).

---

## 11. Breakdown de PRs

### PR-A — Timeout MCP (R1) — D1 decidido: 30s + prog + JSON
1. `DefaultHTTPTimeout = 30s`; default em `New`.
2. `WithHTTPTimeout(d)`; campo JSON `timeout` em config/`LoadConfig`.
3. Testes unitários + hang + tabela JSON de durations.
4. Docs EN+PT + SECURITY + CHANGELOG; coverage `mcp` ≥ 90%; checklist de code review.

### PR-B — Política OutputFile (R3) — D2 decidido: EvalSymlinks on na jail
1. `Clean` + vazio; `ErrOutputPathRejected`.
2. `OutputDir`; sempre `EvalSymlinks` com jail; fail closed.
3. Testes (escape, symlink, modo 0600) + docs EN/PT + SECURITY; coverage ≥ 90%; code review.

### PR-C — Threat model MCP (R2-P0)
1. Só docs EN/PT + SECURITY.

### PR-D — RedactHandler (R5) — D3 decidido: pacote raiz
1. `crewai.RedactHandler` no root; testes; example fino; README EN/PT.
2. Coverage ≥ 90% no root; checklist de code review.

### PR-E — Single-flight Kickoff (R6) — D4 decidido: ErrCrewRunning
1. `TryLock` + `ErrCrewRunning`; teste race concorrente; sequencial OK.
2. Docs crews EN/PT + godoc; coverage ≥ 90%; code review.

### PR-F — Guards de catálogo MCP (R2-P1, opcional)
1. Options + `FilterTools`; testes; docs.

### PR-G — Schema batch 1 (R4) — D7 decidido: length em bytes
1. `additionalProperties`, bounds (`minLength`/`maxLength` = bytes); testes; docs EN/PT.
2. Coverage ≥ 90%; code review.

### PR-H — Schema batch 2 (R4)
1. `pattern`, `oneOf`/`anyOf`/`allOf`; `StrictSchema`; testes; docs.

### PR-I — Structured + tools (R8) — D6 decidido: warn + capture
1. Pipeline `AllowTools`; gather esgotado → `AddWarning` e capture; facts.
2. Matriz de testes; docs EN/PT; coverage ≥ 90%; code review.

### PR-J — Delegation tool (R7) — D5 decidido: EnableDelegationTool opt-in
1. Tool + depth/ciclo; `Crew.EnableDelegationTool` (default false) + WithTools explícito.
2. `AllowDelegation` = alvo elegível; example; docs EN/PT; coverage ≥ 90%; code review.

---

## 12. Decisões em aberto — decidir antes da implementação

> Legenda: ⏳ pendente · ✅ decidido
>
> **Todas D1–D7 decididas (2026-08-20).** PRs A–J podem seguir na ordem das
> fases. Quality gates da §6.1 valem sempre.

### D1 — R1 timeout HTTP padrão do MCP ✅

| | |
|---|---|
| **Pergunta** | Timeout default do client quando o caller omite `WithHTTPClient`? |
| **Opção A** | **30s** — falha mais rápido; MCP cold-start pode precisar override |
| **Opção B** | **60s** — mais amigável a cold-start; server travado demora mais |
| **Opção C** | **Sem timeout default** (rejeita o residual) |
| **Escape hatch (A/B)** | `WithHTTPClient(&http.Client{Timeout: 0})` ou client custom |
| **Recomendação** | **A — 30s**. Timeout longo deve ser explícito; combinar com deadline no ctx. |
| **Decisão** | **A — 30s**, configurável **programaticamente e via JSON** (ver adendo). |
| **PR** | A |
| **Data** | 2026-08-20 |

**Superfície de configuração decidida (adendo D1):**

1. **Default:** `DefaultHTTPTimeout = 30 * time.Second` quando nenhum client custom é passado.
2. **Programático:**
   - `mcp.WithHTTPClient(&http.Client{Timeout: d})` — controle total (incl. `Timeout: 0` para desligar).
   - `mcp.WithHTTPTimeout(d time.Duration)` — atalho para o client default da lib, sem montar `http.Client`.
   - **Precedência:** `WithHTTPClient` fornecido pelo caller é usado **como está**. `WithHTTPTimeout` só vale quando a lib constrói o client default.
3. **JSON (`LoadConfig`):** estender o `ServerConfig` existente (chave top-level `servers`) com `Timeout` opcional / `json:"timeout"`, ex.:
   ```json
   {
     "servers": [
       {
         "name": "filesystem",
         "endpoint": "http://127.0.0.1:8080/mcp",
         "headers": { "Authorization": "Bearer …" },
         "timeout": "30s"
       }
     ]
   }
   ```
   - `timeout` aceita duration string do Go (`"30s"`, `"1m"`, `"0s"`).
   - Omitido / vazio → `DefaultHTTPTimeout` (30s).
   - `"0s"` / zero → sem timeout de client (opt-out explícito; deadlines de ctx ainda valem).
   - Duration inválida → erro de `LoadConfig` nomeando o servidor (sem segredos no erro).
4. **Docs:** guias MCP EN+PT + SECURITY + CHANGELOG com default, opções programáticas e campo JSON.
5. **Testes / gates:** default 30s; `WithHTTPTimeout`; JSON omit / `"45s"` / `"0s"` / inválido; hang + timeout curto; coverage `mcp` ≥ 90%; checklist de code review; docs EN+PT.

### D2 — R3 EvalSymlinks na jail do OutputDir ✅

| | |
|---|---|
| **Pergunta** | Com jail `OutputDir`, avaliar symlinks antes do prefix check? |
| **Opção A** | **On por default** na jail (`EvalSymlinks`) — fecha escapes |
| **Opção B** | **Opt-in** via `WithOutputDirEvalSymlinks(true)` |
| **Opção C** | **Nunca** — só `Abs` + prefix (mais fraco) |
| **Recomendação** | **A — on por default** quando a jail está configurada (jail já é opt-in). Se `EvalSymlinks` falhar, fail closed com `ErrOutputPathRejected`. |
| **Decisão** | **A — EvalSymlinks on por default** sempre que a jail `OutputDir` estiver setada. Fail closed se eval falhar. |
| **PR** | B |
| **Data** | 2026-08-20 |

### D3 — R5 onde fica o RedactHandler ✅

| | |
|---|---|
| **Pergunta** | Onde vive `RedactHandler`? |
| **Opção A** | **Pacote raiz** `crewai.RedactHandler` |
| **Opção B** | **Subpacote** (ex. `crewai/logredact`) |
| **Recomendação** | **A — raiz**. Uma função; `redactError` já está no root. |
| **Decisão** | **A — pacote raiz** `crewai.RedactHandler`. |
| **PR** | D |
| **Data** | 2026-08-20 |

### D4 — R6 Kickoff concorrente ✅

| | |
|---|---|
| **Pergunta** | Segundo `Kickoff` concorrente no mesmo `*Crew`? |
| **Opção A** | **`TryLock` + `ErrCrewRunning`** — fail fast |
| **Opção B** | **Bloquear** no mutex até o primeiro terminar |
| **Recomendação** | **A — erro**. Fila esconde sobrecarga e complica cancel de ctx. |
| **Decisão** | **A — `TryLock` + `ErrCrewRunning`** (fail fast). |
| **PR** | E |
| **Data** | 2026-08-20 |

### D5 — R7 como anexar a tool de delegação ✅

| | |
|---|---|
| **Pergunta** | Como os agents ganham `delegate_to_coworker`? |
| **Opção A** | **Só explícito** — `WithTools(NewDelegationTool(crew))` |
| **Opção B** | **Auto-attach** no hierárquico se `AllowDelegation` |
| **Opção C** | **Auto opt-in** via `Crew.EnableDelegationTool` (default false) + explícito |
| **Recomendação** | **C — flag opt-in, default off**. Sem surpresa; ergonômico no hierárquico quando ligado. Alvo ainda exige `AllowDelegation`. |
| **Decisão** | **C — `Crew.EnableDelegationTool` opt-in (default false)** + `WithTools(NewDelegationTool(...))` explícito ainda suportado. Alvos exigem `AllowDelegation`. |
| **PR** | J |
| **Data** | 2026-08-20 |

### D6 — R8 gather esgotado ✅

| | |
|---|---|
| **Pergunta** | `AllowTools` esgotou `MaxIterations` sem parada limpa — e agora? |
| **Opção A** | **Seguir para capture** com o que houver |
| **Opção B** | **Falhar a task** (`ErrGatherBudgetExceeded`) |
| **Opção C** | **Seguir + `AddWarning`** ("gather budget exhausted") e capture |
| **Recomendação** | **C — seguir + warning**. Observabilidade sem ser tão duro quanto B nem silencioso como A. |
| **Decisão** | **C — seguir para capture + `AddWarning`** ("gather budget exhausted"). |
| **PR** | I |
| **Data** | 2026-08-20 |

### D7 — R4 unidade de minLength/maxLength ✅

| | |
|---|---|
| **Pergunta** | `minLength` / `maxLength` contam o quê? |
| **Opção A** | **Bytes** (`len(s)`) |
| **Opção B** | **Runes** (`utf8.RuneCountInString`) |
| **Recomendação** | **A — bytes**, documentado no godoc + docs de tasks. |
| **Decisão** | **A — bytes** (`len(s)`), documentado no godoc + tasks EN/PT. |
| **PR** | G |
| **Data** | 2026-08-20 |

### Log de decisões (preencher ao resolver)

| ID | Residual | Escolha | Data | Notas |
|---|---|---|---|---|
| D1 | R1 timeout | **A (30s)** + prog + JSON | 2026-08-20 | `WithHTTPTimeout` + JSON `timeout`; `0` desliga |
| D2 | R3 symlinks | **A** EvalSymlinks on na jail | 2026-08-20 | fail closed se eval falhar |
| D3 | R5 package | **A** root `crewai.RedactHandler` | 2026-08-20 | |
| D4 | R6 Kickoff | **A** TryLock + `ErrCrewRunning` | 2026-08-20 | fail fast |
| D5 | R7 attach | **C** `EnableDelegationTool` opt-in default false | 2026-08-20 | WithTools explícito ainda OK |
| D6 | R8 gather | **C** seguir + `AddWarning` | 2026-08-20 | depois capture |
| D7 | R4 length | **A** bytes (`len`) | 2026-08-20 | documentar em tasks |

---

## 13. Referências

- Merge: PR #27 — https://github.com/rhgs/crewai-go/pull/27
- Default MCP: `mcp/client.go` (`http.DefaultClient`)
- Write de output: `task.go` `setOutput`
- Subconjunto schema: `schema.go` godoc de `validateSchema`
- Short-circuit structured: `executor.go` `executeTaskDefault`
- AllowDelegation: `agent.go`
- Example redactor: `examples/logging/main.go`
- Notas de operador: `SECURITY.md`

---

## 14. Acompanhamento de status

| Residual | Fase | PR | Status |
|---|---|---|---|
| R1 timeout MCP | P0 | A | **Implementado** na branch (D1) |
| R2 trust MCP docs/guards | P0/P1 | C/F | **Implementado** (threat model + guards de catalogo) |
| R3 política OutputFile | P0 | B | **Implementado** na branch (D2) |
| R4 keywords schema | P2 | G/H | **D7 decidido** (bytes); impl pendente |
| R5 RedactHandler | P1 | D | **Implementado** na branch (D3) |
| R6 single-flight Kickoff | P1 | E | **Implementado** na branch (D4) |
| R7 tool de delegação | P2 | J | **D5 decidido** (EnableDelegationTool opt-in); impl pendente |
| R8 structured+tools | P2 | I | **D6 decidido** (warn+capture); impl pendente |
