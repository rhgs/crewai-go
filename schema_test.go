package crewai

import (
	"encoding/json"
	"testing"
)

func mustRaw(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestValidateSchema_ObjectOK(t *testing.T) {
	schema := mustRaw(t, map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
			"age":  map[string]any{"type": "integer"},
		},
		"required": []any{"name", "age"},
	})
	doc := mustRaw(t, map[string]any{"name": "Alice", "age": float64(30)})
	if err := validateSchema(doc, schema); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestValidateSchema_MissingRequired(t *testing.T) {
	schema := mustRaw(t, map[string]any{
		"type":     "object",
		"required": []any{"name", "age"},
	})
	doc := mustRaw(t, map[string]any{"name": "Alice"})
	err := validateSchema(doc, schema)
	if err == nil {
		t.Fatal("expected error for missing required field")
	}
	verrs, ok := err.(ValidationErrors)
	if !ok {
		t.Fatalf("expected ValidationErrors, got %T: %v", err, err)
	}
	found := false
	for _, ve := range verrs {
		if ve.Path == "/age" && ve.Message == "missing required field" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected error about /age missing, got: %v", err)
	}
}

func TestValidateSchema_WrongType(t *testing.T) {
	schema := mustRaw(t, map[string]any{
		"type": "object",
		"properties": map[string]any{
			"age": map[string]any{"type": "integer"},
		},
	})
	doc := mustRaw(t, map[string]any{"age": "thirty"})
	err := validateSchema(doc, schema)
	if err == nil {
		t.Fatal("expected error for wrong type")
	}
	verrs, ok := err.(ValidationErrors)
	if !ok {
		t.Fatalf("expected ValidationErrors, got %T", err)
	}
	if len(verrs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(verrs), verrs)
	}
	if verrs[0].Path != "/age" {
		t.Errorf("path = %q, want /age", verrs[0].Path)
	}
}

func TestValidateSchema_Enum(t *testing.T) {
	schema := mustRaw(t, map[string]any{
		"type": "string",
		"enum": []any{"red", "green", "blue"},
	})
	// Valid.
	doc := mustRaw(t, "red")
	if err := validateSchema(doc, schema); err != nil {
		t.Errorf("expected nil for valid enum, got %v", err)
	}
	// Invalid.
	doc = mustRaw(t, "yellow")
	err := validateSchema(doc, schema)
	if err == nil {
		t.Fatal("expected error for invalid enum value")
	}
}

func TestValidateSchema_ArrayItems(t *testing.T) {
	schema := mustRaw(t, map[string]any{
		"type":  "array",
		"items": map[string]any{"type": "integer"},
	})
	// Valid.
	doc := mustRaw(t, []any{float64(1), float64(2), float64(3)})
	if err := validateSchema(doc, schema); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
	// Invalid: one item is a string.
	doc = mustRaw(t, []any{float64(1), "two", float64(3)})
	err := validateSchema(doc, schema)
	if err == nil {
		t.Fatal("expected error for invalid array item")
	}
	verrs, ok := err.(ValidationErrors)
	if !ok {
		t.Fatalf("expected ValidationErrors, got %T", err)
	}
	if len(verrs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(verrs), verrs)
	}
	if verrs[0].Path != "/1" {
		t.Errorf("path = %q, want /1", verrs[0].Path)
	}
}

func TestValidateSchema_Nested(t *testing.T) {
	schema := mustRaw(t, map[string]any{
		"type": "object",
		"properties": map[string]any{
			"address": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"city": map[string]any{"type": "string"},
					"zip":  map[string]any{"type": "string"},
				},
				"required": []any{"city"},
			},
		},
		"required": []any{"address"},
	})
	// Valid.
	doc := mustRaw(t, map[string]any{
		"address": map[string]any{"city": "NYC", "zip": "10001"},
	})
	if err := validateSchema(doc, schema); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
	// Invalid: missing city inside address.
	doc = mustRaw(t, map[string]any{
		"address": map[string]any{"zip": "10001"},
	})
	err := validateSchema(doc, schema)
	if err == nil {
		t.Fatal("expected error for missing nested required field")
	}
	verrs, ok := err.(ValidationErrors)
	if !ok {
		t.Fatalf("expected ValidationErrors, got %T", err)
	}
	found := false
	for _, ve := range verrs {
		if ve.Path == "/address/city" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected error at /address/city, got: %v", err)
	}
}

func TestValidateSchema_IntegerVsFloat(t *testing.T) {
	schema := mustRaw(t, map[string]any{"type": "integer"})

	// 5.0 is a valid integer (no fractional part).
	doc := mustRaw(t, float64(5))
	if err := validateSchema(doc, schema); err != nil {
		t.Errorf("expected nil for 5.0 as integer, got %v", err)
	}

	// 5.5 is not a valid integer.
	doc = mustRaw(t, 5.5)
	err := validateSchema(doc, schema)
	if err == nil {
		t.Fatal("expected error for 5.5 as integer")
	}
}

func TestValidateSchema_InvalidJSON(t *testing.T) {
	schema := mustRaw(t, map[string]any{"type": "string"})
	err := validateSchema(json.RawMessage(`not json`), schema)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestValidateSchema_InvalidSchema(t *testing.T) {
	doc := mustRaw(t, `"hello"`)
	err := validateSchema(doc, json.RawMessage(`not json`))
	if err == nil {
		t.Fatal("expected error for invalid schema")
	}
}

func TestValidationError_Error(t *testing.T) {
	ve := &ValidationError{Path: "/foo", Message: "bad type"}
	want := "/foo: bad type"
	if got := ve.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	veNoPath := &ValidationError{Message: "something"}
	if got := veNoPath.Error(); got != "something" {
		t.Errorf("Error() = %q, want %q", got, "something")
	}
}

func TestValidationErrors_Multiple(t *testing.T) {
	verrs := ValidationErrors{
		{Path: "/a", Message: "err1"},
		{Path: "/b", Message: "err2"},
	}
	s := verrs.Error()
	if s == "" {
		t.Error("expected non-empty error string")
	}
}
