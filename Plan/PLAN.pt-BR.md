# Plano — Port do CrewAI para Go (crewai-go)

> **Languages:** [English](PLAN.md) · **Português** (atual)

Documento vivo do planejamento, decisões de arquitetura, status e roadmap do
port do [CrewAI](https://github.com/crewAIInc/crewAI) (Python) para Go.

---

## 1. Objetivo

Portar o **núcleo** do framework CrewAI para Go de forma **idiomática**,
**sem dependências externas** (apenas stdlib), com **documentação e testes
completos** e **exemplos** de instalação/uso.

## 2. Princípios de design

| Princípio | Decisão |
|-----------|---------|
| Zero dependências | Só biblioteca padrão do Go — fácil auditar, instalar e testar. |
| LLM-agnóstico | Interface `LLM` mínima; provedores em subpacotes. |
| Ferramentas via texto | Protocolo **ReAct** (Thought/Action/Observation/Final Answer) — sem depender de _function calling_ nativo de cada provedor. |
| Testável | LLM `mock` + `httptest`; testes hermeticos (sem rede real). |
| Idiomático | `context.Context` em toda I/O, erros sentinela, _functional options_. |

## 2.1 Convenções de documentação

| Convenção | Regra |
|-----------|-------|
| Docs bilíngues | `README.md`, `PLAN.md` e todos os `*.md` são mantidos em **ambos** os idiomas: inglês (padrão) e português (`*.pt-BR.md` / `docs/pt-BR/`). |
| Troca de idioma | Cada doc tem um link `> **Languages:** …` no topo para alternar entre EN/PT. |
| Comentários no código | Sempre em **inglês, sem acentos**. Vale para godoc, mensagens de erro, prompts e strings de identificadores. |
| Idioma padrão | Inglês é a versão primária/autoritativa; PT é um espelho fiel mantido em sincronia. |

## 3. Mapeamento CrewAI → crewai-go

| CrewAI (Python)          | crewai-go                          |
|--------------------------|------------------------------------|
| `Agent`                  | `crewai.Agent` / `NewAgent`        |
| `Task`                   | `crewai.Task` / `NewTask`          |
| `Crew`                   | `crewai.Crew` / `NewCrew`          |
| `crew.kickoff(inputs)`   | `crew.Kickoff(ctx, inputs)`        |
| `Process.sequential`     | `crewai.Sequential`                |
| `Process.hierarchical`   | `crewai.Hierarchical`              |
| `BaseTool` / `@tool`     | `crewai.Tool` / `NewTool`          |
| memória de curto prazo   | `crewai.Memory`                    |
| litellm                  | interface `LLM` + subpacotes `llm/*` |

## 3.1 Recursos exclusivos do crewai-go (ausentes no CrewAI Python)

Os recursos a seguir sao originais do crewai-go e nao tem equivalente direto
no framework CrewAI em Python:

| Recurso | O que faz | Por que importa |
|---|---|---|
| **Saida estruturada com validacao por JSON Schema** | `Task.Structured` (`*StructuredOutput`) exige que o modelo emita JSON validado contra um JSON Schema, com loop de reparo limitado (`RepairMax`). Validacao em Go, nao via prompt. | Garante dados tipados e confiaveis para pipelines que persistem facts em bancos de dados. Sem dependencia de validador externo. |
| **Guardrails pos-saida** | `Crew.Guardrails` e `Task.Guardrail` sao hooks de validacao pos-saida em codigo que bloqueiam a publicacao de saidas que violam invariantes de negocio. Retorna `ErrBlockedByGuardrail`. | A "barreira anti-alucinacao como garantia de codigo". Decisoes de bloqueio sao feitas em Go, nao via prompt. Complementa a validacao de schema (forma vs. significado). |
| **Modelo de Facts e proveniencia** | Tipo `Fact` de primeira classe, populado APENAS por ferramentas conectoras deterministas (`FactSource`), nunca pelo LLM. Facts carregam organizacao fonte, URL fonte, momento de coleta e hash SHA-256 do payload. Helper `AllFactsProvenanced` para guardrails. | Um valor errado nunca pode ser apresentado como "um fato que o modelo lembrou". Facts sao deduplicados por hash do payload e carregam proveniencia completa para auditoria. |
| **Zero dependencias externas** | O framework inteiro usa apenas a biblioteca padrao do Go. Sem pip install, sem conflitos de versao. | Facil de auditar, instalar e testar. `go.mod` tem zero diretivas require. |

## 4. Arquitetura (pacotes)

```
crewai (raiz)          Agent, Task, Crew, Process, Tool, Memory, LLM, executor ReAct/native,
                       StructuredOutput, Guardrails, Facts, Progress, RedactHandler,
                       DelegationTool
├── llm/openai         OpenAI e compatíveis + ToolCallingLLM + WebSearcher
├── llm/anthropic      Claude + ToolCallingLLM + WebSearcher
├── llm/ollama         Ollama local/cloud — /api/chat + tools + web search
├── llm/xai            Grok: API key + OAuth de assinatura (Device Flow RFC 8628 + PKCE)
├── llm/mock           LLM determinístico para testes
├── mcp                Cliente MCP Streamable HTTP, LoadConfig, ToolAdapter, FilterTools
├── tools              Calculadora, hora, contador, WebSearchTool + provedores de busca
├── examples           basic, sequential, hierarchical, staged, tools, native_tools,
                       agentic_loop, facts, guardrails, logging, delegation, mcp, …
└── docs               getting-started, agents, tasks, crews, tools, llms, memory, mcp
```

### Fluxo de execução

1. `Crew.Kickoff` garante single-flight (`ErrCrewRunning` se reentrante),
   anexa opcionalmente `delegate_to_coworker` se `EnableDelegationTool`,
   interpola `{inputs}` e injeta progress + role do agent no `ctx`.
2. Processo: Sequential | Hierarchical (manager escolhe agent) | Staged
   (estágios em ordem; tasks do estágio em paralelo).
3. Por tarefa: AgenticLoop (se houver) → StructuredOutput (AllowTools
   gather→capture / emit_result) → native ToolCallingLLM → ReAct. Facts,
   tool traces e warnings agregam em `CrewOutput`.

## 5. Status atual — ✅ CONCLUÍDO (fase 1)

- [x] Núcleo: Agent, Task, Crew, Process, Tool, Memory, executor ReAct.
- [x] Processos sequencial e hierárquico (com delegação por LLM).
- [x] Contexto entre tarefas (`WithContext`) e interpolação de `{inputs}`.
- [x] Provedores: OpenAI(+compatíveis), Anthropic, Ollama local, Ollama Cloud, xAI (API key + OAuth), mock.
- [x] OAuth de assinatura do xAI: Device Flow (RFC 8628) + PKCE + refresh + persistência.
- [x] Ferramentas embutidas (calculadora com parser próprio, hora, contador).
- [x] Testes herméticos (~90% núcleo; provedores via httptest) e `Example`s.
- [x] **Saida estruturada** — `Task.Structured` com validacao JSON Schema,
  loop de reparo, `WithToolCall` (emit_result), `WithAllowTools`, keywords
  expandidas (v0.5.0).
- [x] Documentação: README + guias bilíngues + MCP + SECURITY; 14 exemplos.
- [x] `go build`, `go vet` e `go test ./...` limpos.

### Snapshot de maturidade (2026-08-20 — v0.5.0)

| Métrica | Valor |
|---------|-------|
| Última release | **v0.5.0** (2026-08-20) |
| LOC Go (aprox.) | ~23k |
| Arquivos `.go` | ~111 |
| Dependências externas | 0 (stdlib) |
| Cobertura — núcleo (`crewai`) | ~96.9% |
| Cobertura — `mcp` / `tools` | ~92% / ~91% |
| Cobertura — provedores `llm/*` | todos ≥ 90% |
| Exemplos executáveis | 14 (+ espelhos pt-BR) |
| Documentação | README + guias bilíngues + MCP + SECURITY |
| CI | GitHub Actions (`gofmt`, `vet`, `test -race`) + CodeQL |

### Limitações conhecidas (ainda abertas após v0.5.0)

- **Sem streaming** — `LLM.Call` retorna a resposta completa; ainda não há
  `CallStream` / `StreamingLLM`.
- **Memória apenas em processo** — sem persistência nem busca semântica/embeddings.
- **JSON Schema ainda é um subconjunto** — núcleo + `additionalProperties`,
  bounds, `pattern`, `oneOf`/`anyOf`/`allOf` (v0.5.0). Ainda sem `$ref`,
  `if`/`then`/`else`, `format`, unevaluated*, etc.
- **Servidores MCP são confiáveis** — descriptions/resultados entram no
  contexto do modelo; use `FilterTools`, allowlists de rede e least privilege
  (ver threat model MCP).
- **Sem Flows event-driven / YAML / training** — roadmap P3.
- **Async além do staged** — parallel-within-stage existe; `async_execution`
  livre entre tasks arbitrárias não.

## 6. Roadmap — próximos passos (fora do escopo atual)

Recursos avançados do CrewAI original ainda **não** portados, por prioridade
sugerida (maior impacto / menor esforço primeiro):

- [x] **Agentic loop** (`AgenticLoop`) — estratégia de execução
  Planejar-Executar-Avaliar-Refinar, opt-in via `Agent.Loop`/`Task.Loop`. Veja
  `PLAN.agentic-loop.md` para o design completo.

- [x] **Residuais de security & code review (pós PR #27)** — **feitos na
  v0.5.0** (PR #28): timeout MCP + guards de catálogo + threat model, jail
  OutputFile, `RedactHandler`, single-flight do Kickoff, keywords de schema,
  `WithAllowTools`, `delegate_to_coworker`. Arquivo de design:
  [`PLAN.security-residuals.pt-BR.md`](PLAN.security-residuals.pt-BR.md)
  ([EN](PLAN.security-residuals.md)).

### Concluído até a v0.5.0 (não reabrir)

- [x] **Processo Staged** + estágios opcionais.
- [x] **Native function calling** (`ToolModeNative` + `ToolCallingLLM`) com fallback ReAct.
- [x] **Web search** — agent-driven + model-driven + SSRF.
- [x] **Cliente MCP** (`mcp/`) + SchemaProvider.
- [x] **Progress** + warnings por tarefa.
- [x] **Structured `emit_result`** + **`WithAllowTools`**.
- [x] **JSON Schema expandido** + `WithStrictSchema`.
- [x] **`RedactHandler`**, jail OutputFile, single-flight do Kickoff.
- [x] **Tool de delegação** `delegate_to_coworker` + `EnableDelegationTool`.
- [x] Tags **v0.1.0 … v0.5.0**; docs bilíngues; CI + CodeQL.

### P0 — Publicação e fundamentos

- [x] **`git init` + repositório público** — versionar, CI, tags até **v0.5.0**.
  - [x] **`git init` + push** — feito em `https://github.com/rhgs/crewai-go.git`.
  - [x] **Module path corrigido** — `github.com/rodolphosa/crewai-go` →
    `github.com/rhgs/crewai-go` em `go.mod` + 27 arquivos (imports, docs,
    exemplos). Publicação consistente com o remote.
  - [x] **G306 (gosec) corrigido** — `Task.setOutput` agora grava com `0600`.
  - [x] Definir CI (GitHub Actions: lint, `go vet`, `go test`, build de exemplos)
    — **feito (2026-08-04):** `.github/workflows/ci.yml` (gofmt, vet, build, `go test -race`).
    Badges de teste adicionados ao README EN/PT.
  - [x] Tag `v0.1.0` — **publicada (2026-08-04)** com CHANGELOG bilíngue.
  - [x] **Pin de toolchain para CVEs da stdlib** — `go.mod` declara
    `toolchain go1.24.9` (linguagem `go 1.24`). CI permanece em `1.24.x`.
    SDK local 1.24.4 ainda compila com `GOTOOLCHAIN=auto` baixando a
    toolchain pinada quando necessário. Reexecutar `govulncheck` após
    upgrade do SDK local.
- [x] **Confirmar module path** (`github.com/rodolphosa/crewai-go` → destino
  final) e atualizar import paths nos exemplos/docs. **Resolvido (2026-08-04).**
- [x] **Documentar o `go vet` e `-race` no CI** (testes já usam concorrência).
  `go test -race ./...` limpo.

## 6.1 Code review e validação de segurança (2026-08-04)

**Ferramentas:** `go vet` ✅ · `gofmt -l` ✅ · `go test -race` ✅ · `gosec` ·
`govulncheck`.

### gosec — 2 findings restantes (falsos positivos, sem `//nolint`)

| ID | Arquivo | Veredito |
|----|---------|----------|
| G304 | `llm/xai/oauth.go:314` `os.ReadFile(path)` | **Falso positivo.** `path` é
  fornecido pelo chamador da biblioteca (ex.: `~/.crewai-xai-token.json`), não
  por atacante. Uma lib não pode restringir o caminho que o usuário escolhe. |
| G117 | `llm/xai/oauth.go:305` marshal de `AccessToken` | **Falso positivo.** O
  propósito do arquivo é justamente persistir o token; não há exposição. |

Não há diretivas `//nolint` no código (campo `Nosec: 0`). Decidido deixar os
falsos positivos sem supressão para não mascarar findings reais no futuro.

### govulncheck — CVEs da stdlib (nota histórica Go 1.24.4)

Scans em Go 1.24.4 reportaram várias issues na stdlib (crypto/tls, crypto/x509,
etc.) nos caminhos TLS de providers/MCP. **Mitigação (higiene v0.5.0):**
`toolchain go1.24.9` no `go.mod`; CI usa `1.24.x`. Sem vuln no código da
aplicação. Re-scan após upgrades do SDK local.

### Code review manual — pontos de atenção

- **`Crew.Kickoff` single-flight (v0.5.0)** — Kickoff concorrente no mesmo
  `*Crew` retorna `ErrCrewRunning`. Reuso sequencial é suportado.
- **OAuth Device Flow** (`llm/xai/oauth.go`): implementação correta — PKCE com
  `crypto/rand` (32 bytes), S256, polling honra `authorization_pending`/`slow_down`
  (RFC 8628 §3.5), `refreshingSource` com `sync.Mutex`, persistência `0600`.
  Sem hardcoded de `client_id`/endpoints (configuráveis via options).
- **Auth dos provedores** (`llm/openai`): `TokenSource` dinâmico (OAuth) tem
  prioridade sobre chave estática; token resolvido por chamada → renovação
  automática. Correto.
- **`delegate` (hierárquico)** faz fallback gracioso para o 1º agente em caso de
  erro do LLM — não trava a execução.
- Nenhum `TODO`/`FIXME`/`HACK` no código de produção.

### Validação de segredos

Varredura completa (arquivos rastreados + histórico de commits): **nenhuma
chave, token ou credencial publicada.** Menções a segredos são apenas
placeholders (`sk-...`, `xai-...`) e nomes de variáveis de ambiente em
docs/README. `.gitignore` protege `.claude/`, `.env`, `*token.json`.

### P1 — Extensibilidade do núcleo

- [ ] **Streaming** — adicionar `CallStream(ctx, messages) (<-chan StreamChunk, error)`
  à interface `LLM` (ou interface opcional `StreamingLLM`) e propagar no executor.
- [x] **Function calling nativo** por provedor (OpenAI/Anthropic/Ollama/xAI)
  via `ToolModeNative` + `ToolCallingLLM` — ReAct continua default/fallback.
- [ ] **Async/paralelismo** de tarefas (`async_execution`) com `sync.WaitGroup`
  / `errgroup`-like usando goroutines e canais.
- [x] **Delegação real** entre agentes — `NewDelegationTool` /
  `delegate_to_coworker` + `EnableDelegationTool` (v0.5.0). Atribuição do
  manager hierárquico permanece separada.

### P2 — Persistência e observabilidade

- [ ] **Memória de longo prazo** com embeddings/busca semântica e persistência
  (interface `MemoryStore` + implementação em arquivo/sqlite como padrão).
- [ ] **Callbacks e telemetria** — hooks de início/fim de tarefa e iteração
  ReAct, exportáveis (log estruturado, métricas).
- [x] **Guardrails** — validacao de saida com retry via loop de reparo da saida
  estruturada (`Task.Structured` + `StructuredOutput.RepairMax`). O loop
  reenvia ao modelo os erros de validacao e tenta ate um numero limitado de
  tentativas. Nao transforma a saida (apenas valida).
- [x] **Guardrails pos-saida** — hooks de validacao pos-saida em codigo
  (`Crew.Guardrails` e `Task.Guardrail`) que bloqueiam a publicacao de saidas
  que violam invariantes de negocio. Nova sentinela `ErrBlockedByGuardrail`.
  Complementa a validacao de schema da saida estruturada (forma vs.
  significado). Cobertura 93.7% no pacote raiz.
- [x] **Facts e proveniencia** — tipo `Fact` de primeira classe, populado
  apenas por tools `FactSource`, nunca pelo LLM. Facts carregam organizacao
  fonte, URL fonte, momento de coleta e hash do payload (SHA-256). Construtor
  `NewFactSourceTool`, helper `AllFactsProvenanced`, `dedupFacts` por
  PayloadHash. Cobertura 94.5% no pacote raiz.
- [ ] **Extensoes da saida estruturada** — expandir o validador de JSON Schema
  para suportar mais palavras-chave (`additionalProperties`, `oneOf`/`anyOf`,
  `pattern`, `minimum`/`maximum`, `minItems`/`maxItems`); permitir uso de
  ferramentas antes da saida estruturada; function calling nativo opcional
  para saida estruturada (OpenAI/Anthropic). Rastreado como R4 + R8 em
  [`PLAN.security-residuals.pt-BR.md`](PLAN.security-residuals.pt-BR.md).

### P3 — Avançado

- [ ] **Flows** (orquestração event-driven com estado e roteamento).
- [ ] Mais ferramentas (HTTP, arquivos, RAG) — **web search já existe** (v0.3+).
- [ ] Definição declarativa via YAML (agents.yaml/tasks.yaml).
- [ ] _Training_ (ajuste fino de prompts a partir de execuções).

## 7. Decisões em aberto

- **Module path**: hoje `github.com/rhgs/crewai-go`. Ajustar ao publicar.
- **xAI OAuth**: `client_id` e endpoints exatos não são públicos; implementado
  conforme RFC 8628 com endpoints configuráveis. Fixar padrões quando a xAI
  publicar a documentação oficial.
- **Streaming**: definir se a interface `LLM` ganha um método `CallStream` (quebra
  implementações existentes) ou se introduzimos interface opcional
  `StreamingLLM` que o executor verifica com type assertion.
- **Function calling vs ReAct**: manter o ReAct como camada universal de
  ferramentas e adicionar function calling nativo como otimização opcional por
  provedor, ou migrar completamente para function calling? Tende-se à primeira.
