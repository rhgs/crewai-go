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
