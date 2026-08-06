package crewai

import (
	"encoding/json"
	"testing"
)

// --- Tests to raise coverage above 90% ---

func TestCheckType_AllTypes(t *testing.T) {
	cases := []struct {
		value any
		typ  string
		want  bool
	}{
		// object
		{map[string]any{}, "object", true},
		{"x", "object", false},
		// array
		{[]any{}, "array", true},
		{"x", "array", false},
		// string
		{"hello", "string", true},
		{float64(1), "string", false},
		// number
		{float64(3.14), "number", true},
		{"x", "number", false},
		// boolean
		{true, "boolean", true},
		{false, "boolean", true},
		{"x", "boolean", false},
		// null
		{nil, "null", true},
		{"x", "null", false},
		// unknown type -> always pass
		{"x", "gizmo", true},
	}
	for _, c := range cases {
		got := checkType(c.value, c.typ)
		if got != c.want {
			t.Errorf("checkType(%T, %q) = %v, want %v", c.value, c.typ, got, c.want)
		}
	}
}

func TestCheckType_NonStringType(t *testing.T) {
	// If the "type" value in the schema is not a string (e.g. a number),
	// checkType should return true (skip).
	if !checkType("x", float64(42)) {
		t.Error("checkType with non-string type should return true")
	}
}

func TestJsonTypeOf_AllTypes(t *testing.T) {
	cases := []struct {
		value any
		want  string
	}{
		{map[string]any{}, "object"},
		{[]any{}, "array"},
		{"x", "string"},
		{float64(1), "number"},
		{true, "boolean"},
		{nil, "null"},
		{42, "unknown"}, // int is not produced by encoding/json
	}
	for _, c := range cases {
		got := jsonTypeOf(c.value)
		if got != c.want {
			t.Errorf("jsonTypeOf(%T) = %q, want %q", c.value, got, c.want)
		}
	}
}

func TestCheckEnum_NonArrayEnum(t *testing.T) {
	// If the "enum" value is not an array, checkEnum should return true.
	if !checkEnum("x", "not an array") {
		t.Error("checkEnum with non-array enum should return true")
	}
}

func TestValidateArray_ItemsNotObject(t *testing.T) {
	schema := mustRaw(t, map[string]any{
		"type":  "array",
		"items": "not an object", // invalid items sub-schema
	})
	doc := mustRaw(t, []any{float64(1), float64(2)})
	// Should pass without error (items not a map -> skip).
	if err := validateSchema(doc, schema); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestValidateObject_RequiredNotStrings(t *testing.T) {
	schema := mustRaw(t, map[string]any{
		"type":     "object",
		"required": []any{float64(42)}, // non-string required entry
	})
	doc := mustRaw(t, map[string]any{})
	// Should pass: the non-string required entry is skipped.
	if err := validateSchema(doc, schema); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestValidateObject_PropertiesNotObject(t *testing.T) {
	schema := mustRaw(t, map[string]any{
		"type":       "object",
		"properties": "not an object", // invalid properties
	})
	doc := mustRaw(t, map[string]any{})
	if err := validateSchema(doc, schema); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestValidateSchema_NonObjectSchema(t *testing.T) {
	// A bare boolean schema is not an object; should pass (no constraints).
	doc := mustRaw(t, `"hello"`)
	if err := validateSchema(doc, json.RawMessage(`true`)); err != nil {
		t.Errorf("expected nil for boolean schema, got %v", err)
	}
}

func TestValidateSchema_NumberType(t *testing.T) {
	schema := mustRaw(t, map[string]any{"type": "number"})
	doc := mustRaw(t, float64(3.14))
	if err := validateSchema(doc, schema); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
	// String is not a number.
	doc = mustRaw(t, `"x"`)
	if err := validateSchema(doc, schema); err == nil {
		t.Error("expected error for string as number")
	}
}

func TestValidateSchema_BooleanType(t *testing.T) {
	schema := mustRaw(t, map[string]any{"type": "boolean"})
	doc := mustRaw(t, true)
	if err := validateSchema(doc, schema); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestValidateSchema_NullType(t *testing.T) {
	schema := mustRaw(t, map[string]any{"type": "null"})
	doc := mustRaw(t, nil)
	if err := validateSchema(doc, schema); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestValidationErrors_Empty(t *testing.T) {
	ve := ValidationErrors{}
	if got := ve.Error(); got != "no validation errors" {
		t.Errorf("Error() = %q, want %q", got, "no validation errors")
	}
}

func TestValidationErrors_Single(t *testing.T) {
	ve := ValidationErrors{&ValidationError{Path: "/x", Message: "bad"}}
	want := "/x: bad"
	if got := ve.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestExtractJSON_FenceNoNewline(t *testing.T) {
	// Input is just "```" with no newline -> should return as-is.
	got := extractJSON("```")
	if got != "```" {
		t.Errorf("extractJSON(``````) = %q, want %q", got, "```")
	}
}

func TestExtractJSON_BareFence(t *testing.T) {
	// Fence without language specifier.
	got := extractJSON("```\n{\"a\":1}\n```")
	if got != `{"a":1}` {
		t.Errorf("extractJSON = %q, want %q", got, `{"a":1}`)
	}
}