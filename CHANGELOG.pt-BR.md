# Changelog

Todas as mudancas relevantes no **crewai-go** sao documentadas aqui. Este projeto
segue o [Versionamento Semantico](https://semver.org/lang/pt-BR/).

## [Unreleased]

### Adicionado

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
