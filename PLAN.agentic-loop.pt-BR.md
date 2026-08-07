# Plano — Agentic Loop: Planejar-Executar-Avaliar-Refinar

> **Languages:** [English](PLAN.agentic-loop.md) · **Português** (atual)

> **Status:** Rascunho para implementação
> **Escopo:** Adicionar um loop de execução de agente opcional e mais sofisticado
> que vai além do simples ciclo ReAct. O AgenticLoop segue um padrao
> Planejar-Executar-Avaliar-Refinar, dando aos agentes a capacidade de
> autoavaliar sua saida e melhora-la iterativamente antes de retornar uma
> resposta final. Zero novas dependencias, backward compatible.

---

## 1. Objetivos e Restricoes

### 1.1 Objetivos

1. Fornecer um loop de execucao alternativo (`AgenticLoop`) que substitui o
   loop ReAct de passagem unica por um ciclo multifase:
   - **Plan (Planejar)**: o agente decompoe a tarefa em passos acionaveis.
   - **Execute (Executar)**: o agente executa cada passo, usando tools conforme necessario.
   - **Evaluate (Avaliar)**: o agente (ou um avaliador) avalia a saida contra
     a saida esperada da tarefa e criterios de qualidade.
   - **Refine (Refinar)**: se a avaliacao falhar, o agente revisa e reexecuta,
     ate um numero maximo configuravel de rodadas de refinamento.
2. Permitir que a fase de avaliacao use um LLM/agente avaliador separado (ou o
   mesmo agente com um prompt diferente) para uma avaliacao independente de qualidade.
3. Suportar condicoes de parada configuraveis: max rodadas de refinamento,
   limiar de avaliacao (pass/fail), ou uma funcao avaliadora customizada.
4. Integrar com features existentes: saida estruturada (a resposta final pode
   passar por validacao de schema), guardrails (a saida do loop esta sujeita
   a guardrails de crew/task) e facts (coletados durante a execucao).
5. Funcionar com ou sem tools — a fase de planejamento e omitida quando o
   agente nao tem tools (equivalente a uma chamada direta com autoavaliacao).

### 1.2 Restricoes (devem ser preservadas)

| Restricao | Aplicacao |
|---|---|
| Zero dependencias externas | `go.mod` inalterado; o loop usa apenas stdlib (`context`, `fmt`, `strings`, `encoding/json`). |
| LLM-agnostico, `LLM.Call` sem breaking change | A assinatura `Call(ctx, []Message) (string, error)` nunca e modificada. O AgenticLoop funciona via prompt engineering + orchestracao em Go. |
| Comentarios em ingles, sem acentos | Todo godoc, mensagens de erro, prompts e identificadores seguem esta regra. |
| Testes hermeticos | `llm/mock` + spy tools; sem rede real. |
| Sentinel errors, context em todo I/O, opcoes funcionais | Novas sentinelas em `errors.go`; `ctx` propagado em todas as fases; opcoes funcionais para `AgenticLoop`. |
| Seguranca de concorrencia | O loop e stateless por execucao; sem estado mutavel compartilhado entre goroutines. |
| Backward compatibility | O executor padrao continua sendo o loop ReAct. AgenticLoop e opt-in via `Agent.Loop` ou `Task.Loop`. O comportamento existente e inalterado quando nenhum loop e configurado. |

---

## 2. Contexto — Por que um Agentic Loop?

### 2.1 Limitacoes do loop ReAct atual

O executor atual (`executor.go`) roda um unico loop ReAct:

```
Thought → Action → Observation → ... → Final Answer
```

Isso funciona bem para tarefas simples, mas tem limitacoes:

1. **Sem fase de planejamento** — o agente reage passo a passo mas nunca
   planeja explicitamente sua abordagem, o que pode levar a exploracao desfocada.
2. **Sem autoavaliacao** — assim que o agente produz um `Final Answer`, o loop
   termina. Nao ha verificacao de qualidade; uma resposta mediocre e retornada.
3. **Sem refinamento iterativo** — se a resposta estiver incompleta ou errada,
   nao ha mecanismo para tentar novamente com feedback.
4. **Sem avaliacao independente** — o mesmo agente que produziu a saida a
   avalia (se e que avalia), criando um ponto cego.

### 2.2 O que o AgenticLoop adiciona

O AgenticLoop introduz um **meta-nivel** acima do loop ReAct:

```
┌─────────────────────────────────────────────────┐
│                  AgenticLoop                     │
│                                                  │
│  ┌─────────┐    ┌──────────┐    ┌──────────┐   │
│  │  Plan   │───▶│ Execute  │───▶│ Evaluate │   │
│  └─────────┘    └──────────┘    └────┬─────┘   │
│                      ▲                │         │
│                      │           pass? │         │
│                      │                ▼         │
│                  ┌───┴────┐      ┌─────────┐   │
│                  │ Refine │◀────│  Fail   │   │
│                  └────────┘      └─────────┘   │
│                      │                          │
│                      ▼                          │
│               (max rodadas?)                    │
│                      │                          │
│              ┌───────────────┐                 │
│              │  Final Answer │                 │
│              └───────────────┘                 │
└─────────────────────────────────────────────────┘
```

- **Plan**: o agente produz uma lista numerada de passos antes de agir. Isso
  e injetado como contexto para a fase de execucao, mantendo o agente focado.
- **Execute**: o agente roda o loop ReAct (executor existente) com o plano
  como contexto adicional.
- **Evaluate**: um avaliador (mesmo agente com prompt de avaliacao, ou um
  agente avaliador separado) pontua a saida contra a saida esperada e criterios
  de qualidade. Retorna PASS ou FAIL + feedback.
- **Refine**: em caso de FAIL, o feedback e injetado como contexto e o agente
  reexecuta (ou apenas reescreve, dependendo da configuracao). O loop repete
  ate `MaxRefinements` vezes.

---

## 3. Nova superficie de API

### 3.1 Tipo `Loop` (pacote raiz)

```go
// Loop e uma estrategia de execucao opcional para um agente. Quando definido
// em um Agent ou Task, substitui o executor ReAct de passagem unica por um
// ciclo multifase Planejar-Executar-Avaliar-Refinar.
type Loop interface {
    // Run executa a tarefa usando a estrategia de loop e retorna a resposta
    // final, facts coletados e um erro.
    Run(ctx context.Context, a *Agent, t *Task, contextText string, log Logger) (string, []Fact, error)
}
```

### 3.2 Struct `AgenticLoop`

```go
// AgenticLoop implementa o padrao Planejar-Executar-Avaliar-Refinar.
type AgenticLoop struct {
    // MaxRefinements e o numero maximo de ciclos avaliar→refinar
    // apos a execucao inicial. Se <= 0, o padrao e 2.
    MaxRefinements int

    // Evaluator e um agente separado opcional que avalia a saida.
    // Se nil, o mesmo agente se avalia com um prompt dedicado.
    Evaluator *Agent

    // EvaluationPrompt e um template de prompt opcional customizado para a
    // fase de avaliacao. Se vazio, um template padrao e usado.
    // Recebe {task}, {expected_output}, {actual_output} como variaveis.
    EvaluationPrompt string

    // PassThreshold e a pontuacao minima de avaliacao (0-100) para considerar
    // a saida aceitavel. Se <= 0, o padrao e 70.
    // O avaliador deve retornar um objeto JSON com um campo "score" (0-100)
    // e um campo opcional "feedback".
    PassThreshold int

    // SkipPlan, quando true, omite a fase de planejamento e vai direto para
    // a execucao. Util para tarefas simples com tools.
    SkipPlan bool
}
```

### 3.3 Opcoes funcionais

```go
// WithMaxRefinements define o numero maximo de ciclos de refinamento.
func WithMaxRefinements(n int) func(*AgenticLoop)

// WithEvaluator define um agente avaliador separado.
func WithEvaluator(a *Agent) func(*AgenticLoop)

// WithEvaluationPrompt define um template de prompt de avaliacao customizado.
func WithEvaluationPrompt(template string) func(*AgenticLoop)

// WithPassThreshold define a pontuacao minima para aprovar (0-100).
func WithPassThreshold(score int) func(*AgenticLoop)

// WithSkipPlan omite a fase de planejamento.
func WithSkipPlan() func(*AgenticLoop)
```

### 3.4 Novos campos de `Agent` e `Task`

```go
type Agent struct {
    // ... campos existentes ...

    // Loop, quando definido, substitui o executor ReAct padrao para todas as
    // tarefas executadas por este agente (a menos que a task o sobrescreva).
    Loop Loop
}

type Task struct {
    // ... campos existentes ...

    // Loop, quando definido, sobrescreve o loop do agente para esta tarefa
    // especifica.
    Loop Loop
}
```

### 3.5 Novas sentinelas de erro

```go
var (
    // ErrEvaluationFailed e retornado quando o avaliador pontua a saida
    // abaixo do limiar de aprovacao e todas as tentativas de refinamento
    // se esgotam.
    ErrEvaluationFailed = errors.New("crewai: output did not pass evaluation after all refinements")

    // ErrInvalidEvaluation e retornado quando o avaliador produz uma
    // resposta que nao pode ser parseada como um resultado de avaliacao valido.
    ErrInvalidEvaluation = errors.New("crewai: evaluator returned an invalid response")
)
```

### 3.6 Construtor

```go
// NewAgenticLoop cria um AgenticLoop com as opcoes fornecidas.
func NewAgenticLoop(opts ...func(*AgenticLoop)) *AgenticLoop
```

---

## 4. Fluxo de Execucao

### 4.1 `AgenticLoop.Run`

```
1. Se nao SkipPlan e o agente tem tools:
   a. Chamar LLM com planPrompt(task, contextText) → string do plano
   b. Prepend o plano ao contextText

2. Fase de execucao:
   a. Chamar executeTask(ctx, agent, task, contextText, log)
      - Reutiliza o executor ReAct existente.
      - Se Task.Structured estiver definido, executeStructured roda normalmente.
   b. Coletar saida e facts.

3. Fase de avaliacao:
   a. Construir evaluationPrompt(task, expectedOutput, actualOutput)
   b. Se Evaluator estiver definido, chamar Evaluator.LLM; senao chamar Agent.LLM
      com um prompt de sistema dedicado a avaliacao.
   c. Parsear a resposta do avaliador como JSON: {"score": int, "feedback": string}
   d. Se o parse falhar → retornar ErrInvalidEvaluation (ou tentar reparo uma vez)

4. Decisao:
   a. Se score >= PassThreshold → retornar saida, facts, nil
   b. Se score < PassThreshold e refinamentos < MaxRefinements:
      - Anexar feedback ao contextText
      - Ir para passo 2 (refinar)
   c. Se refinamentos esgotados → retornar ErrEvaluationFailed (wrap do ultimo feedback)

5. Integracao com saida estruturada:
   - Se Task.Structured estiver definido, o caminho de saida estruturada roda
     dentro de executeTask normalmente. A avaliacao verifica o JSON canonizado
     contra a saida esperada, nao a resposta bruta do modelo.
```

### 4.2 Prompt de planejamento

```
You are about to work on the following task:

{task description}
{expected output}
{context from previous tasks}

Before acting, create a concise, numbered plan of the steps you will take.
Consider which tools you have available and how to use them.

Respond ONLY with the plan, numbered 1-N. No other text.
```

### 4.3 Prompt de avaliacao (padrao)

```
You are an evaluator. Your job is to assess whether the output meets the
expected quality and completeness for the task.

Task:
{task description}

Expected output:
{expected output}

Actual output:
{actual output}

Evaluate the actual output against the expected output. Respond ONLY with a
JSON object:
{
  "score": <integer 0-100>,
  "feedback": "<brief explanation of issues or confirmation of quality>"
}

Scoring guide:
- 90-100: excellent, fully meets expectations
- 70-89: good, minor issues
- 50-69: partial, significant gaps
- 0-49: poor, major problems
```

### 4.4 Prompt de refinamento

```
The evaluator provided the following feedback on your previous output:

{feedback}

Score: {score}/{threshold}

Please revise your output to address the feedback. Re-execute the task with
the improvements in mind.
```

---

## 5. Interacao com Features Existentes

### 5.1 Saida estruturada

Quando `Task.Structured` esta definido, a fase de execucao do AgenticLoop chama
`executeStructured` (via `executeTask`) normalmente. O loop de reparo da saida
estruturada roda primeiro; depois a fase de avaliacao do AgenticLoop avalia
o JSON canonizado contra a saida esperada. Isso cria uma validacao em duas camadas:

1. **Validacao de schema** (loop de reparo da saida estruturada): verifica a forma do JSON.
2. **Avaliacao de qualidade** (AgenticLoop): verifica a qualidade do conteudo.

Se o loop de reparo da saida estruturada falhar (`ErrRepairBudgetExceeded`), o
AgenticLoop NAO repete — o erro se propaga imediatamente. A fase de avaliacao
so roda em JSON validado com sucesso.

### 5.2 Guardrails

Guardrails rodam DEPOIS que o AgenticLoop completa, no mesmo lugar que rodam
hoje (em `Crew.execute` apos `executeTask` retornar). O AgenticLoop e transparente
para guardrails — retorna uma tupla `(string, []Fact, error)` como o executor ReAct.

### 5.3 Facts

Facts coletados durante a fase de execucao (de tools `FactSource`) sao retornados
junto com a saida final. Em rodadas de refinamento, facts de todas as rodadas sao
acumulados e deduplicados por `PayloadHash` (mesmo mecanismo `dedupFacts`).

### 5.4 Processo hierarquico

O AgenticLoop funciona com o processo hierarquico — o gerente delega para um
agente, e esse agente usa seu loop configurado. O gerente em si nao usa um loop
(delegacao e uma unica chamada LLM).

### 5.5 Memoria

A memoria e tratada no nivel de `Crew.execute`, nao no executor. A saida do
AgenticLoop e armazenada na memoria assim como a saida do executor ReAct. Sem
mudancas necessarias.

---

## 6. Mudancas em Arquivos

| Arquivo | Mudanca | Descricao |
|---------|---------|-----------|
| `loop.go` | **Novo** | Interface `Loop`, struct `AgenticLoop`, metodo `Run`, prompts de planejamento/avaliacao/refinamento. |
| `loop_test.go` | **Novo** | Testes hermeticos usando `llm/mock`: plan→execute→evaluate→pass, plan→execute→evaluate→fail→refine→pass, max refinements esgotado, erro de parse do avaliador, skip plan, sem tools, integracao com saida estruturada, coleta de facts. |
| `errors.go` | **Modificado** | Adicionar `ErrEvaluationFailed` e `ErrInvalidEvaluation`. |
| `agent.go` | **Modificado** | Adicionar campo `Loop`. |
| `task.go` | **Modificado** | Adicionar campo `Loop`. |
| `executor.go` | **Modificado** | Em `executeTask`, verificar `t.Loop` depois `a.Loop` antes de padronizar para ReAct. |
| `prompts.go` | **Modificado** | Adicionar `buildPlanPrompt`, `buildEvaluationPrompt`, `buildRefinePrompt`. |
| `doc.go` | **Modificado** | Documentar o AgenticLoop na visao geral do pacote. |
| `examples/agentic_loop/main.go` | **Novo** | Exemplo executavel com mock LLM. |
| `docs/agents.md` | **Modificado** | Adicionar secao AgenticLoop. |
| `docs/pt-BR/agents.md` | **Modificado** | Espelho em portugues. |
| `PLAN.md` | **Modificado** | Atualizar roadmap: marcar AgenticLoop como em andamento/concluido. |
| `PLAN.pt-BR.md` | **Modificado** | Espelho em portugues. |
| `README.md` | **Modificado** | Adicionar AgenticLoop a "What's new" / lista de features. |
| `README.pt-BR.md` | **Modificado** | Espelho em portugues. |
| `CHANGELOG.md` | **Modificado** | Adicionar entrada sob `[Unreleased]`. |
| `CHANGELOG.pt-BR.md` | **Modificado** | Espelho em portugues. |

---

## 7. Modificacao do `executeTask`

A funcao `executeTask` atual e o ponto de entrada para a execucao de tarefas.
Sera modificada para verificar um loop configurado:

```go
func executeTask(ctx context.Context, a *Agent, t *Task, contextText string, log Logger) (string, []Fact, error) {
    if a.LLM == nil {
        return "", nil, ErrNoLLM
    }

    // Verificar loop: nivel task tem precedencia sobre nivel agent.
    if t != nil && t.Loop != nil {
        return t.Loop.Run(ctx, a, t, contextText, log)
    }
    if a.Loop != nil {
        return a.Loop.Run(ctx, a, t, contextText, log)
    }

    // Padrao: saida estruturada ou loop ReAct (comportamento existente).
    if t != nil && t.Structured != nil {
        out, err := executeStructured(ctx, a, t, contextText, log)
        return out, nil, err
    }

    // ... codigo existente do loop ReAct inalterado ...
}
```

---

## 8. Parse da Resposta de Avaliacao

O avaliador deve retornar JSON: `{"score": int, "feedback": string}`.
O parse usa a mesma abordagem `extractJSON` + `encoding/json` da feature de
saida estruturada. Se o avaliador retornar nao-JSON, o loop tenta novamente a
avaliacao uma vez com um prompt de reparo (similar ao reparo da saida estruturada).
Se o parse ainda falhar, `ErrInvalidEvaluation` e retornado.

```go
type evaluationResult struct {
    Score    int    `json:"score"`
    Feedback string `json:"feedback"`
}

func parseEvaluation(raw string) (evaluationResult, error) {
    cleaned := extractJSON(raw)
    var result evaluationResult
    if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
        return evaluationResult{}, fmt.Errorf("%w: %v", ErrInvalidEvaluation, err)
    }
    if result.Score < 0 || result.Score > 100 {
        return evaluationResult{}, fmt.Errorf("%w: score out of range (0-100)", ErrInvalidEvaluation)
    }
    return result, nil
}
```

---

## 9. Plano de Testes

Todos os testes usam `llm/mock` — sem rede real.

### 9.1 Testes unitarios (`loop_test.go`)

| Teste | Descricao |
|-------|-----------|
| `TestAgenticLoop_PassOnFirstEvaluation` | Plan → execute → evaluate (score 95) → pass. Verificar saida e facts. |
| `TestAgenticLoop_RefineThenPass` | Execute → evaluate (score 50) → refine → evaluate (score 85) → pass. Verificar feedback injetado. |
| `TestAgenticLoop_MaxRefinementsExhausted` | Execute → evaluate (score 40) → refine → evaluate (score 40) → refine → evaluate (score 40) → `ErrEvaluationFailed`. MaxRefinements=2. |
| `TestAgenticLoop_EvaluatorParseError` | Avaliador retorna nao-JSON → reparo → ainda nao-JSON → `ErrInvalidEvaluation`. |
| `TestAgenticLoop_SkipPlan` | SkipPlan=true → sem chamada de plano, execute direto → evaluate → pass. Verificar apenas 2 chamadas LLM (execute + evaluate). |
| `TestAgenticLoop_NoTools` | Agente sem tools → plano omitido → chamada direta → evaluate → pass. |
| `TestAgenticLoop_WithStructuredOutput` | Task com Structured definido → execute roda caminho estruturado → evaluate verifica JSON → pass. |
| `TestAgenticLoop_FactsCollection` | Agente com tool FactSource → facts coletados entre rodadas de refinamento → deduplicados. |
| `TestAgenticLoop_SeparateEvaluator` | Agente avaliador definido → avaliacao usa LLM do avaliador, nao do executor. |
| `TestAgenticLoop_CustomEvaluationPrompt` | Template de prompt customizado → verificar que aparece na mensagem de avaliacao. |
| `TestAgenticLoop_TaskLoopOverridesAgentLoop` | Agente tem Loop A, Task tem Loop B → Task.Loop e usado. |
| `TestAgenticLoop_DefaultReActUnchanged` | Agente/Task sem Loop → comportamento ReAct existente, sem caminho de AgenticLoop. |

### 9.2 Exemplo

`examples/agentic_loop/main.go` — um exemplo autonomo usando `llm/mock`
com respostas roteirizadas mostrando o ciclo completo Planejar-Executar-Avaliar-Refinar.

---

## 10. Ordem de Implementacao

1. **`errors.go`** — adicionar `ErrEvaluationFailed`, `ErrInvalidEvaluation`.
2. **`prompts.go`** — adicionar `buildPlanPrompt`, `buildEvaluationPrompt`,
   `buildRefinePrompt`, `buildEvalRepairPrompt`.
3. **`loop.go`** — interface `Loop`, struct `AgenticLoop`, `NewAgenticLoop`,
   metodo `Run`, helper `parseEvaluation`.
4. **`agent.go`** — adicionar campo `Loop`.
5. **`task.go`** — adicionar campo `Loop`.
6. **`executor.go`** — modificar `executeTask` para verificar `Loop`.
7. **`loop_test.go`** — todos os testes unitarios.
8. **`examples/agentic_loop/main.go`** — exemplo executavel.
9. **`docs/agents.md`** + **`docs/pt-BR/agents.md`** — documentacao.
10. **`doc.go`** — atualizar visao geral do pacote.
11. **`PLAN.md`** + **`PLAN.pt-BR.md`** — atualizar status do roadmap.
12. **`README.md`** + **`README.pt-BR.md`** — lista de features + "What's new".
13. **`CHANGELOG.md`** + **`CHANGELOG.pt-BR.md`** — adicionar entrada.

---

## 11. Decisoes em Aberto

| Decisao | Opcoes | Recomendacao |
|---------|--------|-------------|
| O avaliador deve usar um `LLM` separado (nao um `Agent` completo)? | `Evaluator LLM` vs `Evaluator *Agent` | Usar `*Agent` para flexibilidade (persona/backstory separado para avaliacao). Um `LLM` puro pode ser envolvido em um agente facilmente. |
| O plano deve ser uma string ou um array JSON estruturado de passos? | String (simples) vs JSON (parseavel) | String — mantem simples e evita modo de falha por parse. O plano e orientacao, nao um programa. |
| O refinamento deve reexecutar (com tools) ou apenas reescrever? | Reexecutar vs apenas reescrever | Reexecutar por padrao (o agente pode usar tools novamente), com opcao `RefineRewriteOnly bool` para omitir tools no refinamento. |
| A pontuacao de avaliacao deve ser obrigatoria em JSON ou aceitar texto livre? | Apenas JSON vs texto livre com regex | Apenas JSON — consistente com saida estruturada, parseavel, e o prompt de reparo lida com respostas malformadas. |
| O `AgenticLoop` deve ser aninhavel (loop dentro de loop)? | Sim vs nao | Nao — um loop por task. Aninhar adiciona complexidade sem beneficio claro. |