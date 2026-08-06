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

## Boas práticas

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
