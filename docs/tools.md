# Tools

> **Languages:** **English** (current) · [Português](pt-BR/tools.md)

A **Tool** gives "hands" to an agent: it lets the agent perform actions —
calculate, query an API, read a file — during reasoning.

## The interface

```go
type Tool interface {
	Name() string
	Description() string
	Call(ctx context.Context, input string) (string, error)
}
```

Input and output are `string` to match the agent's text-based reasoning. If
your tool needs structured arguments, document the expected format (e.g. a
JSON object) in its `Description`.

## Creating a tool from a function

```go
weather := crewai.NewTool(
	"weather",
	"Looks up the weather for a city. Input: the city name.",
	func(ctx context.Context, city string) (string, error) {
		// call your API here...
		return fmt.Sprintf("Sunny in %s, 27°C", city), nil
	},
)
```

## Built-in tools (the `tools` package)

```go
import "github.com/rhgs/crewai-go/tools"

tools.Calculator()      // evaluates "2 + 2 * (3 - 1)" — offline and safe
tools.CurrentTime("")   // current date/time (time package layout; "" = RFC3339)
tools.WordCount()       // counts words and characters in the text
```

## Attaching tools

To an agent (available for all its tasks):

```go
agent.WithTools(tools.Calculator(), weather)
```

To a specific task (overrides the agent's tools for that task only):

```go
task.Tools = []crewai.Tool{weather}
```

## The ReAct protocol

When an agent has tools, it follows a **Reasoning + Action** cycle. The model
is instructed to respond in this format:

```
Thought: I need to calculate the total
Action: calculator
Action Input: 1500 * 1.12
```

The framework runs the tool and returns:

```
Observation: 1680
```

The cycle repeats until the model concludes:

```
Thought: now I know the answer
Final Answer: The final value is $1,680.00.
```

### Robustness

- If the model does **not** follow the protocol, the output is treated as the
  final answer (instead of stalling).
- If the model asks for a **non-existent** tool, the framework returns an error
  `Observation` listing the valid tools, and the agent tries again.
- Errors returned by a tool become an error `Observation` — the agent can react
  to them.
- The loop stops at `MaxIterations` (default 15), returning `ErrMaxIterations`.

## Best practices

- **Short names** without spaces (`web_search`, not `Web Search`).
- **Clear descriptions** stating *what it does* and *what input is expected*.
- Tools should be **idempotent** when possible — the agent may call them more
  than once.
- Respect `context.Context` (timeouts/cancellation) in network calls.

## Facts & provenance

A **Fact** is a piece of data produced by a deterministic connector tool, not
by the LLM. It always carries provenance (source organization, source URL,
collection time, payload hash) so a wrong value can never be presented as a
"fact the model remembered".

### The Fact type

```go
type Fact struct {
    Claim       string    `json:"claim"`
    SourceOrg   string    `json:"source_org"`
    SourceURL   string    `json:"source_url"`
    CollectedAt time.Time `json:"collected_at"`
    PayloadHash string    `json:"payload_hash"`
}
```

### Making a tool a FactSource

A tool declares itself as a fact source by using `NewFactSourceTool`:

```go
tool := crewai.NewFactSourceTool(
    "cnpj_lookup",
    "Looks up CNPJ status. Input: the CNPJ number.",
    func(_ context.Context, cnpj string) (string, error) {
        // call your API...
        return "Company X is ATIVA", nil
    },
    func(_ context.Context, output string) []crewai.Fact {
        return []crewai.Fact{
            crewai.NewFact(output, "Receita Federal",
                "https://api.receita.gov.br/v1/cnpj/...", []byte(rawPayload)),
        }
    },
)
```

After each successful `Call`, the executor collects the tool's `Facts()` and
attaches them to `TaskOutput.Facts` and `CrewOutput.Facts`.

### Rules

- The LLM NEVER produces a Fact. Facts come only from FactSource tools.
- Facts are deduplicated by PayloadHash (first occurrence kept).
- Tools not implementing FactSource contribute zero facts.

### Provenance guardrails

Use `AllFactsProvenanced` in a guardrail to enforce that every fact has
SourceURL and PayloadHash:

```go
crew.Guardrails = []crewai.Guardrail{
    func(_ context.Context, out *crewai.CrewOutput) error {
        return crewai.AllFactsProvenanced(out.Facts)
    },
}
```

If any fact lacks provenance, `Kickoff` returns `ErrBlockedByGuardrail`.
