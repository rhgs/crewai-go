# Changelog

Todas as mudancas relevantes no **crewai-go** sao documentadas aqui. Este projeto
segue o [Versionamento Semantico](https://semver.org/lang/pt-BR/).

## [Unreleased]

### Adicionado

- **`format: time` (O-J2 → D-JT1)**: formato `time` do JSON Schema validado
  como full-time RFC 3339 (`HH:MM:SS[.fff][Z|±hh:mm]`); docs da allowlist
  atualizados.

### Documentação

- **Backlog adiado** — ack de produto 2026-08-21 (A1–A5): M5 → D-MT1–D-MT5,
  D-S10 → D-ST1–D-ST4, D-J9 → D-JE1–D-JE3 fechados (sem agenda); A5 permanece
  adiado; P-XAI-OAUTH segue externo. Ver `Plan/DECISIONS.pt-BR.md` §7A.
- **Plano de design P3**: `Plan/PLAN.p3-flows-tools-yaml-training.md` (+
  PT) — Flows, tools embutidas, YAML subconjunto JSON, training/export de
  traces; gates de code/security review, cobertura ≥90% e revisão de docs
  por trem; linkado no P3 do roadmap (ainda não implementado).
- **Plano de backlog adiado**: `Plan/PLAN.deferred-backlog.md` (+ PT) —
  seis itens abertos/adiados (xAI OAuth, M5, A5, D-S10, D-J9, O-J2) com
  condições de desbloqueio, espaço de design e IDs pré-alocados.

## [v0.8.0] — 2026-08-21

### Adicionado

- **Eventos de lifecycle (P2)**: `CrewEvent` + `Crew.WithEvents` /
  `ContextWithEvents`, `KickoffID`, dual-emit com Progress, `llm_call_*`,
  `react_iteration`, `structured_repair`, `loop_phase`, `guardrail_blocked`,
  `wave_*`. Helpers `ProgressAsEvents`, `EventLogger`. Exemplo
  `examples/events`. Design: `Plan/PLAN.p2-callbacks-schema.md` Parte C
  (D-C1–D-C10). PR #39.
- **Remainder de JSON Schema (P2)**: `$ref` local (`#/$defs`, `#/definitions`)
  com caps; allowlist de `format`; `const`, `not`, `if`/`then`/`else`,
  `minProperties`/`maxProperties`, `uniqueItems`; schemas booleanos.
  StrictSchema ainda rejeita `unevaluated*`. Design Parte J (D-J1–D-J12).
  PR #39.

### Documentação

- **Registro de decisões**: `Plan/DECISIONS.md` (+ PT) vivo cataloga IDs
  fechados/abertos com opções e escolhas. PR #42.
- **Guia de concorrência**: `docs/concurrency.md` (+ PT) — data races vs
  semantic races, barreira/fold, Memory D-M7. PRs #40, #41.
- **Docs P2**: crews `WithEvents`, tabela de keywords em tasks, SECURITY,
  examples; What's new do README → v0.8.0.

## [v0.7.0] — 2026-08-21

### Adicionado

- **Streaming de tokens do LLM (P1)**: `StreamingLLM` opcional (`CallStream` →
  `<-chan StreamChunk`), `Crew.WithStream` / `ContextWithStream`,
  `CollectStream` / `CallOrStream` públicos. Executor streama caminhos
  ReAct/native **sem tools**; demux Task/Agent nos chunks (D-S13); teto de
  bytes do body via `MaxProviderResponseBytes`; providers mock +
  openai/ollama/anthropic/xai. Exemplo `examples/streaming`. Design:
  `Plan/PLAN.streaming.md` (D-S1–D-S14). PRs #35, #36.

### Documentação

- **What's new do README**: atualizado para **v0.7.0** (EN + PT); Why/
  conceitos/comparação incluem Streaming; exemplo `streaming` listado.
- **Guias de Streaming**: `docs/llms.md` / crews (+ PT), nota SECURITY de
  stream sinks, maturidade do PLAN em v0.7.0; higiene pós-merge
  (`drainToSink` em cancel, notas de timeout HTTP).
- **Polimento de docs pós-v0.6** carregado: surface memory/async no README,
  link Memory no getting-started, listas de examples, suporte SECURITY.

## [v0.6.0] — 2026-08-21

### Alterado

- **Pin de toolchain Go**: `go.mod` declara `toolchain go1.24.9` (linguagem
  permanece `go 1.24`) para preferir stdlib com correções de CVE conhecidas;
  CI segue na linha `1.24.x`.

### Adicionado

- **Contrato `MemoryStore` de memória de longo prazo (M1)**: novos tipos
  `MemoryEntry`, `MemoryStore` e `MemoryQuery` com tetos de entrada/consulta
  (`MaxMemoryEntryBytes`, `MaxMemoryQueryLimit`, `DefaultMemoryQueryLimit`,
  `DefaultMemoryMaxChars`). O `*Memory` embutido agora implementa
  `MemoryStore`: `Put` mapeia para `Save` atribuindo `ID` e `CreatedAt`,
  `Query` busca em `Content`/`Task` (sem maiúsculas; `Text` vazio devolve os
  mais recentes primeiro, limitado por `Limit`/`MaxChars`), `Delete` é
  idempotente e `Close` é no-op. Entradas são particionadas por
  `MemoryScope`. O wiring no Crew chegou no M2; FileStore no M3. Sem mudança
  no caminho de curto prazo de `Memory bool` além da ponte de store.

- **`examples/mcp`**: demo de wiring offline para MCP `FilterTools` /
  `WithDescriptionLimit` / `NewToolAdapter` (`MCP_ENDPOINT` opcional ao vivo).

### Adicionado (async além de Staged: A2/A3)

- **`Task.Async` + agendador de waves**: marcar uma tarefa com `WithAsync()`
  (ou `Async = true`) permite que tarefas independentes rodem em concorrência
  sob `Sequential` e `Hierarchical`. O agendamento é por waves:
  `Task.Context` define a DAG, `planWaves` coloca cada tarefa na primeira
  wave estritamente após suas dependências, e a agregação agrega cada wave
  por índice de declaração após a barreira (mesmo contrato do Staged). Ciclo,
  auto-dependência ou ponteiro de tarefa duplicado falham no Kickoff com a
  nova sentinela `ErrTaskDependencyCycle` (G12). No Hierarchical, agentes são
  pré-resolvidos em série antes das waves (D-A2). Em `Staged` o flag é
  ignorado com um Warn único (D-A5/G5).

- **`Crew.AsyncMaxWorkers` / `Crew.AsyncFailFast`**: a concorrência da wave é
  limitada por `AsyncMaxWorkers`; `NewCrew` usa `DefaultAsyncMaxWorkers`
  (**8**) e `AsyncFailFast = true`. Use `WithAsyncMaxWorkers(n)` para
  sobrescrever, ou **0 para ilimitado** (opt-out explícito — limitar o
  fan-out de LLM passa a ser sua responsabilidade). Com `AsyncFailFast =
  false`, ramos independentes continuam após uma falha e apenas os
  dependentes da tarefa que falhou são ignorados com erro (D-A3); o padrão
  `true` cancela os irmãos e aborta o Kickoff. Ambos são ignorados em
  `Staged`.


- **`MemoryPolicy` + barreira de commit D-M7 (M2)**: `Crew.MemoryStore` e
  `Crew.MemoryPolicy` ligam memória de longo prazo ao Kickoff. `Memory=true`
  permanece o alias permanente v0.x que garante store InMemory quando nenhum
  store externo é definido (D-M1/G4). AutoSave só em sucesso (G2); inject usa
  `queryCommitted` sobre o snapshot commitado (latest N, orçado por
  `DefaultLimit`/`DefaultMaxChars`). Em waves/stages paralelas, AutoSave é
  bufferizado por tarefa e commitado na barreira em **ordem de declaração**
  (D-M7/G9) — o inject da próxima wave não vê writes de irmãos em voo.
  Buffers de falha/cancelamento são descartados. Erros de AutoSave geram
  warn+capture e não abortam o Kickoff (G11). `Scope` padrão = `Crew.Name`
  quando definido (G3). Use `NewMemoryPolicy()` para os defaults da lib (um
  `MemoryPolicy{}` zero não é esses defaults).

- **Correções A3**: `AsyncMaxWorkers` agora limita de fato os workers em
  flight nas waves async (semáforo). Waves mistas rodam as non-Async depois
  do subset Async (antes eram dropadas). `AsyncFailFast=false` ignora só
  dependentes da tarefa que falhou (D-A3). `0` continua ilimitado por design;
  `NewCrew` ainda define 8.

### Adicionado (embeddings: M4)

- **`EmbeddingFunc` + Query por cosseno (M4)**: `Crew.Embed` aceita um
  embedder da app. Com `MemoryPolicy.AutoEmbed = true`, cada entrada de
  AutoSave é embedada **em série na barreira de commit** (G8) antes do Put —
  nunca dentro dos workers paralelos. Stores embutidos (*Memory, FileStore)
  ranqueiam `MemoryQuery.Embedding` por similaridade de cosseno (stdlib);
  `Text` substring é ignorado no caminho semântico. Dim mismatch / norma
  zero pontuam 0; matches positivos cortam linhas zero; se nada estiver
  embedado, Query cai no latest-N. Erros de AutoEmbed são soft
  (warn+capture, entrada ainda salva). Exemplo: `examples/memory_embed`
  (mock bag-of-words offline).

### Adicionado (FileStore: M3)

- **Backend `FileStore` JSONL**: `OpenFileStore(dir)` abre um `MemoryStore`
  durável só-stdlib sob um root confiável pelo caller (`filestore.go`,
  D-M2=A). Layout `{root}/scopes/{urlsafeScope}/{entries.jsonl,meta.json}`;
  arquivos `0600`, dirs `0700`. Put faz append + índice em RAM; Delete grava
  tombstone; Query segue a semântica in-memory (latest-N, Limit/MaxChars,
  Scope). Linhas JSONL corrompidas são ignoradas no Open e contadas em meta /
  `CorruptSkipped()` (D-M5). v1 é single-writer por root (G7); a app é dona
  do `Close` (D-M6). Root vazio/branco retorna `ErrFileStoreRoot`; uso após
  Close retorna `ErrFileStoreClosed`. Exemplo: `examples/memory_file`.

### Alterado

- **Interno**: o runtime paralelo staged foi extraído para a primitiva
  compartilhada `runTaskGroup` (A1), que `runStaged` agora chama por stage.
  A barreira (join) é o único ponto onde resultados são agregados, sempre
  indexados pela posição da tarefa (ordem de declaração), nunca por ordem de
  conclusão. Sem mudança de comportamento público no processo staged — é a
  base que o agendador de waves async (A2/A3) e a barreira de commit de
  memória (M2, D-M7) usam.

### Documentação

- **Docs de usuário para memory + async**: `docs/memory.md`, `docs/crews.md`,
  `docs/tasks.md` (+ espelhos PT) cobrem `MemoryStore` / `MemoryPolicy` /
  D-M7, `FileStore`, embeddings e waves `Task.Async`. Godoc do pacote
  (`doc.go`) atualizado. Exemplos offline `async_tasks`, `memory_file`,
  `memory_embed`.
- **Sync do Plan**: snapshot de maturidade e itens do roadmap marcados como
  shipped na v0.6.0 (PR #31).

## [v0.5.0] — 2026-08-20

### Seguranca

- **Single-flight do Kickoff**: `Kickoff` concorrente no mesmo `*Crew`
  retorna `ErrCrewRunning` (fail fast; nao enfileira). Reuso sequencial
  continua suportado.

- **Jail de path do OutputFile**: paths de `Task.OutputFile` sao limpos;
  paths vazios sao rejeitados. `Task.OutputDir` / `Crew.OutputDir`
  opcionais prendem writes com avaliacao de symlink (`EvalSymlinks`,
  fail closed) e sentinela `ErrOutputPathRejected`. Modo permanece `0600`.

- **Timeout HTTP padrao do MCP**: `mcp.New` nao usa mais
  `http.DefaultClient`. Constroi um client com
  `DefaultHTTPTimeout` (30s). Configure via `WithHTTPTimeout`,
  `WithHTTPClient` (como esta, incl. `Timeout: 0`), ou JSON
  `servers[].timeout` (duration string Go; omitido → 30s; `"0s"` →
  desligado). Durations JSON invalidas falham o `LoadConfig` antes de
  qualquer chamada de rede.

- **Corpos de resposta dos provedores**: `Call` em OpenAI, Anthropic e
  Ollama agora limita o body a `MaxProviderResponseBytes` (antes so
  `CallWithTools` / `WebSearch` faziam isso). Erros nao-2xx nao ecoam
  mais o body da resposta (provedores podem reexibir credenciais).
- **Round-trip de native tools**: `Message.ToolCallID` e populado pelo
  executor e mapeado para `tool_call_id` (OpenAI) e `tool_use_id`
  (Anthropic). Definicoes de tools do Anthropic sao enviadas no formato
  flat `{name, description, input_schema}` (nao no shape aninhado da
  OpenAI), e turnos assistant/tool sao reenviados como content blocks
  tipados para o loop multi-turno funcionar de ponta a ponta.
- **Cap de observacao ReAct**: outputs de tools no loop ReAct em texto
  sao truncados com o mesmo limite `MaxToolOutputBytes` do native tool
  calling.
- **Endurecimento SSRF** (`tools.WebSearchTool`): tambem bloqueia
  userinfo, multicast, CGNAT (`100.64.0.0/10`), hosts vazios e aliases
  de metadata (`metadata`, `metadata.google.internal`).
- **Erros de provedores de busca**: Google, Brave, LangSearch e
  Serpstack nao ecoam mais bodies de erro / URLs de request que
  poderiam vazar API keys em logs.
- **Caminhos OAuth xAI**: `SaveToken` / `LoadToken` expandem um
  `~/...` inicial para o home do usuario (antes o til era tratado
  literalmente).

### Adicionado

- **Tool de delegacao entre agents**: `NewDelegationTool(roster)` expoe
  `delegate_to_coworker` (JSON `coworker`/`request`/`context`). Alvos devem
  ter `AllowDelegation`. Chamadas aninhadas respeitam
  `DefaultMaxDelegationDepth` (2) com guardas de ciclo/auto.
  `Crew.EnableDelegationTool` (default false) anexa a tool no Kickoff;
  `Crew` implementa `DelegationRoster` via `PeerAgents()`.

- **Keywords JSON Schema (expandidas)**: o validador agora suporta
  `additionalProperties`, `minLength`/`maxLength` (bytes),
  `minimum`/`maximum`/`exclusiveMinimum`/`exclusiveMaximum`,
  `minItems`/`maxItems`, `pattern` e `oneOf`/`anyOf`/`allOf`.
  `WithStrictSchema()` falha cedo em keywords nao suportadas (`$ref`, etc.).
- **`WithAllowTools`**: fase gather opcional antes do capture structured;
  preserva facts de `FactSource`; esgotar o budget de gather registra
  warning e ainda captura JSON (D6).

- **Guards de catalogo MCP**: `mcp.FilterTools` (allowlist por nome,
  deny-by-default ao filtrar) e `mcp.WithDescriptionLimit` em
  `NewToolAdapter` (strip de controles ASCII + truncar em N runes).
  Defaults inalterados sem options.

- **`RedactHandler`**: wrapper opt-in de `slog.Handler` no pacote raiz
  que aplica as regras existentes de redacao de segredos a mensagens e
  atributos string. O logger default nao muda.
  `examples/logging` agora usa `crewai.RedactHandler`.

- **Suporte a MCP** (`mcp/`): cliente padrao Go (somente stdlib) para o
  Model Context Protocol sobre Streamable HTTP (JSON-RPC 2.0, versao de
  protocolo 2025-06-18). Dois modos de configuracao, ambos informados
  programaticamente pelo chamador:
  - **Programatica**: `mcp.New(endpoint, opts...)` + `client.Initialize(ctx, name, version)`.
  - **Arquivo JSON**: `mcp.LoadConfig(ctx, path, name, version)` le um
    arquivo com uma ou mais entradas de servidor (Name, Endpoint, Headers, Timeout opcional),
    cria um cliente para cada, chama `Initialize` e retorna o slice.
  Cada ferramenta MCP e exposta como `crewai.Tool` via `mcp.NewToolAdapter`.
  O adapter implementa `crewai.SchemaProvider`, entao o `inputSchema`
  original e encaminhado ao native function calling em vez de ser
  substituido por um placeholder. Um `isError: true` no nivel da
  ferramenta e retornado ao modelo como texto de observacao (prefixado
  com `[tool error]`), nao como erro de Go — apenas falhas HTTP /
  JSON-RPC viram `error`. Novos helpers: `WithHTTPClient`, `WithHTTPTimeout`,
  `WithHeader`, `Client.Close` (teardown de sessao via HTTP DELETE; idempotente).
  `LoadConfig` fecha os clients ja inicializados se um servidor
  posterior falha no `Initialize`. Nova constante
  `MaxMCPResponseBytes = 16 MiB`. Sem novas dependencias;
  totalmente backward compatible.

- **Interface `SchemaProvider`**: uma capacidade opcional, verificada via
  type assertion, que um `Tool` pode implementar para expor seu JSON
  Schema. O `toToolSpecs` do executor consulta `SchemaProvider` antes de
  cair no schema de objeto vazio padrao — usado pelo adapter MCP e
  disponivel a qualquer ferramenta que queira encaminhar seu schema real
  ao modelo. Puramente aditivo.

- **Warnings por tarefa (degradacao graciosa)**: novos metodos thread-safe
  `Task.AddWarning(msg)` e `Task.Warnings()`, a interface opcional
  `WarningSink` recuperavel do `context.Context` via `warningSinkFromCtx`,
  e o helper `AddWarningFromCtx(ctx, msg)`. O executor (`Crew.execute`)
  injeta a tarefa como `WarningSink` no `ctx` antes de invocar as
  ferramentas, permitindo que uma ferramenta registre um diagnostico nao
  fatal enquanto a tarefa sucede. Os warnings sao agregados em
  `TaskOutput.Warnings` e `CrewOutput.Warnings` em ordem de execucao.
  `Agent.Execute` standalone nao injeta sink — `AddWarningFromCtx` ai e
  um no-op silencioso. Distinto de `Stage.Optional`: warnings significam
  *sucesso parcial dentro da tarefa*; estagios opcionais significam
  *falha da tarefa nao aborta o crew*. Sem novas dependencias;
  backward compatible.

- **Observabilidade de progresso**: novos tipos `crewai.ProgressFunc` e
  `crewai.Progress` mais o setter fluente `Crew.WithProgress(fn)`.
  Durante `Kickoff`, o executor emite eventos `stage_started`,
  `stage_completed`, `task_started`, `task_completed` e `tool_invoked`
  (caminhos ReAct e native). O callback e invocado de multiplas
  goroutines quando estagios rodam em paralelo — deve ser thread-safe,
  como um `slog.Handler`. Panics dentro do callback sao recuperados e
  logados via `slog.Default()`; `Kickoff` nunca e abortado por falha
  no callback. Payloads de `Progress` nunca contem corpo de prompt,
  saida do LLM nem input de ferramenta — apenas metadados. Sem novas
  dependencias; backward compatible.

- **Saida estruturada via tool-call (`WithToolCall`)**: novo modo em
  `StructuredOutput` que extrai JSON via uma chamada de tool
  sintetica em vez de prompt de texto JSON puro. Util para provedores
  que nao suportam saidas estruturadas atraves do parametro `format`
  (notavelmente Ollama Cloud): o executor declara um `ToolSpec` de
  nome `emit_result` com `Parameters = schema`, pede ao modelo que
  chame exatamente uma vez e interpreta o campo `arguments` da
  chamada (ja em `json.RawMessage` em toda `ToolCallingLLM` desta
  repo) como saida validada. Se o modelo retornar texto livre ou os
  argumentos falharem na validacao, o loop de reparo existente
  (`RepairMax`) e acionado. Se a LLM nao implementa `ToolCallingLLM`,
  retorna `ErrToolCallStructuredUnsupported`. O modo JSON-only legado
  segue como padrao e e backward compatible. Ative com
  `crewai.WithToolCall()`.

### Modificado

- O hook de pre-commit local (`scripts/pre-commit`) agora roda
  `govulncheck ./...` no Go 1.25.x alem do `gofmt`. O modulo em si
  nao tem vulnerabilidades conhecidas. Instale com
  `ln -sf ../../scripts/pre-commit .git/hooks/pre-commit`.

## [v0.4.0] — 2026-08-17

### Adicionado

- **Processo em estagios (Staged)** (`process.go`, `crew.go`): um terceiro modo
  de orquestracao (`Staged`) que agrupa tarefas em estagios. Os estagios rodam
  em sequencia, enquanto as tarefas dentro de um mesmo estagio rodam
  concorrentemente. A saida de cada estagio alimenta os seguintes via
  `Task.Context`. Um estagio marcado como `Optional` nao aborta o crew quando
  uma de suas tarefas falha. Novo tipo `Stage` e sentinela `ErrNoStages`.

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
  processo, sem function calling nativo. Veja `Plan/PLAN.md` para o roadmap completo.

[Unreleased]: https://github.com/rhgs/crewai-go/compare/v0.8.0...HEAD
[v0.8.0]: https://github.com/rhgs/crewai-go/compare/v0.7.0...v0.8.0
[v0.7.0]: https://github.com/rhgs/crewai-go/compare/v0.6.0...v0.7.0
[v0.6.0]: https://github.com/rhgs/crewai-go/compare/v0.5.0...v0.6.0
[v0.5.0]: https://github.com/rhgs/crewai-go/compare/v0.4.0...v0.5.0
[v0.4.0]: https://github.com/rhgs/crewai-go/compare/v0.3.0...v0.4.0
[v0.3.0]: https://github.com/rhgs/crewai-go/compare/v0.2.0...v0.3.0
[v0.2.0]: https://github.com/rhgs/crewai-go/compare/v0.1.0...v0.2.0
[v0.1.0]: https://github.com/rhgs/crewai-go/releases/tag/v0.1.0
