package crewai

import (
	"context"
	"fmt"
	"strings"
)

// executeTask runs the reasoning loop (ReAct) of an agent for a task.
func executeTask(ctx context.Context, a *Agent, t *Task, contextText string, log Logger) (string, error) {
	if a.LLM == nil {
		return "", ErrNoLLM
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
			return "", fmt.Errorf("agent %q: %w", a.Role, err)
		}
		return strings.TrimSpace(stripFinalAnswer(out)), nil
	}

	for i := 0; i < maxIter; i++ {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		out, err := a.LLM.Call(ctx, messages)
		if err != nil {
			return "", fmt.Errorf("agent %q: %w", a.Role, err)
		}
		out = strings.TrimSpace(out)
		log.Debugf("[%s] thought:\n%s", a.Role, out)

		if answer, ok := parseFinalAnswer(out); ok {
			return strings.TrimSpace(answer), nil
		}

		action, input, ok := parseAction(out)
		if !ok {
			// The model did not follow the protocol: we treat the output as
			// the final answer so execution doesn't stall.
			return strings.TrimSpace(out), nil
		}

		messages = append(messages, AssistantMessage(out))

		tool, found := findTool(tools, action)
		var observation string
		if !found {
			observation = fmt.Sprintf("Error: tool %q does not exist. Available tools: %s.",
				action, strings.Join(toolNames(tools), ", "))
		} else {
			log.Infof("[%s] usando ferramenta %q com entrada: %s", a.Role, action, input)
			result, err := tool.Call(ctx, input)
			if err != nil {
				observation = fmt.Sprintf("Error running tool %q: %v", action, err)
			} else {
				observation = result
			}
		}

		messages = append(messages, UserMessage("Observation: "+observation))
	}

	return "", fmt.Errorf("agent %q: %w (%d iterations)", a.Role, ErrMaxIterations, maxIter)
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
