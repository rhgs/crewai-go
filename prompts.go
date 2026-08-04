package crewai

import (
	"fmt"
	"strings"
)

// buildSystemPrompt assembles the system message that defines the agent's persona.
func buildSystemPrompt(a *Agent) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are %s.\n", a.Role)
	if a.Backstory != "" {
		fmt.Fprintf(&b, "%s\n", a.Backstory)
	}
	if a.Goal != "" {
		fmt.Fprintf(&b, "\nYour personal goal is: %s\n", a.Goal)
	}
	return strings.TrimSpace(b.String())
}

// buildToolInstructions describes the available tools and the ReAct protocol
// the model should follow to use them.
func buildToolInstructions(tools []Tool) string {
	if len(tools) == 0 {
		return ""
	}
	var b strings.Builder
	names := make([]string, 0, len(tools))
	b.WriteString("\nYou have access to the following tools:\n\n")
	for _, t := range tools {
		fmt.Fprintf(&b, "- %s: %s\n", t.Name(), t.Description())
		names = append(names, t.Name())
	}
	b.WriteString("\nTo use a tool, respond EXACTLY in this format:\n\n")
	b.WriteString("Thought: your reasoning about what to do\n")
	fmt.Fprintf(&b, "Action: the tool name, exactly one of [%s]\n", strings.Join(names, ", "))
	b.WriteString("Action Input: the input for the tool\n\n")
	b.WriteString("The system will respond with:\n\n")
	b.WriteString("Observation: the tool's result\n\n")
	b.WriteString("Repeat this cycle (Thought/Action/Action Input) as many times as needed. ")
	b.WriteString("When you have the final answer, respond EXACTLY in this format:\n\n")
	b.WriteString("Thought: I now know the final answer\n")
	b.WriteString("Final Answer: the complete, final answer to the task\n")
	return b.String()
}

// buildTaskPrompt assembles the user message with the task to be performed.
func buildTaskPrompt(t *Task, context string) string {
	var b strings.Builder
	b.WriteString("Current task:\n")
	b.WriteString(t.Description)
	b.WriteString("\n")
	if t.ExpectedOutput != "" {
		b.WriteString("\nExpected format/goal of the answer:\n")
		b.WriteString(t.ExpectedOutput)
		b.WriteString("\n")
	}
	if strings.TrimSpace(context) != "" {
		b.WriteString("\nContext from previous tasks:\n")
		b.WriteString(context)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

// noToolFinalInstruction is added when the agent has no tools, to reinforce
// that it should answer directly.
const noToolFinalInstruction = "\nAnswer directly with the best possible answer for the task, with no preamble."
