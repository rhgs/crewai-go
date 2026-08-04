# Plano — Port do CrewAI para Go (crewai-go)

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

## 4. Arquitetura (pacotes)

```
crewai (raiz)          Agent, Task, Crew, Process, Tool, Memory, LLM, executor ReAct
├── llm/openai         OpenAI e compatíveis (Groq, Azure…) + WithTokenSource (OAuth)
├── llm/anthropic      Claude (system prompt separado)
├── llm/ollama         Ollama local (sem auth) e cloud (Bearer) — API nativa /api/chat
├── llm/xai            Grok: API key + OAuth de assinatura (Device Flow RFC 8628 + PKCE)
├── llm/mock           LLM determinístico para testes
├── tools              Calculadora, hora atual, contador de palavras
├── examples           basic, sequential, hierarchical, tools, custom_llm, ollama, xai_oauth
└── docs               getting-started, agents, tasks, crews, tools, llms, memory
```

### Fluxo de execução

1. `Crew.Kickoff` interpola `{inputs}` nas tarefas.
2. Sequencial: executa em ordem, encadeando contexto/memória.
   Hierárquico: um gerente (ManagerLLM/ManagerAgent) delega cada tarefa.
3. Por tarefa, o `executor` roda o laço ReAct: monta prompt (persona + tools),
   chama o LLM, interpreta `Action`/`Final Answer`, executa ferramentas e
   repete até a resposta final ou `MaxIterations`.

## 5. Status atual — ✅ CONCLUÍDO (fase 1)

- [x] Núcleo: Agent, Task, Crew, Process, Tool, Memory, executor ReAct.
- [x] Processos sequencial e hierárquico (com delegação por LLM).
- [x] Contexto entre tarefas (`WithContext`) e interpolação de `{inputs}`.
- [x] Provedores: OpenAI(+compatíveis), Anthropic, Ollama local, Ollama Cloud, xAI (API key + OAuth), mock.
- [x] OAuth de assinatura do xAI: Device Flow (RFC 8628) + PKCE + refresh + persistência.
- [x] Ferramentas embutidas (calculadora com parser próprio, hora, contador).
- [x] Testes herméticos (~90% núcleo; provedores via httptest) e `Example`s.
- [x] Documentação: README + 7 guias + godoc; 7 exemplos executáveis.
- [x] `go build`, `go vet` e `go test ./...` limpos.

### Snapshot de maturidade (2026-08-04)

| Métrica | Valor |
|---------|-------|
| LOC Go (total) | 3.818 |
| Arquivos `.go` | 41 |
| Dependências externas | 0 (stdlib) |
| Cobertura — núcleo (`crewai`) | 90.0% |
| Cobertura — `tools` | 92.1% |
| Cobertura — provedores `llm/*` | 71.7%–87.5% |
| Exemplos executáveis | 7 |
| Guias de documentação | 7 + README |

### Limitações conhecidas da fase 1

- ~~**Sem git**~~ — **resolvido (2026-08-04):** repositório inicializado e publicado
  em https://github.com/rhgs/crewai-go.git (branch `main`), `.gitignore` cobrindo
  `.claude/`, tokens OAuth do xAI e `.env`; licença MIT mantida (remote trazia GPL
  v3 do template do GitHub, resolvido em favor da MIT declarada no README). Falta
  apenas configurar CI.
- **Hierárquico simplificado** — o gerente escolhe um agente por tarefa via LLM, mas
  não há delegação interagente em tempo de execução (um agente chamar outro
  durante o raciocínio). O campo `Agent.AllowDelegation` é "informativo nesta
  versão" (sem efeito prático).
- **Sem streaming** — `LLM.Call` retorna a resposta completa; não há
  `CallStream(ctx, messages) (<-chan StreamChunk, error)`.
- **Memória apenas em processo** — sem persistência nem busca semântica.
- **Sem function calling nativo** — todo uso de ferramentas passa pelo ReAct
  textual, que funciona com qualquer provedor mas não aproveita tools nativas.

## 6. Roadmap — próximos passos (fora do escopo atual)

Recursos avançados do CrewAI original ainda **não** portados, por prioridade
sugerida (maior impacto / menor esforço primeiro):

### P0 — Publicação e fundamentos

- [ ] **`git init` + repositório público** — versionar, definir CI (lint, test,
  build de exemplos), tag `v0.1.0`.
  - [x] **`git init` + push** — feito em `https://github.com/rhgs/crewai-go.git`.
  - [x] **Module path corrigido** — `github.com/rodolphosa/crewai-go` →
    `github.com/rhgs/crewai-go` em `go.mod` + 27 arquivos (imports, docs,
    exemplos). Publicação consistente com o remote.
  - [x] **G306 (gosec) corrigido** — `Task.setOutput` agora grava com `0600`.
  - [ ] Definir CI (GitHub Actions: lint, `go vet`, `go test`, build de exemplos).
  - [ ] Tag `v0.1.0`.
  - [ ] **Upgrade toolchain para Go 1.24.9+** — govulncheck reporta 21 CVEs na
    stdlib do Go 1.24.4 (crypto/x509 etc.), corrigidos em 1.24.9. Sem mudança de
    código, só bumpar `go.mod`/CI.
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

### govulncheck — 21 CVEs na stdlib (Go 1.24.4)

Todas em `crypto/x509` e afins, via caminhos TLS dos provedores (`ollama.NewCloud`
→ `x509.ParseCertificate`, etc.). **Corrigidas em Go 1.24.9.** Não há vuln no
código do projeto; basta bumpar a toolchain no `go.mod` e no CI.

### Code review manual — pontos de atenção

- **`Crew.Kickoff` muta `Task.Description`/`ExpectedOutput` em `interpolate`
  sem segurar `Task.mu`.** Seguro no uso normal (Kickoff é chamado uma vez, fluxo
  single-threaded), mas **não é seguro para reusar a mesma `Crew` em goroutines
  concorrentes**. Documentar ou proteger com lock se o P1 (async) for
  implementado.
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
- [ ] **Function calling nativo** por provedor (OpenAI/Anthropic) como via
  alternativa ao ReAct textual — mantém o ReAct como fallback universal.
- [ ] **Async/paralelismo** de tarefas (`async_execution`) com `sync.WaitGroup`
  / `errgroup`-like usando goroutines e canais.
- [ ] **Delegação real** entre agentes — ferramenta "Delegate work to coworker"
  que permite a um agente invocar outro durante o raciocínio (remove a
  inércia do `AllowDelegation`).

### P2 — Persistência e observabilidade

- [ ] **Memória de longo prazo** com embeddings/busca semântica e persistência
  (interface `MemoryStore` + implementação em arquivo/sqlite como padrão).
- [ ] **Callbacks e telemetria** — hooks de início/fim de tarefa e iteração
  ReAct, exportáveis (log estruturado, métricas).
- [ ] **Guardrails** — validação/transformação de saída com retry.

### P3 — Avançado

- [ ] **Flows** (orquestração event-driven com estado e roteamento).
- [ ] Mais ferramentas (busca web, HTTP, arquivos, RAG).
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
