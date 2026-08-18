package crewai

import (
	"encoding/json"
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

// structuredSystemInstruction is appended to the system prompt when the
// task requires structured (JSON) output.
const structuredSystemInstruction = "You MUST respond with a single, valid JSON object that satisfies the provided JSON Schema. Do NOT include any explanation, markdown, or surrounding text. Output ONLY the JSON."

// toolCallStructuredInstruction is appended when StructuredOutput is in
// tool-call mode. It directs the model to use the synthetic
// `emit_result` tool instead of replying with plain JSON text — the
// reliable path for providers that do not support structured outputs
// via the `format` parameter (e.g. Ollama Cloud).
const toolCallStructuredInstruction = "To submit your final answer you MUST call the `emit_result` tool exactly once and pass your answer as the tool's arguments. Do not return plain text, do not wrap the answer in markdown, and do not call any other tool."

// buildStructuredPrompt assembles the user message for a structured-output
// task, including the task description and the JSON Schema.
func buildStructuredPrompt(t *Task, context string, schema json.RawMessage) string {
	var b strings.Builder
	b.WriteString(buildTaskPrompt(t, context))
	b.WriteString("\n\nYou must reply ONLY with a JSON object that conforms to the following JSON Schema. No markdown, no prose, no surrounding text.\n\n")
	b.WriteString("JSON Schema:\n")
	b.Write(schema)
	b.WriteString("\n")
	return strings.TrimSpace(b.String())
}

// buildPlanPrompt assembles the user message that asks the agent to produce a
// numbered plan before acting. The plan is injected as context for the
// execution phase, keeping the agent focused.
func buildPlanPrompt(t *Task, context string) string {
	var b strings.Builder
	b.WriteString("You are about to work on the following task:\n\n")
	b.WriteString(t.Description)
	b.WriteString("\n")
	if t.ExpectedOutput != "" {
		b.WriteString("\nExpected output:\n")
		b.WriteString(t.ExpectedOutput)
		b.WriteString("\n")
	}
	if strings.TrimSpace(context) != "" {
		b.WriteString("\nContext from previous tasks:\n")
		b.WriteString(context)
		b.WriteString("\n")
	}
	b.WriteString("\nBefore acting, create a concise, numbered plan of the steps you will take. ")
	b.WriteString("Consider which tools you have available and how to use them.\n\n")
	b.WriteString("Respond ONLY with the plan, numbered 1-N. No other text.")
	return strings.TrimSpace(b.String())
}

// buildEvaluationPrompt assembles the user message for the evaluation phase.
// It asks the evaluator to score the actual output against the expected output
// and return a JSON object with a score and feedback.
func buildEvaluationPrompt(t *Task, actualOutput string) string {
	var b strings.Builder
	b.WriteString("You are an evaluator. Your job is to assess whether the output meets the expected quality and completeness for the task.\n\n")
	b.WriteString("Task:\n")
	b.WriteString(t.Description)
	b.WriteString("\n")
	if t.ExpectedOutput != "" {
		b.WriteString("\nExpected output:\n")
		b.WriteString(t.ExpectedOutput)
		b.WriteString("\n")
	}
	b.WriteString("\nActual output:\n")
	b.WriteString(actualOutput)
	b.WriteString("\n\n")
	b.WriteString("Evaluate the actual output against the expected output. Respond ONLY with a JSON object:\n")
	b.WriteString("{\n  \"score\": <integer 0-100>,\n  \"feedback\": \"<brief explanation of issues or confirmation of quality>\"\n}\n\n")
	b.WriteString("Scoring guide:\n")
	b.WriteString("- 90-100: excellent, fully meets expectations\n")
	b.WriteString("- 70-89: good, minor issues\n")
	b.WriteString("- 50-69: partial, significant gaps\n")
	b.WriteString("- 0-49: poor, major problems")
	return strings.TrimSpace(b.String())
}

// buildRefinePrompt assembles the user message for the refine phase. It
// injects the evaluator's feedback so the agent can revise its output.
func buildRefinePrompt(feedback string, score, threshold int) string {
	var b strings.Builder
	b.WriteString("The evaluator provided the following feedback on your previous output:\n\n")
	b.WriteString(feedback)
	b.WriteString("\n\n")
	fmt.Fprintf(&b, "Score: %d/%d\n\n", score, threshold)
	b.WriteString("Please revise your output to address the feedback. Re-execute the task with the improvements in mind.")
	return strings.TrimSpace(b.String())
}

// buildEvalRepairPrompt assembles the user message for a retry of the
// evaluation phase after the evaluator returned a non-JSON response.
func buildEvalRepairPrompt(raw string) string {
	var b strings.Builder
	b.WriteString("Your previous evaluation response was not valid JSON with a \"score\" field.\n\n")
	b.WriteString("Your previous response was:\n")
	b.WriteString(raw)
	b.WriteString("\n\n")
	b.WriteString("Respond ONLY with a JSON object:\n")
	b.WriteString("{\n  \"score\": <integer 0-100>,\n  \"feedback\": \"<brief explanation>\"\n}")
	return strings.TrimSpace(b.String())
}

// buildRepairPrompt assembles the user message for a repair attempt. It
// includes the previous (invalid) output, the validation errors, and the
// schema, then asks the model to fix and return only the corrected JSON.
func buildRepairPrompt(previousOutput string, valErr error, schema json.RawMessage) string {
	var b strings.Builder
	b.WriteString("Your previous response did not validate against the JSON Schema.\n\n")
	b.WriteString("Your previous response was:\n")
	b.WriteString(previousOutput)
	b.WriteString("\n\nValidation errors:\n")
	b.WriteString(valErr.Error())
	b.WriteString("\n\nThe JSON Schema is:\n")
	b.Write(schema)
	b.WriteString("\n\nFix the issues and reply ONLY with the corrected JSON. No markdown, no prose, no surrounding text.")
	return b.String()
}

// buildToolCallRepairPrompt assembles the user message for a repair
// attempt in tool-call structured output mode. The feedback explains
// why the previous emit_result call was rejected (or why the model
// produced plain text instead of calling the tool), and the same
// schema is included for reference.
func buildToolCallRepairPrompt(feedback string, schema json.RawMessage) string {
	var b strings.Builder
	b.WriteString(feedback)
	b.WriteString("\n\nCall the `emit_result` tool exactly once with your corrected answer as arguments. The tool's parameters must conform to the following JSON Schema:\n\n")
	b.Write(schema)
	b.WriteString("\n")
	return b.String()
}
