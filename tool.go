package crewai

import (
	"context"
	"fmt"
)

// Tool is a capability an agent can invoke during task execution (search the
// web, query a database, do a calculation, etc.).
//
// The input and output are strings to keep the interface simple and
// interoperable with the agent's text-based (ReAct) reasoning. A tool that
// needs structured arguments should document the expected format (for example,
// a JSON object) in its Description.
type Tool interface {
	// Name is the unique, short identifier of the tool.
	Name() string
	// Description explains to the model what the tool does and how to use it.
	Description() string
	// Call runs the tool with the input provided by the agent.
	Call(ctx context.Context, input string) (string, error)
}

// FunctionTool adapts an ordinary Go function to the Tool interface.
type FunctionTool struct {
	name        string
	description string
	fn          func(ctx context.Context, input string) (string, error)
}

// NewTool creates a tool from a function.
//
//	calc := crewai.NewTool(
//	    "calculator",
//	    "Evaluates a simple arithmetic expression. Input: the expression.",
//	    func(ctx context.Context, in string) (string, error) { ... },
//	)
func NewTool(name, description string, fn func(ctx context.Context, input string) (string, error)) *FunctionTool {
	return &FunctionTool{name: name, description: description, fn: fn}
}

// Name implements Tool.
func (t *FunctionTool) Name() string { return t.name }

// Description implements Tool.
func (t *FunctionTool) Description() string { return t.description }

// Call implements Tool.
func (t *FunctionTool) Call(ctx context.Context, input string) (string, error) {
	if t.fn == nil {
		return "", fmt.Errorf("tool %q: nil function", t.name)
	}
	return t.fn(ctx, input)
}

// findTool looks up a tool by name (case-insensitive at the edges) within a
// list.
func findTool(tools []Tool, name string) (Tool, bool) {
	for _, t := range tools {
		if t.Name() == name {
			return t, true
		}
	}
	return nil, false
}
