package crewai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
)

// StructuredOutput configures a Task to require JSON output validated
// against a JSON Schema. When non-nil on a Task, the executor instructs
// the model to reply with JSON only, validates the response in Go, and
// retries (repairs) up to RepairMax times if validation fails.
//
// When ToolCall is true, the executor instead declares a synthetic
// tool named "emit_result" with parameters equal to Schema, asks the
// model to call it once, and parses the tool call arguments as the
// final JSON output. This is the reliable path for providers that do
// not support structured outputs via the `format` parameter (notably
// Ollama Cloud); native tool calling is widely supported and the
// arguments are returned pre-parsed by every provider in this repo.
type StructuredOutput struct {
	// Schema is the JSON Schema the output must satisfy. Required.
	// It is stored as raw JSON so the caller can pass any schema object
	// marshaled with encoding/json.
	Schema json.RawMessage

	// RepairMax is the maximum number of repair attempts after the
	// initial call. If <= 0, it defaults to 2.
	RepairMax int

	// ToolCall switches the extraction strategy from JSON-only text
	// prompts to a synthetic emit_result tool call. Requires the
	// agent's LLM to implement ToolCallingLLM; otherwise
	// ErrToolCallStructuredUnsupported is returned.
	ToolCall bool
}

// NewStructuredOutput creates a StructuredOutput from a schema (any value
// marshalable by encoding/json) and optional functional options.
func NewStructuredOutput(schema any, opts ...func(*StructuredOutput)) (*StructuredOutput, error) {
	raw, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("crewai: marshaling schema: %w", err)
	}
	s := &StructuredOutput{Schema: raw}
	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

// WithRepairMax sets the repair attempt limit on a StructuredOutput.
func WithRepairMax(n int) func(*StructuredOutput) {
	return func(s *StructuredOutput) { s.RepairMax = n }
}

// WithToolCall enables the emit_result tool-call extraction strategy.
// Equivalent to setting StructuredOutput.ToolCall = true. Default is
// false (backward compatible JSON-only mode).
func WithToolCall() func(*StructuredOutput) {
	return func(s *StructuredOutput) { s.ToolCall = true }
}

// emitResultToolName is the fixed name of the synthetic tool used
// when ToolCall is enabled. The tool exists ONLY in the
// ToolSpec passed to the model; the executor never invokes it as a
// real crewai.Tool.
const emitResultToolName = "emit_result"

// defaultRepairMax is the repair limit used when RepairMax <= 0.
const defaultRepairMax = 2

// executeStructured runs the structured-output loop for a task whose
// Structured field is non-nil. It validates the output against the
// JSON Schema, and retries up to RepairMax times if validation
// fails. Two extraction strategies are supported:
//
//  1. JSON-only (default): instructs the model to reply with JSON
//     text via LLM.Call.
//  2. Tool-call (ToolCall == true): declares a synthetic tool named
//     "emit_result" with parameters = Schema and parses the model's
//     call arguments as the output. Requires the LLM to implement
//     ToolCallingLLM; returns ErrToolCallStructuredUnsupported
//     otherwise.
//
// It returns the canonicalized JSON string on success or a sentinel
// error on failure.
func executeStructured(ctx context.Context, a *Agent, t *Task, contextText string, log *slog.Logger) (string, error) {
	if a.LLM == nil {
		return "", ErrNoLLM
	}

	repairMax := t.Structured.RepairMax
	if repairMax <= 0 {
		repairMax = defaultRepairMax
	}

	schema := t.Structured.Schema
	if len(schema) == 0 {
		return "", fmt.Errorf("%w: empty schema", ErrInvalidOutput)
	}

	// Pre-validate the schema itself; programmer error if invalid.
	var probe any
	if err := json.Unmarshal(schema, &probe); err != nil {
		return "", fmt.Errorf("%w: invalid schema JSON: %v", ErrInvalidOutput, err)
	}

	if t.Structured.ToolCall {
		return executeStructuredToolCall(ctx, a, t, contextText, schema, repairMax, log)
	}
	return executeStructuredJSON(ctx, a, t, contextText, schema, repairMax, log)
}

// executeStructuredJSON is the legacy path: instructions ask the
// model to reply with JSON only.
func executeStructuredJSON(ctx context.Context, a *Agent, t *Task, contextText string, schema json.RawMessage, repairMax int, log *slog.Logger) (string, error) {
	system := buildSystemPrompt(a) + "\n\n" + structuredSystemInstruction
	taskPrompt := buildStructuredPrompt(t, contextText, schema)

	messages := []Message{
		SystemMessage(system),
		UserMessage(taskPrompt),
	}

	attempt := 0
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		out, err := a.LLM.Call(ctx, messages)
		if err != nil {
			return "", fmt.Errorf("agent %q: %w", a.Role, err)
		}

		cleaned := extractJSON(out)
		canonical, valErr := validateAndCanonicalize(cleaned, schema)
		if valErr == nil {
			log.DebugContext(ctx, "structured output validated", "agent", a.Role, "attempt", attempt)
			return canonical, nil
		}

		log.DebugContext(ctx, "structured output validation failed", "agent", a.Role, "attempt", attempt, "error", valErr)

		if attempt >= repairMax {
			return "", fmt.Errorf("agent %q: %w (last error: %v)", a.Role, ErrRepairBudgetExceeded, valErr)
		}

		messages = append(messages,
			AssistantMessage(out),
			UserMessage(buildRepairPrompt(out, valErr, schema)),
		)
		attempt++
	}
}

// executeStructuredToolCall is the emit_result tool-call extraction
// strategy. The synthetic tool emit_result carries the JSON Schema as
// its parameters; the model is asked to call it exactly once. The
// tool's arguments — already RawMessage-normalised by every
// ToolCallingLLM implementation in this repo (llm/openai.go,
// llm/ollama.go, etc.) — are the canonical output.
//
// If the model fails to call the tool (returns plain text instead),
// the loop falls into a repair cycle with a prompt asking it to call
// the tool. After RepairMax failed attempts, returns
// ErrRepairBudgetExceeded.
func executeStructuredToolCall(ctx context.Context, a *Agent, t *Task, contextText string, schema json.RawMessage, repairMax int, log *slog.Logger) (string, error) {
	tcll, ok := a.LLM.(ToolCallingLLM)
	if !ok {
		return "", ErrToolCallStructuredUnsupported
	}

	spec := ToolSpec{
		Type: "function",
		Function: ToolFunction{
			Name:        emitResultToolName,
			Description: "Submit the structured answer to the task. Call this tool exactly once.",
			Parameters:  schema,
		},
	}

	system := buildSystemPrompt(a) + "\n\n" + toolCallStructuredInstruction
	taskPrompt := buildStructuredPrompt(t, contextText, schema)

	messages := []Message{
		SystemMessage(system),
		UserMessage(taskPrompt),
	}

	attempt := 0
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		resp, err := tcll.CallWithTools(ctx, messages, []ToolSpec{spec})
		if err != nil {
			return "", fmt.Errorf("agent %q: %w", a.Role, err)
		}

		canonical, valErr, found := extractEmitResultArguments(resp.ToolCalls, schema)
		if found && valErr == nil {
			log.DebugContext(ctx, "structured output validated (tool-call)", "agent", a.Role, "attempt", attempt)
			return canonical, nil
		}

		log.DebugContext(ctx, "structured output validation failed (tool-call)", "agent", a.Role, "attempt", attempt, "error", valErr)

		if attempt >= repairMax {
			return "", fmt.Errorf("agent %q: %w (last error: %v)", a.Role, ErrRepairBudgetExceeded, valErr)
		}

		// Compose an assistant entry that mirrors what the model
		// returned, so subsequent turns carry context.
		assistantMsg := Message{
			Role:      RoleAssistant,
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		}
		messages = append(messages, assistantMsg)

		// Feedback goes into the next user turn so the model can
		// retry: "you did not call emit_result" or "your arguments
		// failed validation".
		feedback := "Your previous reply did not satisfy the required schema."
		if resp.Content != "" && len(resp.ToolCalls) == 0 {
			feedback = "Your previous reply was plain text. Call the emit_result tool exactly once with your answer as arguments."
		} else if valErr != nil {
			feedback = "Your emit_result call did not validate: " + valErr.Error()
		}
		messages = append(messages, UserMessage(buildToolCallRepairPrompt(feedback, schema)))
		attempt++
	}
}

// extractEmitResultArguments finds the emit_result tool call (if any)
// in the response, validates it against schema, and returns the
// canonical JSON. found=false means the model did not call the
// tool; the caller treats it as a failed validation that can be
// repaired.
func extractEmitResultArguments(calls []ToolCall, schema json.RawMessage) (canonical string, valErr error, found bool) {
	for _, tc := range calls {
		if tc.Function.Name != emitResultToolName {
			continue
		}
		found = true
		args := tc.Function.Arguments
		if len(args) == 0 {
			valErr = fmt.Errorf("%w: emit_result arguments are empty", ErrInvalidOutput)
			return
		}
		canonical, valErr = validateAndCanonicalize(string(args), schema)
		return
	}
	return
}

// validateAndCanonicalize validates cleaned JSON text against the schema
// and, on success, returns the canonicalized (re-marshaled) JSON string.
// On failure it returns an empty string and a non-nil error wrapping
// ErrInvalidOutput.
func validateAndCanonicalize(cleaned string, schema json.RawMessage) (string, error) {
	// Syntax check: unmarshal into a json.RawMessage to confirm valid JSON.
	var raw json.RawMessage
	if err := json.Unmarshal([]byte(cleaned), &raw); err != nil {
		return "", fmt.Errorf("%w: invalid JSON: %v", ErrInvalidOutput, err)
	}

	if valErr := validateSchema(raw, schema); valErr != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidOutput, valErr)
	}

	// Canonicalize: re-marshal for a stable, compact representation.
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return "", fmt.Errorf("%w: canonicalization: %v", ErrInvalidOutput, err)
	}
	canonical, err := json.Marshal(doc)
	if err != nil {
		return "", fmt.Errorf("%w: canonicalization: %v", ErrInvalidOutput, err)
	}
	return string(canonical), nil
}

// extractJSON extracts the JSON payload from a model response. It trims
// leading/trailing whitespace and strips optional markdown code fences
// (```json ... ```). It does NOT attempt to find JSON inside arbitrary
// free text; if the result is not valid JSON, the validator will report
// an error and the repair loop handles it.
func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	// Strip opening fence.
	if strings.HasPrefix(s, "```") {
		// Remove the opening fence line.
		idx := strings.IndexByte(s, '\n')
		if idx < 0 {
			return s
		}
		s = strings.TrimSpace(s[idx+1:])
	}
	// Strip closing fence.
	if strings.HasSuffix(s, "```") {
		s = strings.TrimSpace(s[:len(s)-3])
	}
	return strings.TrimSpace(s)
}
