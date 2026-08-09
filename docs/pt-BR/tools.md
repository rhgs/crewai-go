# Tools

> **Languages:** [English](../tools.md) · **Português** (atual)

Uma **Tool** dá "mãos" ao agente: permite que ele realize ações — calcular,
consultar uma API, ler um arquivo — durante o raciocínio.

## A interface

```go
type Tool interface {
	Name() string
	Description() string
	Call(ctx context.Context, input string) (string, error)
}
```

Entrada e saída são `string`, para casar com o raciocínio textual do agente.
Se sua ferramenta precisa de argumentos estruturados, documente o formato
(ex.: um JSON) na `Description`.

## Criando uma ferramenta a partir de uma função

```go
clima := crewai.NewTool(
	"clima",
	"Consulta o clima de uma cidade. Entrada: o nome da cidade.",
	func(ctx context.Context, cidade string) (string, error) {
		// chame sua API aqui...
		return fmt.Sprintf("Ensolarado em %s, 27°C", cidade), nil
	},
)
```

## Ferramentas embutidas (`pacote tools`)

```go
import "github.com/rhgs/crewai-go/tools"

tools.Calculator()      // avalia "2 + 2 * (3 - 1)" — offline e seguro
tools.CurrentTime("")   // data/hora atual (layout do pacote time; "" = RFC3339)
tools.WordCount()       // conta palavras e caracteres do texto
```

## Anexando ferramentas

A um agente (disponível em todas as suas tarefas):

```go
agente.WithTools(tools.Calculator(), clima)
```

A uma tarefa específica (sobrepõe as do agente só naquela tarefa):

```go
tarefa.Tools = []crewai.Tool{clima}
```

## O protocolo ReAct

Quando um agente tem ferramentas, ele segue um ciclo de **Raciocínio + Ação**.
O modelo é instruído a responder neste formato:

```
Thought: preciso calcular o total
Action: calculator
Action Input: 1500 * 1.12
```

O framework executa a ferramenta e devolve:

```
Observation: 1680
```

O ciclo se repete até o modelo concluir:

```
Thought: agora sei a resposta
Final Answer: O valor final é R$ 1.680,00.
```

### Robustez

- Se o modelo **não** seguir o protocolo, a saída é tratada como resposta final
  (em vez de travar).
- Se o modelo pedir uma ferramenta **inexistente**, o framework devolve uma
  `Observation` de erro listando as ferramentas válidas, e o agente tenta de novo.
- Erros retornados pela ferramenta viram uma `Observation` de erro — o agente
  pode reagir a eles.
- O laço para em `MaxIterations` (padrão 15), retornando `ErrMaxIterations`.

## Web search (WebSearchTool)

O `WebSearchTool` é uma ferramenta que busca na web através de um provedor
de busca (`SearchProvider`). Ele implementa tanto `crewai.Tool` (para uso
no loop ReAct) quanto `crewai.FactSource` (para coleta de `Facts` com
proveniência).

- **Como Tool**: o agente decide quando buscar durante o raciocínio ReAct.
- **Como FactSource**: após cada `Call` bem-sucedida, os resultados são
  coletados como `Facts` com `SourceOrg`, `SourceURL` e `PayloadHash`.
- **SSRF protection**: todas as URLs de resultados são filtradas por
  `isBlockedURL`, que bloqueia esquemas não-http(s), IPs privados/loopback/
  link-local, e resolve nomes de domínio via DNS para prevenir _DNS rebinding_.

### Provedores de busca disponíveis

| Provedor       | Construtor                          | Custo                         | Observação                                  |
|----------------|-------------------------------------|-------------------------------|---------------------------------------------|
| **Wikipedia**  | `tools.NewWikipediaSearch()`        | Grátis, sem chave de API       | Padrão; busca apenas artigos da Wikipedia    |
| **LangSearch** | `tools.NewLangSearch(apiKey)`       | 100% grátis (requer chave)     | Busca web geral; `LANGSEARCH_API_KEY`       |
| **Serpstack**  | `tools.NewSerpstack(apiKey)`        | 1000 buscas/mês grátis         | `SERPSTACK_API_KEY`; tier grátis usa HTTP   |
| **DuckDuckGo** | `tools.NewDuckDuckGoSearch()`       | Grátis, sem chave              | ⚠️ Pode ser bloqueado (captcha); deprecated  |
| **Google**     | `tools.NewGoogleSearch(apiKey, cxID)`| Pago (API key + CSE ID)       | `GOOGLE_API_KEY` + `GOOGLE_CSE_ID`          |
| **Brave**      | `tools.NewBraveSearch(apiKey)`      | Pago (API key)                 | `BRAVE_API_KEY`                             |

### Criando um WebSearchTool

```go
import "github.com/rhgs/crewai-go/tools"

// Wikipedia (padrão, grátis) — provider nil usa Wikipedia.
ws := tools.NewWebSearch(nil)

// Ou com um provedor específico:
ws := tools.NewWebSearch(tools.NewBraveSearch("")) // lê BRAVE_API_KEY

// Opções:
ws := tools.NewWebSearch(tools.NewGoogleSearch("", ""),
	tools.WithMaxResults(10),
	tools.WithSearchTimeout(60*time.Second),
)

agente.WithTools(ws)
```

### Exemplos por provedor

#### Wikipedia (padrão, grátis)

```go
ws := tools.NewWebSearch(tools.NewWikipediaSearch())
// Ou em português:
ws := tools.NewWebSearch(tools.NewWikipediaSearchWithLanguage("pt"))
agente.WithTools(ws)
```

#### LangSearch (100% grátis)

```go
ws := tools.NewWebSearch(tools.NewLangSearch("")) // lê LANGSEARCH_API_KEY
agente.WithTools(ws)
```

#### Serpstack (1000/mês grátis)

```go
ws := tools.NewWebSearch(tools.NewSerpstack("")) // lê SERPSTACK_API_KEY
agente.WithTools(ws)
```

#### DuckDuckGo (opcional, pode ser bloqueado)

```go
// ⚠️ Deprecated: DuckDuckGo bloqueia acesso automatizado.
// Prefira WikipediaSearch (grátis) ou BraveSearch.
ws := tools.NewWebSearch(tools.NewDuckDuckGoSearch())
agente.WithTools(ws)
```

#### Google (API key)

```go
ws := tools.NewWebSearch(tools.NewGoogleSearch("", "")) // lê GOOGLE_API_KEY + GOOGLE_CSE_ID
agente.WithTools(ws)
```

#### Brave (API key)

```go
ws := tools.NewWebSearch(tools.NewBraveSearch("")) // lê BRAVE_API_KEY
agente.WithTools(ws)
```

### FactSource integration

O `WebSearchTool` implementa `crewai.FactSource`. Após cada `Call`
bem-sucedida, os resultados da busca são transformados em `Fact`s com
`SourceOrg`, `SourceURL` (a URL do resultado) e `PayloadHash` (SHA-256 do
payload JSON do resultado). Esses facts são anexados a `TaskOutput.Facts` e
`CrewOutput.Facts`, como qualquer outra `FactSourceTool`.

```go
crew.Guardrails = []crewai.Guardrail{
	func(_ context.Context, out *crewai.CrewOutput) error {
		return crewai.AllFactsProvenanced(out.Facts)
	},
}
```

### Proteção SSRF

O `WebSearchTool` filtra todas as URLs de resultados com `isBlockedURL`:

- **Esquemas** não-http(s) são bloqueados.
- **IPs privados** (loopback, link-local, unspecified, `127.0.0.1`, `::1`,
  `localhost`) são bloqueados.
- **Nomes de domínio** são resolvidos via DNS; se algum IP resolvido for
  privado, a URL é bloqueada. Isso previne ataques de **DNS rebinding**.
- Se a resolução DNS falhar, a URL é bloqueada por padrão (_fail-closed_).

Isso impede que um resultado de busca malicioso direcione o agente para
endpoints internos (ex.: _cloud metadata_ em `169.254.169.254`).

### WebSearcher vs WebSearchTool

| Característica        | `WebSearcher` (LLM)         | `WebSearchTool` (Tool)              |
|-----------------------|-----------------------------|-------------------------------------|
| Quem decide a busca    | Código Go (agent-driven)    | O LLM decide (via ReAct)            |
| Consome tokens        | Depende do provedor         | Não (apenas a API do provedor)      |
| Provedores            | Ollama, OpenAI, Anthropic, xAI | Wikipedia, LangSearch, Serpstack, DuckDuckGo, Google, Brave |
| FactSource            | Não                         | Sim                                 |
| Uso                   | Busca direta em Go          | Ferramenta no loop ReAct             |

- **Nomes curtos** e sem espaços (`busca_web`, não `Busca na Web`).
- **Descrições claras** dizendo *o que faz* e *qual é a entrada esperada*.
- Ferramentas devem ser **idempotentes** quando possível — o agente pode
  chamá-las mais de uma vez.
- Respeite o `context.Context` (timeouts/cancelamento) em chamadas de rede.

## Facts e proveniencia

Um **Fact** (fato) e uma peca de dado produzida por um conector deterministico,
nao pelo LLM. Sempre carrega proveniencia (organizacao fonte, URL fonte,
momento de coleta, hash do payload) para que um valor errado nunca possa ser
apresentado como um "fato que o modelo lembrou".

### O tipo Fact

```go
type Fact struct {
    Claim       string    `json:"claim"`
    SourceOrg   string    `json:"source_org"`
    SourceURL   string    `json:"source_url"`
    CollectedAt time.Time `json:"collected_at"`
    PayloadHash string    `json:"payload_hash"`
}
```

### Transformando uma tool em FactSource

Uma tool se declara como fonte de fatos usando `NewFactSourceTool`:

```go
tool := crewai.NewFactSourceTool(
    "cnpj_lookup",
    "Consulta status do CNPJ. Entrada: o numero do CNPJ.",
    func(_ context.Context, cnpj string) (string, error) {
        // chame sua API...
        return "Empresa X esta ATIVA", nil
    },
    func(_ context.Context, output string) []crewai.Fact {
        return []crewai.Fact{
            crewai.NewFact(output, "Receita Federal",
                "https://api.receita.gov.br/v1/cnpj/...", []byte(rawPayload)),
        }
    },
)
```

Apos cada `Call` bem-sucedida, o executor coleta os `Facts()` da tool e os
anexa em `TaskOutput.Facts` e `CrewOutput.Facts`.

### Regras

- O LLM NUNCA produz um Fact. Facts vem apenas de tools FactSource.
- Facts sao deduplicados por PayloadHash (primeira ocorrencia mantida).
- Tools que nao implementam FactSource contribuem com zero facts.

### Guardrails de proveniencia

Use `AllFactsProvenanced` em um guardrail para exigir que todo fato tenha
SourceURL e PayloadHash:

```go
crew.Guardrails = []crewai.Guardrail{
    func(_ context.Context, out *crewai.CrewOutput) error {
        return crewai.AllFactsProvenanced(out.Facts)
    },
}
```

Se qualquer fato nao tiver proveniencia, `Kickoff` retorna
`ErrBlockedByGuardrail`.
