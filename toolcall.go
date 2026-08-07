package crewai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ToolMode selects the tool execution strategy for an agent.
type ToolMode string

const (
	// ToolModeReact uses text-based ReAct parsing (default, backward compatible).
	ToolModeReact ToolMode = "react"
	// ToolModeNative uses the provider's native function calling API.
	ToolModeNative ToolMode = "native"
)

// ToolCallingLLM is an optional capability that an LLM may implement when
// the underlying provider supports native tool calling (function calling).
// When an Agent's ToolMode is "native" and its LLM implements this
// interface, the executor uses native tool calling instead of ReAct text
// parsing. Providers that do not support tool calling do not implement this
// interface, and the executor returns ErrNativeToolsUnsupported.
type ToolCallingLLM interface {
	LLM
	// CallWithTools sends the conversation with a list of available tools
	// and returns the model's response, which may contain tool_calls.
	CallWithTools(ctx context.Context, messages []Message, tools []ToolSpec) (*ToolCallResponse, error)
}

// ToolSpec describes a tool the model can invoke via native function calling.
// It mirrors the structure used by OpenAI and Ollama: a type ("function") and
// a Function block with name, description, and a JSON Schema for parameters.
type ToolSpec struct {
	Type     string       `json:"type"` // always "function"
	Function ToolFunction `json:"function"`
}

// ToolFunction is the function metadata the model sees.
type ToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"` // JSON Schema
}

// ToolCall is a tool invocation requested by the model in its response.
type ToolCall struct {
	// ID is the tool call identifier (some providers require it to match
	// the result back to the call). May be empty for providers that do not
	// assign IDs (e.g. Ollama).
	ID string `json:"id,omitempty"`
	// Function carries the tool name and the arguments as raw JSON.
	Function ToolCallFunction `json:"function"`
}

// ToolCallFunction carries the name and arguments of a requested tool call.
// Arguments is raw JSON (not a string) — this matches the Ollama wire format
// and differs from OpenAI, where arguments arrive as a JSON string. Each
// provider client normalizes to raw JSON before returning.
type ToolCallFunction struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ToolCallResponse is the result of a CallWithTools invocation. It carries
// the text content (which may be empty if the model only called tools) and
// any tool calls the model requested. When ToolCalls is empty, the model is
// done and Content holds the final answer.
type ToolCallResponse struct {
	Content   string     // free text from the model; may be empty
	ToolCalls []ToolCall // tool invocations; empty = model is done
}

// ToolTrace records a single tool invocation during native tool execution.
type ToolTrace struct {
	Tool     string          `json:"tool"`
	Args     json.RawMessage `json:"args,omitempty"`
	Output   string          `json:"output"`
	Failed   bool            `json:"failed"`
	Duration time.Duration   `json:"duration"`
}

// Security constants for native tool calling.
const (
	// MaxToolArgsBytes limits the size of tool call arguments to prevent
	// denial-of-service via extremely large JSON payloads. Default: 1 MiB.
	MaxToolArgsBytes = 1 << 20

	// MaxToolArgsDepth limits JSON nesting depth in tool call arguments to
	// prevent billion-laughs attacks and stack overflow. Default: 100.
	MaxToolArgsDepth = 100

	// MaxToolOutputBytes limits the size of a tool's output before
	// truncation. Default: 1 MiB.
	MaxToolOutputBytes = 1 << 20

	// MaxProviderResponseBytes limits the HTTP response body size from
	// LLM providers to prevent OOM. Default: 10 MiB.
	MaxProviderResponseBytes = 10 << 20
)

// toToolSpecs converts the crewai Tool interface to ToolSpec for the wire.
func toToolSpecs(tools []Tool) []ToolSpec {
	out := make([]ToolSpec, len(tools))
	for i, t := range tools {
		out[i] = ToolSpec{
			Type: "function",
			Function: ToolFunction{
				Name:        t.Name(),
				Description: t.Description(),
				Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
			},
		}
	}
	return out
}

// validateToolArgs checks that the raw JSON arguments are within safe limits.
func validateToolArgs(args json.RawMessage) error {
	if len(args) > MaxToolArgsBytes {
		return fmt.Errorf("tool arguments exceed %d bytes", MaxToolArgsBytes)
	}
	if depth := jsonDepth(args); depth > MaxToolArgsDepth {
		return fmt.Errorf("tool arguments nesting depth %d exceeds limit %d", depth, MaxToolArgsDepth)
	}
	return nil
}

// jsonDepth computes the maximum nesting depth of a JSON document.
func jsonDepth(data []byte) int {
	var depth, max int
	inString := false
	for i := 0; i < len(data); i++ {
		b := data[i]
		if inString {
			if b == '\\' && i+1 < len(data) {
				i++ // skip escaped char
			} else if b == '"' {
				inString = false
			}
			continue
		}
		switch b {
		case '"':
			inString = true
		case '{', '[':
			depth++
			if depth > max {
				max = depth
			}
		case '}', ']':
			depth--
		}
	}
	return max
}

// truncateToolOutput truncates a tool's output to MaxToolOutputBytes.
func truncateToolOutput(s string) string {
	if len(s) <= MaxToolOutputBytes {
		return s
	}
	return s[:MaxToolOutputBytes] + "\n... [output truncated, exceeded 1 MiB]"
}

// executeTaskWithTools runs the native tool-calling loop. It sends the
// conversation with tools to the model, executes any tool calls, feeds
// results back as role:"tool" messages, and repeats until the model stops
// requesting tools or the iteration budget is exhausted.
//
// This is the native counterpart to the ReAct loop in executor.go. It is
// only called when Agent.ToolMode == "native" and the LLM implements
// ToolCallingLLM.
func executeTaskWithTools(ctx context.Context, a *Agent, t *Task, contextText string, log Logger) (string, []ToolTrace, []Fact, error) {
	tcll, ok := a.LLM.(ToolCallingLLM)
	if !ok {
		return "", nil, nil, ErrNativeToolsUnsupported
	}

	tools := effectiveTools(a, t)
	if len(tools) == 0 {
		// No tools: fall through to a plain call (same as ReAct no-tools path).
		out, err := a.LLM.Call(ctx, []Message{
			SystemMessage(buildSystemPrompt(a)),
			UserMessage(buildTaskPrompt(t, contextText)),
		})
		return strings.TrimSpace(stripFinalAnswer(out)), nil, nil, err
	}

	specs := toToolSpecs(tools)
	maxIter := a.MaxIterations
	if maxIter <= 0 {
		maxIter = defaultMaxIterations
	}

	messages := []Message{
		SystemMessage(buildSystemPrompt(a)),
		UserMessage(buildTaskPrompt(t, contextText)),
	}

	var traces []ToolTrace
	var collectedFacts []Fact

	for i := 0; i < maxIter; i++ {
		select {
		case <-ctx.Done():
			return "", traces, collectedFacts, ctx.Err()
		default:
		}

		resp, err := tcll.CallWithTools(ctx, messages, specs)
		if err != nil {
			return "", traces, collectedFacts, fmt.Errorf("agent %q: %w", a.Role, err)
		}

		// No tool calls: the model is done, content is the final answer.
		if len(resp.ToolCalls) == 0 {
			log.Debugf("[%s] native tool loop done after %d iterations", a.Role, i+1)
			return strings.TrimSpace(resp.Content), traces, collectedFacts, nil
		}

		// Append the assistant message (with tool_calls) to the conversation.
		messages = append(messages, Message{
			Role:      RoleAssistant,
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		// Execute each requested tool call.
		for _, tc := range resp.ToolCalls {
			trace := ToolTrace{
				Tool: tc.Function.Name,
				Args: tc.Function.Arguments,
			}
			start := time.Now()

			if err := validateToolArgs(tc.Function.Arguments); err != nil {
				trace.Failed = true
				trace.Output = fmt.Sprintf("Error: %v", err)
				trace.Duration = time.Since(start)
				traces = append(traces, trace)
				messages = append(messages, Message{
					Role:     RoleTool,
					ToolName: tc.Function.Name,
					Content:  trace.Output,
				})
				continue
			}

			tool, found := findTool(tools, tc.Function.Name)
			if !found {
				trace.Failed = true
				trace.Output = fmt.Sprintf("Error: tool %q does not exist. Available tools: %s.",
					tc.Function.Name, strings.Join(toolNames(tools), ", "))
				trace.Duration = time.Since(start)
				traces = append(traces, trace)
				messages = append(messages, Message{
					Role:     RoleTool,
					ToolName: tc.Function.Name,
					Content:  trace.Output,
				})
				continue
			}

			log.Infof("[%s] native tool call: %s(%s)", a.Role, tc.Function.Name, string(tc.Function.Arguments))

			result, err := tool.Call(ctx, string(tc.Function.Arguments))
			trace.Duration = time.Since(start)
			if err != nil {
				trace.Failed = true
				trace.Output = fmt.Sprintf("Error running tool %q: %v", tc.Function.Name, err)
			} else {
				trace.Output = truncateToolOutput(result)
				// Collect facts from FactSource tools after a successful call.
				if fs, ok := tool.(FactSource); ok {
					collectedFacts = dedupFacts(collectedFacts, fs.Facts())
				}
			}
			traces = append(traces, trace)

			messages = append(messages, Message{
				Role:     RoleTool,
				ToolName: tc.Function.Name,
				Content:  trace.Output,
			})
		}
	}

	return "", traces, collectedFacts, fmt.Errorf("agent %q: %w (%d iterations)", a.Role, ErrMaxIterations, maxIter)
}
