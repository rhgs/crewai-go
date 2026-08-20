package crewai

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// executeTask is the dispatch entry point for task execution. It resolves the
// loop (task-level takes precedence over agent-level) and delegates to it, or
// falls back to the default ReAct/structured executor.
func executeTask(ctx context.Context, a *Agent, t *Task, contextText string, log *slog.Logger) (string, []Fact, error) {
	if a.LLM == nil {
		return "", nil, ErrNoLLM
	}

	// Check for a loop: task-level takes precedence over agent-level.
	if t != nil && t.Loop != nil {
		return t.Loop.Run(ctx, a, t, contextText, log)
	}
	if a.Loop != nil {
		return a.Loop.Run(ctx, a, t, contextText, log)
	}

	return executeTaskDefault(ctx, a, t, contextText, log)
}

// executeTaskDefault runs the reasoning loop (ReAct) of an agent for a task.
// It returns the final answer string, collected facts (from FactSource
// tools), and an error. It does NOT check for a configured loop; that is the
// responsibility of executeTask.
func executeTaskDefault(ctx context.Context, a *Agent, t *Task, contextText string, log *slog.Logger) (string, []Fact, error) {
	if a.LLM == nil {
		return "", nil, ErrNoLLM
	}
	if t != nil && t.Structured != nil {
		out, err := executeStructured(ctx, a, t, contextText, log)
		return out, nil, err
	}

	// Native tool calling path.
	if a.ToolMode == ToolModeNative {
		result, traces, facts, err := executeTaskWithTools(ctx, a, t, contextText, log)
		if err != nil {
			return "", nil, err
		}
		// Attach traces to the task output via a side channel.
		if t != nil {
			t.setToolTraces(traces)
		}
		return result, facts, nil
	}

	tools := effectiveTools(a, t)
	maxIter := a.MaxIterations
	if maxIter <= 0 {
		maxIter = defaultMaxIterations
	}

	system := buildSystemPrompt(a)
	if len(tools) > 0 {
		system += buildToolInstructions(tools)
	} else {
		system += noToolFinalInstruction
	}

	messages := []Message{
		SystemMessage(system),
		UserMessage(buildTaskPrompt(t, contextText)),
	}

	// No tools: a single call is enough.
	if len(tools) == 0 {
		out, err := a.LLM.Call(ctx, messages)
		if err != nil {
			return "", nil, fmt.Errorf("agent %q: %w", a.Role, err)
		}
		return strings.TrimSpace(stripFinalAnswer(out)), nil, nil
	}

	var collectedFacts []Fact

	for i := 0; i < maxIter; i++ {
		select {
		case <-ctx.Done():
			return "", collectedFacts, ctx.Err()
		default:
		}

		out, err := a.LLM.Call(ctx, messages)
		if err != nil {
			return "", collectedFacts, fmt.Errorf("agent %q: %w", a.Role, err)
		}
		out = strings.TrimSpace(out)
		log.DebugContext(ctx, "agent thought", "agent", a.Role, "output", out)

		if answer, ok := parseFinalAnswer(out); ok {
			return strings.TrimSpace(answer), collectedFacts, nil
		}

		action, input, ok := parseAction(out)
		if !ok {
			// The model did not follow the protocol: we treat the output as
			// the final answer so execution doesn't stall.
			return strings.TrimSpace(out), collectedFacts, nil
		}

		messages = append(messages, AssistantMessage(out))

		tool, found := findTool(tools, action)
		var observation string
		if !found {
			observation = fmt.Sprintf("Error: tool %q does not exist. Available tools: %s.",
				action, strings.Join(toolNames(tools), ", "))
		} else {
			log.InfoContext(ctx, "tool invoked", "agent", a.Role, "tool", action, "input", input)
			toolStart := time.Now()
			result, err := tool.Call(ctx, input)
			toolDur := time.Since(toolStart)
			if err != nil {
				observation = fmt.Sprintf("Error running tool %q: %v", action, err)
			} else {
				// Bound observation size the same way native tool calling
				// does, so a single runaway tool cannot blow up the next
				// prompt (or the process heap).
				observation = truncateToolOutput(result)
				// Collect facts from FactSource tools after a successful call.
				if fs, ok := tool.(FactSource); ok {
					collectedFacts = dedupFacts(collectedFacts, fs.Facts())
				}
			}
			emitProgress(ctx, Progress{
				Agent:    a.Role,
				Event:    "tool_invoked",
				Tool:     action,
				Duration: toolDur,
			})
		}

		messages = append(messages, UserMessage("Observation: "+observation))
	}

	return "", collectedFacts, fmt.Errorf("agent %q: %w (%d iterations)", a.Role, ErrMaxIterations, maxIter)
}

// effectiveTools combines the agent's tools with the task-specific ones.
func effectiveTools(a *Agent, t *Task) []Tool {
	if t != nil && len(t.Tools) > 0 {
		// Task tools take precedence and replace the agent's tools.
		return t.Tools
	}
	return a.Tools
}

func toolNames(tools []Tool) []string {
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name()
	}
	return names
}

// parseFinalAnswer extracts the text after "Final Answer:".
func parseFinalAnswer(s string) (string, bool) {
	idx := indexMarker(s, "final answer:")
	if idx < 0 {
		return "", false
	}
	return s[idx:], true
}

// stripFinalAnswer removes an eventual "Final Answer:" prefix from a direct
// answer (used in the no-tools path).
func stripFinalAnswer(s string) string {
	if ans, ok := parseFinalAnswer(s); ok {
		return ans
	}
	return s
}

// parseAction extracts the tool name ("Action:") and its input
// ("Action Input:") from a model response.
func parseAction(s string) (action, input string, ok bool) {
	actIdx := indexMarker(s, "action:")
	if actIdx < 0 {
		return "", "", false
	}
	rest := s[actIdx:]

	inputIdx := indexMarker(rest, "action input:")
	if inputIdx < 0 {
		// There is only Action, with no Action Input.
		action = strings.TrimSpace(firstLine(rest))
		return action, "", action != ""
	}

	// The action is whatever is between "Action:" and "Action Input:".
	actionPart := rest[:markerStart(rest, "action input:")]
	action = strings.TrimSpace(firstLine(actionPart))

	inputPart := rest[inputIdx:]
	// The input goes up to the next "Observation:" or the end of the text.
	if obs := markerStart(inputPart, "observation:"); obs >= 0 {
		inputPart = inputPart[:obs]
	}
	input = strings.TrimSpace(inputPart)
	return action, input, action != ""
}

// indexMarker returns the index of the start of the CONTENT right after the
// marker (case-insensitive), or -1 if not found.
func indexMarker(s, marker string) int {
	i := strings.Index(strings.ToLower(s), marker)
	if i < 0 {
		return -1
	}
	return i + len(marker)
}

// markerStart returns the index of the START of the marker (case-insensitive).
func markerStart(s, marker string) int {
	return strings.Index(strings.ToLower(s), marker)
}

// firstLine returns the first non-empty line of a chunk.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			return line
		}
	}
	return ""
}
