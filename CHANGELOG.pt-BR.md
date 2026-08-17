# Changelog

Todas as mudancas relevantes no **crewai-go** sao documentadas aqui. Este projeto
segue o [Versionamento Semantico](https://semver.org/lang/pt-BR/).

## [Nao liberado]

### Adicionado

- **Agentic loop** (`loop.go`): uma estrategia de execucao opcional
  Planejar-Executar-Avaliar-Refinar (`AgenticLoop`) que substitui o executor
  ReAct de passagem unica. Defina `Agent.Loop` ou `Task.Loop` para ativa-lo.
  Recursos:
  - Fase de planejamento (omitida quando o agente nao tem ferramentas, ou via
    `WithSkipPlan`).
  - Fase de avaliacao com limiar de aprovacao configuravel (`WithPassThreshold`)
    e um agente avaliador independente opcional (`WithEvaluator`).
  - Refinamento ate `MaxRefinements` rodadas, com `WithRefineRewriteOnly` para
    reescrever sem reexecutar ferramentas.
  - Novas sentinelas de erro `ErrEvaluationFailed` e `ErrInvalidEvaluation`.

## [v0.3.0] — 2026-08-17

### Modificado

- **Logger trocado por `log/slog`**: a interface custom `Logger`
  (`Infof`/`Debugf` no estilo format-string) baseada em `log.Logger` foi
  removida em favor de `*slog.Logger` da biblioteca padrão. Todas as
  funções executoras (`executeTask`, `executeStructured`,
  `executeTaskWithTools`) agora recebem `*slog.Logger` e emitem logs
  estruturados em pares chave-valor via `InfoContext`/`DebugContext`/
  `WarnContext`. Novos métodos fluentes `Crew.WithLogger(*slog.Logger) *Crew`
  e `Agent.WithLogger(*slog.Logger) *Agent` permitem injetar qualquer
  `*slog.Logger`. Retrocompatível: `Crew.Verbose` continua funcionando —
  agora controla o nível do logger fallback (`Verbose=true` → `LevelDebug`,
  `Verbose=false` → `LevelError`, refletendo o comportamento legado de
  "silencioso quando off"). `Agent.Execute` standalone usa `slog.Default()`
  como fallback. Subpacotes (`llm/*`, `tools/*`) continuam sem logging.
  `logger.go` removido. Sem novas dependências; apenas `log/slog` da stdlib.

### Adicionado

- **Interface WebSearcher e helper SearchWeb**: nova interface opcional
  `WebSearcher` (implementa `LLM`) permite que provedores com API de busca
  nativa sejam chamados diretamente do Go. Helper `crewai.SearchWeb(ctx, llm,
  query, max)` evita _type assertion_ manual. Tipo `SearchHit` com `Title`,
  `URL` e `Content`. Sentinela `ErrWebSearchUnsupported`.
- **WebSearchTool com 7 provedores de busca**: `tools.WebSearchTool` implementa
  `crewai.Tool` e `crewai.FactSource` para uso no loop ReAct. Provedores:
  Wikipedia (padrão, grátis), LangSearch (100% grátis), Serpstack (1000/mês
  grátis), DuckDuckGo (opcional, pode ser bloqueado), Google (API key + CSE ID),
  Brave (API key). Resultados coletados como `Fact`s com proveniência. Opções
  `WithMaxResults` e `WithSearchTimeout`.
- **Suporte a web search para Ollama, OpenAI, Anthropic, xAI**: Ollama via
  `POST /api/web_search` (busca pura, sem invocar modelo); OpenAI via
  `web_search_options` (não `tools`) com modelos de busca (`gpt-4o-search-preview`,
  `gpt-5-search-api`); Anthropic via ferramenta `web_search_20250305` (server tool)
  com resposta `web_search_tool_result`; xAI delega para cliente OpenAI compatível.

### Segurança

- **Proteção SSRF com prevenção de DNS rebinding**: `WebSearchTool` filtra todas
  as URLs de resultados com `isBlockedURL` — bloqueia esquemas não-http(s), IPs
  privados/loopback/link-local, resolve nomes de domínio via DNS para prevenir
  _DNS rebinding_, e adota _fail-closed_ (hosts não resolvidos são bloqueados).

### Segurança (logger → slog)

- **Redação de segredos em logs** (`redact.go`): erros de providers logados
  via `logger.WarnContext` (notavelmente o caminho de delegação hierárquica)
  passam agora por `redactError`, que mascara segredos prováveis na mensagem:
  - Tokens alfanuméricos longos (≥20 chars), preservando 4 caracteres
    iniciais e 4 finais quando o token tiver ≥24 chars.
  - `Bearer <token>` em mensagens estilo HTTP.
  - Valores de query-string `api_key=`, `token=`, `key=`, `secret=`.

  Veja `redact.go` e `redact_test.go` para as regras exatas. Exemplo
  mostrando o mesmo padrão de redação no nível do handler: `examples/logging/`.

- **Documentos de segurança de logging**: README + doc.go agora alertam que
  logs em nível Debug contêm a saída completa do LLM e inputs de
  ferramentas, e que erros de providers podem incluir API keys na mensagem.
  Recomenda-se envolver handlers com um redator.

- **`WithLogger` não é concorrente-safe**: documentado em `Crew.logger`,
  `Agent.logger`, `Crew.WithLogger`, e `Agent.WithLogger`. Múltiplas
  chamadas sequenciais são idempotentes (última vence), testado por
  `TestWithLogger_Idempotent_Crew` e `TestWithLogger_Idempotent_Agent`.

### Adicionado (native tool calling)

- **Native tool calling**: `Agent.ToolMode` (`"react"` | `"native"`) seleciona
  entre o loop ReAct baseado em texto existente e a API de function calling
  nativa do provedor. Nova interface `ToolCallingLLM` (implementa `LLM`),
  tipos `ToolSpec`, `ToolCall`, `ToolCallResponse`, `ToolTrace`.
  `CallWithTools` implementado para Ollama, OpenAI, Anthropic e Mock.
  `ToolTraces` em `TaskOutput` para observabilidade. Sentinela
  `ErrNativeToolsUnsupported`. Seguranca: validacao de argumentos (limites de
  tamanho/profundidade), truncamento de output de tools, limites de tamanho de
  resposta de provedores (`io.LimitReader`). Backward compatible: padrao e
  ReAct, sem mudancas em caminhos de codigo existentes.

## [v0.2.0] — 2026-08-07

### Adicionado

- **Facts e proveniencia**: tipo `Fact` de primeira classe, populado apenas
  por tools `FactSource`, nunca pelo LLM. Facts carregam organizacao fonte,
  URL fonte, momento de coleta e hash do payload (SHA-256). Construtor
  `NewFactSourceTool`, helper `AllFactsProvenanced` para guardrails,
  `dedupFacts` por PayloadHash. `CrewOutput.Facts` e `TaskOutput.Facts`.
  `executeTask` retorna `(string, []Fact, error)` internamente. Sem novas
  dependencias; backward compatible.
- **Guardrails**: guardrails de crew (`Crew.Guardrails`) e de task
  (`Task.Guardrail`) — hooks de validacao pos-saida que bloqueiam a
  publicacao de saidas que violam invariantes de negocio. Nova sentinela
  `ErrBlockedByGuardrail`. Opcoes funcionais `WithGuardrails` (crew) e
  `WithGuardrail` (task). Complementa a validacao de schema da saida
  estruturada (forma vs. significado). Sem novas dependencias; backward
  compatible.
- **Saida estruturada**: `Task.Structured` (`*StructuredOutput`) exige que o
  modelo produza JSON validado contra um JSON Schema, com um loop de reparo
  limitado (`RepairMax`, padrao 2). Novas sentinelas `ErrInvalidOutput` e
  `ErrRepairBudgetExceeded`. Validador de schema minimalista embutido (type,
  properties, required, enum, items) — apenas stdlib, sem novas dependencias.
  A interface `LLM.Call` nao foi alterada; a saida estruturada funciona via
  engenharia de prompt e validacao em Go. Construtor `NewStructuredOutput` e
  opcao funcional `WithRepairMax`.

## [v0.1.0] — 2026-08-04

Primeira release pública: um port idiomático do núcleo do framework CrewAI para Go.

### Adicionado

- **Orquestração central**: `Agent`, `Task`, `Crew`, `Process`, `Tool`, `Memory`
  e um executor **ReAct** baseado em texto.
- **Processos**: `Sequential` (padrão) e `Hierarchical` (delegação gerenciada por
  `ManagerLLM` / `ManagerAgent`).
- **Contexto e interpolação**: encadeie saídas de tarefas com `WithContext`;
  injete variáveis `{chave}` via `Crew.Kickoff`.
- **Provedores de LLM** (apenas stdlib, sem dependências externas):
  - OpenAI e endpoints compatíveis (Groq, Azure, Ollama `/v1`, …) — `llm/openai`.
  - Anthropic (Claude) — `llm/anthropic`.
  - Ollama local + Ollama Cloud — `llm/ollama`.
  - xAI (Grok) via chave de API **ou** OAuth de assinatura (Device Flow RFC 8628 +
    PKCE + refresh + persistência) — `llm/xai`.
  - Mock determinístico para testes — `llm/mock`.
- **Ferramentas embutidas**: `Calculator` (parser recursivo descendente seguro),
  `CurrentTime`, `WordCount` — pacote `tools`.
- **Memória**: memória em processo segura para concorrência com busca por substring.
- **Docs e exemplos**: README em inglês + 7 guias; espelhos em português
  (`docs/pt-BR/`); 7 exemplos executáveis; testes herméticos (~90% de cobertura no núcleo).

### Segurança

- Nenhum segredo commitado; `.gitignore` protege `.claude/`, `.env`, `*token.json`.
- Token OAuth do xAI persistido com permissão `0600`.

### Notas

- Licença: MIT.
- Limitações conhecidas desta versão: delegação hierárquica simplificada (sem
  chamadas entre agentes em tempo de execução), sem streaming, memória apenas em
  processo, sem function calling nativo. Veja `PLAN.md` para o roadmap completo.
