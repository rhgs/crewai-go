# Plano — Streaming de tokens do LLM

> **Status:** **Entregue na v0.7.0** (PRs #35/#36).  
> **Decisões:** D-S1–D-S14 fechadas na review de design 2026-08-21 (§9).  
> **Relacionado:** `llm.go` (`LLM`), `toolcall.go` (padrão `ToolCallingLLM`), `executor.go` / `structured.go` / `loop.go`, providers em `llm/*`, roadmap `PLAN.pt-BR.md` §6 P1.  
> **Restrições:** zero dependências externas de módulo no core (`go.mod` permanece só-stdlib). Gates de qualidade do §6.1 de `PLAN.security-residuals.md` valem para cada PR (cobertura ≥ 90% nos pacotes tocados, race-clean, docs EN+PT-BR, CHANGELOG).  
> **Fora de escopo de epics irmãos:** este plano **não** inclui callbacks/telemetria além da entrega do stream (P2), resto de JSON Schema (P2), Flows (P3), nem M5/A5 do memory-async.

---

## 0. Por que streaming agora

| Capacidade | Hoje (v0.6.0) | Lacuna |
|---|---|---|
| **I/O do LLM** | `LLM.Call(ctx, messages) (string, error)` — completion inteira bufferizada | Respostas longas “congelam”; UIs não mostram tokens à medida que chegam |
| **Observabilidade** | `WithProgress` só metadados (sem corpos de prompt/output) | Apps que precisam de texto ao vivo reimplementam SSE do provider |
| **Providers** | OpenAI/Anthropic/Ollama/xAI todos fazem `io.ReadAll` | Os protocolos já suportam stream (`stream: true`, SSE, NDJSON) |

Streaming é o último item **P1** do roadmap. É independente de memory/async (já shippados), mas beneficia as mesmas UIs que usam `WithProgress` para o lifecycle da task.

**Não-objetivos (este plano):**
- Substituir `LLM.Call` ou quebrar implementadores existentes.
- Stream de **JSON parcial de tool-call nativo** na v1 (difícil entre providers; ver D-S4).
- Transformar `Progress` em barramento de tokens (corpos ficam fora de Progress — D-S5).
- OpenTelemetry / barramento completo de telemetria (epic P2 de callbacks).
- Framework de merge de tokens multi-agente.
- Validar structured-output com JSON incompleto no meio do stream.

---

## 1. Baseline (verdade do código em v0.6.0)

### 1.1 Interfaces do core

```go
// llm.go
type LLM interface {
    Call(ctx context.Context, messages []Message) (string, error)
    Model() string
}

// toolcall.go — capacidade opcional (precedente para StreamingLLM)
type ToolCallingLLM interface {
    CallWithTools(ctx context.Context, messages []Message, tools []ToolSpec) (*ToolCallResponse, error)
}
```

`LLM` permanece mínimo. Capacidades opcionais usam **type assertion** no executor (`ToolCallingLLM`, `WebSearcher`, futuro `StreamingLLM`).

### 1.2 Onde `Call` é usado

| Caminho | Arquivo | Precisa da string completa antes de seguir? |
|---|---|---|
| Loop ReAct (com tools) | `executor.go` | **Sim** — parse de `Final Answer:` / `Action:` após cada turno |
| ReAct sem tools | `executor.go` | **Não** para UX — o resultado é a completion inteira |
| Tools nativos | `toolcall.go` | **Sim** para montar tool_calls; turno final de texto poderia streamar |
| Captura structured | `structured.go` | **Sim** — validar JSON Schema exige objeto completo |
| AgenticLoop plan/eval/refine | `loop.go` | **Sim** para JSON de eval; execute segue executeTaskDefault |
| Escolha do manager hierárquico | `crew.go` `delegate` | **Sim** — parse do role; completion curta |

### 1.3 HTTP dos providers hoje

| Provider | Endpoint | Flag stream hoje | Framing natural |
|---|---|---|---|
| `llm/openai` | `POST /chat/completions` | ausente (`stream` default false) | SSE `data: {json}\n\n` + `[DONE]` |
| `llm/xai` | encapsula openai | igual | igual (compatível OpenAI) |
| `llm/anthropic` | `POST /messages` | ausente | SSE `event:` + `data:` (`content_block_delta`, etc.) |
| `llm/ollama` | `POST /api/chat` | **`Stream: false` hardcoded** | linhas NDJSON, `done: true` |
| `llm/mock` | in-process | n/a | chunk sintético de `Responses` |

Caminhos não-stream usam `io.ReadAll(io.LimitReader(body, MaxProviderResponseBytes))`. Caminhos stream devem honrar o **mesmo teto no texto acumulado e nos bytes crus lidos do body** (D-S8).

### 1.4 Contrato de Progress (não regredir)

`Progress` **nunca** carrega corpos de prompt, outputs de LLM ou inputs de tool — só metadados. Entrega de texto em streaming é um **canal separado** (D-S5).

---

## 2. Objetivos

1. **Streaming opt-in** para apps que querem deltas de token sem quebrar nenhum implementador de `LLM`.
2. **Uma API de consumo** no pacote raiz para UIs não dependerem de parsers SSE por provider.
3. **Integração no executor** que streama quando é seguro e **cai para `Call`** quando o caminho exige string completa ou o LLM não tem `StreamingLLM`.
4. **Cobertura de providers** first-party (OpenAI, xAI-via-openai, Ollama, Anthropic) + mock para testes herméticos.
5. **Cancelamento**: cancel de `ctx` aborta a leitura do body HTTP e fecha o canal de chunks.
6. **Zero novas dependências de módulo**; só stdlib (`bufio` / `encoding/json`).
7. **Backward compatible**: `Kickoff` default com LLMs não-stream permanece bit-idêntico (sem goroutines extras, sem callbacks obrigatórios).

---

## 3. Esboço da API pública

### 3.1 Chunk + interface opcional (pacote raiz)

```go
// StreamChunk é uma unidade de uma completion em streaming.
// v1 é centrada em texto: providers mapeiam deltas do wire só em Delta.
// Task/Agent são preenchidos pelo wrapper do executor (D-S13), nunca pelos providers.
type StreamChunk struct {
    // Delta é o fragmento incremental de texto (pode ser vazio em chunks de controle).
    // Providers DEVEM omitir deltas null/vazios do wire (sem spam de Delta vazio).
    Delta string

    // Task é o label da tarefa (Name ou "Task N"). Preenchido pelo executor
    // ao entregar ao sink da app para demux de waves Async concorrentes.
    // Vazio quando CallStream é usado fora de uma task (testes / CollectStream).
    Task string

    // Agent é o role do agente da task em voo. Mesmas regras de Task.
    Agent string

    // Done é true no chunk terminal de sucesso. Delta ainda pode trazer
    // um fragmento final (D-S2-A). Depois de um chunk Done o canal é fechado.
    // Done e Err NÃO DEVEM estar setados juntos (D-S14); CollectStream prioriza Err.
    Done bool

    // Err é non-nil no chunk terminal de falha. Delta fica vazio.
    // Depois de um chunk Err o canal é fechado. Callers também podem ver
    // o mesmo erro no retorno de CollectStream / helpers.
    Err error
}

// StreamingLLM é uma capacidade opcional (mesmo padrão de ToolCallingLLM:
// embute LLM para Model/Call ficarem disponíveis no valor assertado).
// O executor faz type-assert quando um sink de stream está configurado (D-S1, D-S3).
// Implementadores que só suportam Call continuam válidos para sempre.
type StreamingLLM interface {
    LLM
    // CallStream inicia uma completion e devolve um canal de chunks.
    // O canal NUNCA é nil (D-S14). Falhas de setup (marshal, auth, dial)
    // vêm como primeiro chunk {Err: ...} e o canal é fechado.
    // O canal é fechado após o chunk terminal Done ou Err, ou quando
    // ctx é cancelado (produtor sai; CollectStream mapeia close nu para
    // ctx.Err() ou ErrStreamIncomplete). Implementadores DEVEM:
    //   - respeitar cancelamento de ctx com prontidão;
    //   - não bloquear para sempre se o caller parar de receber (preferir ctx);
    //   - aplicar MaxProviderResponseBytes no texto acumulado E nos
    //     bytes crus do body HTTP lidos (D-S8); usar ErrStreamResponseTooLarge;
    //   - ser seguros para Call/CallStream concorrentes no mesmo client;
    //   - deixar Task/Agent vazios (o executor preenche).
    // O canal DEVE ter buffer pequeno (DefaultStreamChanBuffer = 16, D-S11)
    // ou ir pareado a um produtor interno que sai em ctx.Done.
    CallStream(ctx context.Context, messages []Message) <-chan StreamChunk
}

// Erros sentinela (errors.go):
//   ErrStreamIncomplete       — canal fechou sem Done/Err e ctx OK
//   ErrStreamResponseTooLarge — texto ou bytes do body excederam MaxProviderResponseBytes
```

**Checks em compile-time** em cada provider (mesmo estilo de `ToolCallingLLM`):

```go
var _ crewai.StreamingLLM = (*Client)(nil)
```

### 3.2 Helpers (pacote raiz)

```go
// CollectStream drena ch e devolve o texto concatenado dos Deltas.
// API pública estável (O-S3 fechado). Retorna o primeiro chunk.Err non-nil,
// ctx.Err() se ctx acabou quando o canal fecha sem chunk terminal,
// ErrStreamIncomplete se o canal fecha sem Done nem Err com ctx ainda OK,
// ou nil em Done limpo. Ignora Task/Agent na concatenação.
func CollectStream(ctx context.Context, ch <-chan StreamChunk) (string, error)

// CallOrStream prefere StreamingLLM quando sink != nil; senão Call.
// Sempre devolve o texto completo para o executor; sink recebe os deltas.
// Todas as invocações do sink passam por emitStream (recover de panic, D-S12).
func CallOrStream(ctx context.Context, llm LLM, messages []Message, sink StreamFunc) (string, error)

// StreamFunc é chamada para cada chunk entregue à app (deltas, Done, Err).
// DEVE ser segura para uso concorrente quando waves Async / stages Staged
// rodam em paralelo (mesmo contrato de ProgressFunc). Setar via WithStream
// ANTES do Kickoff. Panics são recuperados em emitStream (D-S12).
type StreamFunc func(StreamChunk)
```

`CollectStream` é **público** para apps e executors custom compartilharem um drain; o executor da crew usa `CallOrStream` para LLMs sem stream não precisarem de mudança.

### 3.3 Como a app anexa um sink (D-S3)

**Recomendado na v1: sink no context** (espelha `ContextWithProgress`):

```go
// ContextWithStream anexa um StreamFunc ao ctx. nil é no-op.
func ContextWithStream(ctx context.Context, fn StreamFunc) context.Context

// Crew.WithStream(fn) guarda fn e injeta no ctx do Kickoff
// (mesmo padrão de WithProgress).
func (c *Crew) WithStream(fn StreamFunc) *Crew
```

Razões:
- Não exige campo novo em todo `Agent`/`Task` no caso comum “UI quer tokens”.
- `executeTask` aninhado / agentic loop / delegação veem o mesmo sink.
- Com `fn == nil`, o executor **não** ativa streaming por efeito colateral — **zero mudança de comportamento**.

Alternativa rejeitada na v1: só campo em `Agent` (fácil esquecer sob Hierarchical). Pode entrar depois como override.

### 3.4 O que o sink recebe

| Chunk | Quando | Notas |
|---|---|---|
| `{Delta: "Olá", Task, Agent}` | delta de texto do provider | multi-rune ok; Task/Agent vêm do executor (D-S13) |
| `{Delta: "!", Done: true, Task, Agent}` | último texto + terminal | Delta final pode ir no Done (D-S2-A) |
| `{Err: err, Task, Agent}` | falha HTTP/decode/teto/setup | canal fecha; Done unset (D-S14) |

Sem `Role`, sem parciais de tool-call, sem contagem de tokens na v1. Deltas null/vazios do provider não são encaminhados. AgenticLoop refine pode emitir **várias** sequências Done para o mesmo Task/Agent (uma por rodada de execute) — apps usam fronteiras Done; campo opcional `Phase` fica fora da v1.

---

## 4. Integração no executor

### 4.1 Helper — só nos caminhos **Sim** da matriz

```go
// callLLMText streama quando a matriz permite. NÃO usar como drop-in
// global de todo a.LLM.Call (D-S4).
func callLLMText(ctx context.Context, llm LLM, messages []Message, task *Task, agent *Agent) (string, error) {
    sink := streamFuncFromCtx(ctx)
    if sink != nil {
        sink = withTaskAgent(sink, taskLabel(task), agentRole(agent)) // D-S13
    }
    return CallOrStream(ctx, llm, messages, sink)
}
```

**Regra dura:** substituir `a.LLM.Call` **somente** onde §4.2 diz **Sim**. Structured, turnos ReAct+tools, plan/eval/refine/rewrite e `delegate` hierárquico ficam em `Call` puro.

### 4.2 Matriz de caminhos (D-S4)

| Caminho | Stream de deltas para o sink? | Mecanismo |
|---|---|---|
| ReAct **sem tools** | **Sim** | `callLLMText` / `CallOrStream` |
| ReAct **com tools** (todos os turnos) | **Não** (v1) | `Call` puro — precisa do texto completo para parse Action/Final Answer |
| ReAct **com tools** (só Final Answer) | adiado | fora da v1 |
| Native **sem tools** (`ToolModeNative` + lista de tools vazia) | **Sim** | mesmo fallthrough plain-`Call` em `toolcall.go` → `callLLMText` |
| Rodadas nativas `CallWithTools` (incl. resposta final só-texto) | **Não** (v1) | manter `CallWithTools` bufferizado (D-S10) |
| Structured output (todas as fases) | **Não** | validação precisa do JSON completo; loop de repair também |
| AgenticLoop plan / evaluate / refine / rewrite | **Não** | completions curtas de controle |
| AgenticLoop sub-passo **execute** | segue `executeTaskDefault` | execute sem tools **pode** streamar (pode Done várias vezes entre refines) |
| Hierarchical `delegate` | **Não** | completion minúscula; evita spam na UI |

**Resumo v1:** streamar o **caminho de resposta final visível ao usuário** que já é um `Call` full-text único — ReAct sem tools, native sem tools e agentic execute quando aplicável. Tudo protocol-driven permanece bufferizado.

### 4.3 Fallback (D-S7)

```go
func CallOrStream(ctx context.Context, llm LLM, messages []Message, sink StreamFunc) (string, error) {
    if sink != nil {
        if s, ok := llm.(StreamingLLM); ok {
            ch := s.CallStream(ctx, messages) // nunca nil (D-S14)
            return drainToSink(ctx, ch, sink)   // emitStream por chunk; concat Deltas
        }
    }
    // LLM sem stream ou sem sink: Call de uma vez.
    // D-S7-B: se sink != nil, emite texto cheio como um Delta+Done via emitStream.
    out, err := llm.Call(ctx, messages)
    if err != nil {
        if sink != nil {
            emitStream(ctx, sink, StreamChunk{Err: err})
        }
        return "", err
    }
    if sink != nil {
        emitStream(ctx, sink, StreamChunk{Delta: out, Done: true})
    }
    return out, nil
}
```

`withTaskAgent` copia Task/Agent em todo chunk antes de `emitStream` para providers ficarem sem metadados (D-S13).

### 4.4 Interação com features existentes

| Feature | Interação |
|---|---|
| **Guardrails** | rodam na string final após o stream completar (inalterado) |
| **Memory AutoSave** | salva a string final (inalterado); D-M7 não afeta |
| **Facts** | só caminho de tools; inalterado |
| **Progress** | continua só metadados; eventos `llm_stream_*` **sem** corpo (D-S5-A padrão: **pular** na v1) |
| **Verbose / slog** | Debug pode logar o texto final montado (não cada delta) |
| **Async waves** | cada task pode streamar em paralelo → **StreamFunc DEVE ser concurrency-safe** (documentar como ProgressFunc) |
| **Structured** | sem stream na v1 |
| **MaxIterations / cancel** | cancel de `ctx` fecha o stream no meio |

---

## 5. Notas de implementação por provider

### 5.1 Disciplina compartilhada

1. Request com stream habilitado; **não** fazer `ReadAll` do body.
2. Contar **bytes crus lidos do body** (contador ou `io.LimitedReader`); se exceder → `{Err: ErrStreamResponseTooLarge}`, fechar body e canal (D-S8).
3. Escanear frames (SSE ou NDJSON); mapear fragmentos de texto só para `StreamChunk{Delta}` (Task/Agent vazios). Pular deltas null/vazios do wire.
4. Acumular texto em `strings.Builder`; se `builder.Len() > MaxProviderResponseBytes`, mesmo caminho de too-large.
5. Em EOF limpo / done do provider: enviar `{Done: true}` (com ou sem delta final), fechar canal. Nunca setar Done e Err juntos.
6. Produtor em goroutine; sair em `ctx.Done()` e fechar canal (sem mais sends).
7. Falhas de setup antes do produtor: ainda devolver canal non-nil que emite `{Err}` na hora (D-S14).
8. Nunca logar deltas completos em Info; Debug opcional sob orientação de redação existente.
9. Erros JSON do provider no meio do stream → chunk `{Err}`.

### 5.2 OpenAI / xAI

- `"stream": true` em chat completions.
- Ler SSE: linhas `data: …`; pular comentários; terminal `data: [DONE]`.
- Parsear `choices[0].delta.content`.
- xAI: `CallStream` fino delega a `inner.CallStream` plus
  `var _ crewai.StreamingLLM = (*Client)(nil)` no pacote xai (igual Call).
- Ignorar `delta.content == null` / omitido; não emitir chunks Delta vazios.

### 5.3 Ollama

- `"stream": true` (hoje hardcoded false).
- NDJSON: cada linha é um `chatResponse`; append de `message.content`; terminal quando `done == true`.

### 5.4 Anthropic

- `POST /messages` com `"stream": true`.
- Eventos SSE: `content_block_delta` → `delta.text`; `message_stop` → Done.
- Ignorar `ping` / blocos não-texto na v1.
- **Nota de cobertura:** `llm/anthropic` historicamente fica perto da linha de 90% — qualquer PR que o toque deve deixar o pacote ≥ 90%.

### 5.5 Mock

```go
// StreamChunks opcionalmente fornece sequências por CallStream.
// Se nil, CallStream parte a string de Call em runes de tamanho fixo (ex.: 4)
// para testes do executor assertarem ordem do sink sem rede.
func (m *LLM) CallStream(ctx context.Context, messages []Message) <-chan crewai.StreamChunk
```

---

## 6. Arquivos esperados

| Área | Arquivos |
|---|---|
| API | `llm.go` ou novo `stream.go` (`StreamChunk`, `StreamingLLM`, `CollectStream`, `CallOrStream`, `ContextWithStream`) |
| Crew | `crew.go` — `WithStream`, injetar ctx no `Kickoff` |
| Executor | `executor.go` — caminho sem tools via `CallOrStream`; loop com tools fica em `Call` |
| Providers | `llm/openai`, `llm/ollama`, `llm/anthropic`, `llm/xai` (delegate), `llm/mock` |
| Testes | `stream_test.go`, `*_stream_test.go` nos providers com fixtures `httptest` |
| Docs | `docs/llms.md` + pt-BR, `doc.go`, README What's new (na release), examples |
| Exemplo | `examples/streaming` mock offline despejando em `os.Stdout` |
| CHANGELOG | EN + PT |

Sem pacotes novos. Preferir `stream.go` no root para manter `llm.go` legível.

---

## 7. Segurança

| Tópico | Regra |
|---|---|
| **Teto de bytes** | Texto acumulado ≤ `MaxProviderResponseBytes` **e** bytes crus do **body** lidos ≤ o mesmo teto (D-S8). Evita DoS de frames JSON com deltas vazios. Sentinela `ErrStreamResponseTooLarge`. |
| **Segredos nos deltas** | StreamFunc recebe texto cru do modelo. Docs: não envie deltas crus a clientes multi-tenant sem filtro da app; `RedactHandler` afeta slog, **não** StreamFunc |
| **Pureza do Progress** | Não colocar Delta em `Progress` |
| **Redação de erro** | `chunk.Err` deve seguir a postura `redactError` quando o erro vem das camadas HTTP |
| **Cancelamento** | cancel de `ctx` fecha body HTTP e canal; sem vazamento de goroutine |
| **Consumidor lento** | Se o sink bloqueia, o produtor pode bloquear no send — documentar que StreamFunc deve ser rápida ou a app bufferiza; buffer do canal ≥ 16; ctx cancel é a válvula |
| **SSRF / auth** | inalterado; mesmos endpoints e auth de `Call` |

---

## 8. Testes e gates

### 8.1 Unitário / hermético

- `CollectStream` concatena deltas; propaga Err; respeita cancel de ctx; close nu → `ErrStreamIncomplete` ou `ctx.Err()`.
- `CallOrStream` com LLM não-Streaming + sink → um único Delta+Done via `emitStream`.
- `CallOrStream` com sink nil → sempre `Call` (D-S9).
- Falha de setup em `CallStream` → chan non-nil, primeiro chunk Err (D-S14).
- Executor ReAct sem tools **e** native sem tools: sink recebe deltas ordenados com Task/Agent; output final = concat.
- Executor com tools: sink **não** é chamado em turnos intermediários (v1).
- Wave Async: duas tasks concorrentes → sink vê labels Task distintos (D-S13); sink com mutex no teste.
- Panic em StreamFunc recuperado; Kickoff ainda devolve texto montado (D-S12).
- Provider httptest: SSE OpenAI (incl. content null); NDJSON Ollama; SSE Anthropic; teto de texto **e** de body → `ErrStreamResponseTooLarge`.
- Cancel no meio do stream: canal fecha; `Kickoff` devolve `context.Canceled` / deadline.

### 8.2 Gates de qualidade (todo PR)

Iguais ao §6.1 de security-residuals: cobertura ≥ 90%, `-race`, docs EN+PT, CHANGELOG EN+PT, zero deps novas, gofmt/vet.

### 8.3 Aceite (epic concluído)

- [ ] `StreamingLLM` (embute `LLM`) + `StreamChunk` (Delta/Task/Agent/Done/Err) + `WithStream` / `ContextWithStream` + `CollectStream` público com godoc
- [ ] ReAct sem tools **e** native sem tools streamam com sink + LLM `StreamingLLM`
- [ ] Fallback de um chunk para LLMs sem stream; sentinelas setup/incomplete/too-large
- [ ] openai + ollama + anthropic + xai (`CallStream` delegate) + mock implementam `CallStream`
- [ ] `examples/streaming` offline (inclui demux Async dual de labels Task)
- [ ] docs/llms.md + pt-BR + doc.go + nota SECURITY; checkbox no PLAN.md
- [ ] race-clean; gates de cobertura; CHANGELOG
- [x] Nota de tag: ship como **v0.7.0** (feature release)

---

## 9. Decisões

### 9.1 Fechadas por este plano (defaults — confirmar antes do código da Fase 1)

| ID | Pergunta | Opções | Decisão |
|---|---|---|---|
| **D-S1** | Forma da API | (A) `CallStream` em `LLM` (B) `StreamingLLM` opcional via type assert | **B** — alinha a PLAN §7; zero quebra |
| **D-S2** | Estilo do chunk terminal | (A) delta final pode ir em `{Done:true}` (B) sempre `{Done:true}` vazio depois | **A** |
| **D-S3** | Anexo do sink | (A) ctx + `Crew.WithStream` (B) só campo em `Agent` (C) callback no `LLM` | **A** |
| **D-S4** | Quais caminhos streamam na v1 | (A) todos os Call (B) só texto final / sem tools (C) tudo exceto structured | **B** |
| **D-S5** | Progress vs stream | (A) sem novos eventos Progress (B) stream_started/completed | **A** na v1 |
| **D-S6** | Ordem dos providers | (A) os quatro num PR (B) mock+openai primeiro, depois ollama, anthropic | **B** |
| **D-S7** | LLM sem stream + sink setado | (A) silêncio (B) um Delta+Done com texto cheio | **B** |
| **D-S8** | Limite de tamanho | (A) teto novo maior (B) reusar `MaxProviderResponseBytes` só no **texto** (C) mesmo teto em **texto e bytes crus do body** | **C** — uma constante, dois medidores; fecha DoS de frames JSON com deltas vazios |
| **D-S9** | sink == nil | (A) ainda CallStream se disponível (B) sempre Call | **B** |
| **D-S10** | Stream parcial de tool-call | (A) escopo v1 (B) adiado | **B** |
| **D-S11** | Buffer do canal | (A) unbuffered (B) 16 (C) 64 | **B** — 16 (`DefaultStreamChanBuffer`) |
| **D-S12** | Panic em StreamFunc | (A) derruba Kickoff (B) recover + slog como Progress | **B** — recover em `emitStream` (todos os sinks incl. fallback D-S7); Kickoff continua montando texto |
| **D-S13** | Demux de tasks concorrentes | (A) sink global só com deltas (B) Task/Agent no chunk pelo executor (C) API de sinks por task | **B** — providers só texto; wrapper `withTaskAgent` no executor |
| **D-S14** | Contrato de erro / canal do CallStream | (A) retorno `(<-chan, error)` de setup (B) chan nunca nil; setup → primeiro `{Err}`; close nu → `ErrStreamIncomplete` / `ctx.Err()`; proibir Done∧Err | **B** — um shape de consumo; sentinelas em `errors.go` |

### 9.2 Abertas até spikes de implementação (não bloqueiam Fase 1)

| ID | Tópico | Notas |
|---|---|---|
| **O-S1** | Mapping de tool-result Anthropic sob stream | Só se streamarmos caminhos nativos depois |
| **O-S2** | Helpers de transporte compartilhados Call/CallStream | Refactor interno; sem impacto de API |

~~**O-S3** `CollectStream` público vs interno~~ — **fechado: público**.

---

## 10. Ordem de PRs / implementação

```
Fase 0  ~~Fechar D-S1–D-S14~~ **feito na review de design 2026-08-21** (este patch)
Fase 1  S1: API stream.go + CollectStream/CallOrStream/emitStream + sentinelas + testes (sem provider)
Fase 2  S2: llm/mock CallStream + executor ReAct **e** native sem tools + Crew.WithStream + wrap D-S13
Fase 3  S3: llm/openai CallStream (httptest SSE) + xai CallStream delegate + assert em compile-time
Fase 4  S4: llm/ollama CallStream (NDJSON)
Fase 5  S5: llm/anthropic CallStream (SSE) + cobertura ≥ 90%
Fase 6  S6: examples/streaming + docs EN/PT + doc.go + CHANGELOG
Fase 7  S7: higiene de release → tag v0.7.0 (ou bundle)
```

**Grafo:** S1 → S2 → (S3 ∥ S4 ∥ S5) → S6 → S7.  
**Não** começar provider antes da API S1 mergeada.

---

## 11. Plano de documentação

| Doc | Atualização |
|---|---|
| `docs/llms.md` + `docs/pt-BR/llms.md` | Seção “Streaming”: WithStream, StreamingLLM, fallback, concorrência, nota de segurança sobre deltas crus |
| `doc.go` | Parágrafo no overview + exemplo mínimo |
| `README.md` / `README.pt-BR.md` | Bullet em Why + What's new na release |
| `docs/crews.md` | `WithStream` ao lado de `WithProgress` |
| `examples/streaming` | Mock offline imprimindo deltas; duas tasks `WithAsync` mostram demux de Task |
| `PLAN.md` / `PLAN.pt-BR.md` | Link deste plano; checkbox ao shippar |
| `SECURITY.md` | Uma linha: sinks de stream recebem texto cru do modelo (responsabilidade da app) |

---

## 12. Versionamento e compatibilidade

- **Semver:** APIs novas são aditivas → **minor** (v0.7.0 enquanto em v0.x).
- **Sem mudança** no method set de `LLM`.
- LLMs externos só com `Call` continuam válidos; com `WithStream`, ganham o comportamento de um chunk (D-S7).
- Testes golden de Kickoff sem stream devem permanecer idênticos com `WithStream` nil.

---

## 13. Fora de escopo / depois

| Item | Onde |
|---|---|
| Stream de deltas de argumentos de tool-call nativo | Follow-up ao reabrir D-S10 |
| Stream de JSON structured com parse incremental | Improvável; validar-no-fim basta |
| Eventos Progress por token / spans OTEL | Epic P2 de callbacks/telemetria |
| Helper SSE para expor Kickoff via HTTP | Só padrão de exemplo, não core |
| Token usage / `finish_reason` em StreamChunk | Campos aditivos depois se necessário |

---

## 14. Resumo

Shippar **`StreamingLLM` opcional (embute `LLM`)** + **`Crew.WithStream` / sink no context**, reutilizar o **padrão de type-assert de `ToolCallingLLM`**, streamar **ReAct sem tools + native sem tools** na v1 (não caminhos de protocolo), **fallback para Call de uma vez** (um chunk se houver sink), **demux Task/Agent nos chunks**, **canal non-nil + setup-como-Err**, **tetos de texto e de body**, implementar **mock → openai/xai → ollama → anthropic**, manter **Progress sem corpos** e **só-stdlib**.

Decisões **D-S1–D-S14** estão fechadas neste documento. A Fase 1 de código pode começar em um branch como `feat/streaming-s1-api` após ack de produto (ou seguir se não houver objeções).

---

> **Idioma:** espelho fiel de [`PLAN.streaming.md`](PLAN.streaming.md) (inglês é a fonte autoritativa).
