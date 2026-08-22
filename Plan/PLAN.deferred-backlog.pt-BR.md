# Plano — Backlog adiado: itens abertos/spike (P-XAI-OAUTH, M5, A5, D-S10, D-J9, O-J2)

> **Status:** **Design** — nenhum destes itens está agendado para código. Este plano define as **condições de desbloqueio**, a **superfície de design** quando iniciarem, e os **IDs de decisão** que serão cunhados.  
> **Decisões:** Novos IDs serão **D-D1–D-Dxx** quando cada item sair de `deferred/open` para `closed`. Este documento não fecha nenhum deles.  
> **Relacionado:** [`DECISIONS.md`](DECISIONS.md) §9 (tabela aberta), `PLAN.streaming.md` (D-S10), `PLAN.p2-callbacks-schema.md` (D-J9, O-J2), `PLAN.memory-async.md` (M5, A5), `llm/xai/oauth.go`, `schema.go`.  
> **Restrições:** Mesmos gates — zero deps novas no core, ≥ 90% cobertura, race-clean, docs EN+PT, CHANGELOG.

Fonte autoritativa: [`PLAN.deferred-backlog.md`](PLAN.deferred-backlog.md) (inglês). Este espelho segue a mesma estrutura.

---

## 1. Propósito

O roadmap principal (PLAN.md §6) entregou **P0–P2** e deixou itens bloqueados por dependência externa (xAI docs) ou adiados por falta de demanda (M5, A5, D-S10, D-J9, O-J2). Este plano é o lugar único que define:

- o que desbloqueia cada item,
- o espaço de design quando iniciar,
- quais IDs de decisão vai cunhar,
- e o que *não* pode fazer quando começar.

Não é um plano de implementação. Código só começa quando a condição de desbloqueio é atendida **e** um sub-plano/spike confirma o design.

---

## 2. Índice de itens

| ID | Item | Categoria | Condição de desbloqueio | Complexidade |
|----|------|-----------|--------------------------|--------------|
| **P-XAI-OAUTH** | Defaults OAuth xAI (client_id + endpoints) | Bloqueado externo | xAI publica docs oficiais OAuth | Baixa (só config) |
| **M5** | Tools de memória `recall_memory` / `remember` | Adiado — demanda de produto | Produto pede tools agent-driven de memória | Média |
| **A5** | Alias `Process=DAG` | Adiado — sugar de naming | Usuários confundem "Sequential+Async" | Trivial |
| **D-S10** | Stream parcial de JSON de tool-call | Adiado — complexidade técnica | Demanda + spike de provider | Alta |
| **D-J9** | `unevaluatedProperties` / `unevaluatedItems` | Adiado — custo de correção | Schemas reais precisam; spike de modelo de anotação | Alta |
| **O-J2** | `format: time` | Spike aberto — ambiguidade semântica | Decidir incluir/adir pela análise de ambiguidade | Trivial |

---

## 3. Detalhe por item

### 3.1 P-XAI-OAUTH — defaults OAuth xAI

**Bloqueado em:** xAI publicar docs oficiais de OAuth 2.0 Device Flow (client_id público, URLs de device code e token, scope).

**Estado atual:** implementação genérica RFC 8628 + PKCE. `ClientID` do caller, endpoints padrão convencionais, overridable.

**Quando desbloquear:** ajustar constantes `DefaultXAIClientID` + endpoints se necessário. Mint **D-XA1**.

### 3.2 M5 — Tools de memória

**Adiado até:** produto querer recall/writes acionáveis por LLM (não só auto-inject).

**Por que adiado:** auto-inject + auto-save cobrem o caso comum; tool-driven writes bypassam D-M7; recall precisa de política de budget própria.

**Espaço de design:** quantas tools, como anexar ao agente, budget de recall, visibilidade de write (imediata vs D-M7). Mint **D-MT1–D-MT5**.

### 3.3 A5 — `Process=DAG`

**Adiado até:** sinal de confusão com o nome "Sequential + Async".

**O que muda:** constante/alias para `Process` (additive). Mint **D-A7**. **Non-goal:** mudar semântica do scheduler.

### 3.4 D-S10 — Stream parcial de tool-call

**Adiado porque:** `CallWithTools` devolve ToolCalls completos; JSON parcial de argumentos por provider é difícil e provider-específico.

**Quando desbloquear:** shape de chunk de tool-call, assembly buffer, turno final sem tool_calls. Mint **D-ST1–D-ST4**.

### 3.5 D-J9 — unevaluated*

**Adiado porque:** precisa de anotação de "avaliado" por nó através de aplicadores ($ref, allOf, if/then) — modelo de eval-set não existe hoje.

**Quando desbloquear:** escopo de anotação, interação com $ref, boolean as value. Mint **D-JE1–D-JE3**.

### 3.6 O-J2 — `format: time`

**Spike:** RFC 3339 full-time: `HH:MM:SS` + fração + offset opcional. Ambiguidade entre "time" (hora do dia) e "date-time" (com data).

**Resultado do spike:** incluir ou adiar explicitamente. Mint **D-JT1**.

---

## 4. Alocação de decisões

| Item | Novos IDs | Quando |
|------|-----------|--------|
| P-XAI-OAUTH | **D-XA1**, talvez **D-XA2** | Docs xAI |
| M5 | **D-MT1–D-MT5** | Pedido do produto |
| A5 | **D-A7** | Sinal de confusão |
| D-S10 | **D-ST1–D-ST4** | Spike + demanda |
| D-J9 | **D-JE1–D-JE3** | Spike de anotação |
| O-J2 | **D-JT1** | Conclusão do spike |

Todos os IDs entram na família certa em [`DECISIONS.md`](DECISIONS.md) quando fechados.

---

## 5. Dependências de desbloqueio

```
P-XAI-OAUTH ──► espera xAI ──► D-XA1 fecha
M5 ──► espera produto pedir ──► D-MT* fecha → implementa
A5 ──► espera sinal de confusão ──► D-A7 → alias só
D-S10 ──► espera produto + spike provider ──► D-ST* → novo path stream
D-J9 ──► espera demanda de schema ──► D-JE* → modelo de anotação
O-J2 ──► conclusão do spike ──► D-JT1 → um case em checkFormat
```

Nenhum bloqueia outro nem bloqueia P3.

---

## 6. Ordem de implementação (se tudo desbloquear amanhã)

| # | Item | Por quê |
|---|------|---------|
| 1 | **O-J2** | Spike trivial; um case ou deferral |
| 2 | **P-XAI-OAUTH** | Só constantes, sem risco de interface |
| 3 | **A5** | Naming sugar, ~10 linhas, aditivo |
| 4 | **M5** | Média complexidade; duas tools pequenas |
| 5 | **D-S10** | Alta; toca wire protocols de provider |
| 6 | **D-J9** | Maior; precisa de modelo de anotação |

---

## 7. Resumo

Seis itens: um bloqueado (xAI), cinco adiados. Todos com espaço de design claro e IDs pré-alocados em [`DECISIONS.md`](DECISIONS.md) §9. Quando a condição de desbloqueio for atendida, o sub-plano sai aqui ou em `PLAN.<item>.md`, e código só depois das decisões fecharem.

---

> **Idioma:** espelho fiel de [`PLAN.deferred-backlog.md`](PLAN.deferred-backlog.md) (inglês é a fonte autoritativa).
