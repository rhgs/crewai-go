# Agents

> **Languages:** **English** (current) · [Português](pt-BR/agents.md)

An **Agent** is an autonomous worker. It combines a _persona_ (role, goal,
backstory), an **LLM** for reasoning, and optionally **tools** to act.

## Creating an agent

```go
agent := crewai.NewAgent(
	"Senior Researcher",              // Role
	"Uncover actionable insights",  // Goal
	"You have 20 years of experience...", // Backstory
	llm,                                // LLM
)
```

Or fill the struct directly for more control:

```go
agent := &crewai.Agent{
	Role:          "Senior Researcher",
	Goal:          "Uncover actionable insights",
	Backstory:     "You have 20 years of experience in data analysis.",
	LLM:           llm,
	MaxIterations: 10,   // limit of reasoning cycles per task
	Tools:         []crewai.Tool{ /* ... */ },
}
```

## Fields

| Field             | Type           | Description |
|-------------------|----------------|-------------|
| `Role`            | `string`       | The agent's role. **Required.** |
| `Goal`            | `string`       | Personal goal that guides decisions. |
| `Backstory`       | `string`       | Context and personality. |
| `LLM`             | `crewai.LLM`   | The language model. **Required.** |
| `Tools`           | `[]crewai.Tool`| Tools available for any task. |
| `MaxIterations`   | `int`          | Max reasoning/tool cycles per task (default 15). |
| `AllowDelegation` | `bool`         | Marks the agent as eligible to manage/delegate. |

## Adding tools

```go
agent.WithTools(tools.Calculator(), myTool)
```

`WithTools` is chainable and returns the agent itself:

```go
agent := crewai.NewAgent("Analyst", "...", "...", llm).
	WithTools(tools.Calculator())
```

## Running a standalone agent

Outside a crew (useful in tests or simple flows):

```go
output, err := agent.Execute(context.Background(), task)
```

## How the agent thinks

- **Without tools:** the agent makes a single LLM call and returns the answer.
- **With tools:** the agent enters a ReAct loop — thinks, picks a tool, observes
  the result, and repeats until it reaches a `Final Answer` or hits
  `MaxIterations`. See [tools.md](tools.md).

## Best practices

- Give a **specific role** ("API Technical Writer" rather than "Writer").
- The **goal** should be measurable and outcome-oriented.
- Use the **backstory** to calibrate tone and level of detail.
- Tune `MaxIterations` for tasks that use many tools.
