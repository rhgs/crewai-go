package crewai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// StructuredOutput configures a Task to require JSON output validated
// against a JSON Schema. When non-nil on a Task, the executor instructs
// the model to reply with JSON only, validates the response in Go, and
// retries (repairs) up to RepairMax times if validation fails.
type StructuredOutput struct {
	// Schema is the JSON Schema the output must satisfy. Required.
	// It is stored as raw JSON so the caller can pass any schema object
	// marshaled with encoding/json.
	Schema json.RawMessage

	// RepairMax is the maximum number of repair attempts after the
	// initial call. If <= 0, it defaults to 2.
	RepairMax int
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

// defaultRepairMax is the repair limit used when RepairMax <= 0.
const defaultRepairMax = 2

// executeStructured runs the structured-output loop for a task whose
// Structured field is non-nil. It instructs the model to produce JSON
// only, validates the output against the schema, and retries up to
// RepairMax times if validation fails. It returns the canonicalized
// JSON string on success or a sentinel error on failure.
func executeStructured(ctx context.Context, a *Agent, t *Task, contextText string, log Logger) (string, error) {
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
			log.Debugf("[%s] structured output validated on attempt %d", a.Role, attempt)
			return canonical, nil
		}

		log.Debugf("[%s] structured output attempt %d failed: %v", a.Role, attempt, valErr)

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
