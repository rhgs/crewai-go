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

func TestValidateSchema_AdditionalPropertiesFalse(t *testing.T) {
	schema := mustRaw(t, map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
		"additionalProperties": false,
	})
	if err := validateSchema(mustRaw(t, map[string]any{"name": "a"}), schema); err != nil {
		t.Fatalf("ok doc: %v", err)
	}
	err := validateSchema(mustRaw(t, map[string]any{"name": "a", "extra": 1}), schema)
	if err == nil {
		t.Fatal("expected additional property error")
	}
}

func TestValidateSchema_AdditionalPropertiesSchema(t *testing.T) {
	schema := mustRaw(t, map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
		"additionalProperties": map[string]any{"type": "integer"},
	})
	if err := validateSchema(mustRaw(t, map[string]any{"name": "a", "n": float64(1)}), schema); err != nil {
		t.Fatal(err)
	}
	err := validateSchema(mustRaw(t, map[string]any{"name": "a", "n": "x"}), schema)
	if err == nil {
		t.Fatal("expected type error on additional prop")
	}
}

func TestValidateSchema_StringLengthBytes(t *testing.T) {
	// "á" is 2 bytes in UTF-8; minLength/maxLength count bytes (D7).
	schema := mustRaw(t, map[string]any{"type": "string", "minLength": 2, "maxLength": 2})
	if err := validateSchema(mustRaw(t, "á"), schema); err != nil {
		t.Fatalf("á is 2 bytes: %v", err)
	}
	if err := validateSchema(mustRaw(t, "a"), schema); err == nil {
		t.Fatal("single byte should fail minLength 2")
	}
	schema2 := mustRaw(t, map[string]any{"type": "string", "maxLength": 1})
	if err := validateSchema(mustRaw(t, "á"), schema2); err == nil {
		t.Fatal("2-byte char should fail maxLength 1")
	}
}

func TestValidateSchema_NumberBounds(t *testing.T) {
	schema := mustRaw(t, map[string]any{"type": "number", "minimum": 1, "maximum": 10})
	if err := validateSchema(mustRaw(t, float64(1)), schema); err != nil {
		t.Fatal(err)
	}
	if err := validateSchema(mustRaw(t, float64(0.5)), schema); err == nil {
		t.Fatal("below minimum")
	}
	if err := validateSchema(mustRaw(t, float64(10.5)), schema); err == nil {
		t.Fatal("above maximum")
	}
	// exclusive numeric form
	schema = mustRaw(t, map[string]any{"type": "number", "exclusiveMinimum": 1, "exclusiveMaximum": 10})
	if err := validateSchema(mustRaw(t, float64(1)), schema); err == nil {
		t.Fatal("exclusive min")
	}
	if err := validateSchema(mustRaw(t, float64(5)), schema); err != nil {
		t.Fatal(err)
	}
	if err := validateSchema(mustRaw(t, float64(10)), schema); err == nil {
		t.Fatal("exclusive max")
	}
}

func TestValidateSchema_ArrayBounds(t *testing.T) {
	schema := mustRaw(t, map[string]any{"type": "array", "minItems": 1, "maxItems": 2, "items": map[string]any{"type": "string"}})
	if err := validateSchema(mustRaw(t, []any{}), schema); err == nil {
		t.Fatal("minItems")
	}
	if err := validateSchema(mustRaw(t, []any{"a", "b", "c"}), schema); err == nil {
		t.Fatal("maxItems")
	}
	if err := validateSchema(mustRaw(t, []any{"a"}), schema); err != nil {
		t.Fatal(err)
	}
}

func TestValidateSchema_Pattern(t *testing.T) {
	schema := mustRaw(t, map[string]any{"type": "string", "pattern": "^[a-z]+$"})
	if err := validateSchema(mustRaw(t, "abc"), schema); err != nil {
		t.Fatal(err)
	}
	if err := validateSchema(mustRaw(t, "A1"), schema); err == nil {
		t.Fatal("pattern should fail")
	}
	// invalid pattern
	schema = mustRaw(t, map[string]any{"type": "string", "pattern": "["})
	if err := validateSchema(mustRaw(t, "a"), schema); err == nil {
		t.Fatal("invalid pattern should error")
	}
}

func TestValidateSchema_OneOfAnyOfAllOf(t *testing.T) {
	// oneOf: string or integer, exactly one
	schema := mustRaw(t, map[string]any{
		"oneOf": []any{
			map[string]any{"type": "string"},
			map[string]any{"type": "integer"},
		},
	})
	if err := validateSchema(mustRaw(t, "hi"), schema); err != nil {
		t.Fatal(err)
	}
	if err := validateSchema(mustRaw(t, float64(3)), schema); err != nil {
		t.Fatal(err)
	}
	if err := validateSchema(mustRaw(t, true), schema); err == nil {
		t.Fatal("bool matches neither")
	}

	// anyOf
	schema = mustRaw(t, map[string]any{
		"anyOf": []any{
			map[string]any{"type": "string", "minLength": 3},
			map[string]any{"type": "integer"},
		},
	})
	if err := validateSchema(mustRaw(t, "ab"), schema); err == nil {
		t.Fatal("short string fails anyOf")
	}
	if err := validateSchema(mustRaw(t, "abcd"), schema); err != nil {
		t.Fatal(err)
	}

	// allOf
	schema = mustRaw(t, map[string]any{
		"allOf": []any{
			map[string]any{"type": "object"},
			map[string]any{"required": []any{"x"}},
		},
	})
	if err := validateSchema(mustRaw(t, map[string]any{"x": 1}), schema); err != nil {
		t.Fatal(err)
	}
	if err := validateSchema(mustRaw(t, map[string]any{"y": 1}), schema); err == nil {
		t.Fatal("missing x")
	}
}

func TestCheckSchemaSupported(t *testing.T) {
	ok := mustRaw(t, map[string]any{"type": "object", "properties": map[string]any{"a": map[string]any{"type": "string", "pattern": "^x$"}}})
	if err := checkSchemaSupported(ok); err != nil {
		t.Fatal(err)
	}
	bad := mustRaw(t, map[string]any{"$ref": "#/definitions/x"})
	if err := checkSchemaSupported(bad); err == nil {
		t.Fatal("expected unsupported")
	}
}
