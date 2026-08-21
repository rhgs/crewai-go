package crewai

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func mustSchema(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSchema_RefDefs(t *testing.T) {
	schema := mustSchema(t, map[string]any{
		"$defs": map[string]any{
			"name": map[string]any{"type": "string", "minLength": 1},
		},
		"type": "object",
		"properties": map[string]any{
			"n": map[string]any{"$ref": "#/$defs/name"},
		},
		"required": []any{"n"},
	})
	if err := validateSchema([]byte(`{"n":"hi"}`), schema); err != nil {
		t.Fatal(err)
	}
	if err := validateSchema([]byte(`{"n":""}`), schema); err == nil {
		t.Fatal("expected fail")
	}
}

func TestSchema_RefDefinitions(t *testing.T) {
	schema := mustSchema(t, map[string]any{
		"definitions": map[string]any{
			"age": map[string]any{"type": "integer", "minimum": 0},
		},
		"$ref": "#/definitions/age",
	})
	if err := validateSchema([]byte(`21`), schema); err != nil {
		t.Fatal(err)
	}
	if err := validateSchema([]byte(`-1`), schema); err == nil {
		t.Fatal("expected fail")
	}
}

func TestSchema_RefSiblings(t *testing.T) {
	// $ref + sibling maxLength
	schema := mustSchema(t, map[string]any{
		"$defs": map[string]any{
			"s": map[string]any{"type": "string"},
		},
		"$ref":      "#/$defs/s",
		"maxLength": 2,
	})
	if err := validateSchema([]byte(`"ab"`), schema); err != nil {
		t.Fatal(err)
	}
	if err := validateSchema([]byte(`"abc"`), schema); err == nil {
		t.Fatal("sibling maxLength should fail")
	}
}

func TestSchema_RefExternalRejected(t *testing.T) {
	schema := mustSchema(t, map[string]any{"$ref": "https://example.com/s"})
	err := validateSchema([]byte(`{}`), schema)
	if err == nil || !strings.Contains(err.Error(), "ref") {
		t.Fatalf("%v", err)
	}
}

func TestSchema_RefNotFound(t *testing.T) {
	schema := mustSchema(t, map[string]any{"$ref": "#/$defs/missing"})
	err := validateSchema([]byte(`1`), schema)
	if !errors.Is(err, ErrSchemaRefNotFound) && (err == nil || !strings.Contains(err.Error(), "not found")) {
		// ValidationErrors wrap message
		if err == nil || !strings.Contains(err.Error(), "not found") {
			t.Fatalf("%v", err)
		}
	}
}

func TestSchema_RefCycle(t *testing.T) {
	schema := mustSchema(t, map[string]any{
		"$defs": map[string]any{
			"a": map[string]any{"$ref": "#/$defs/b"},
			"b": map[string]any{"$ref": "#/$defs/a"},
		},
		"$ref": "#/$defs/a",
	})
	err := validateSchema([]byte(`1`), schema)
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("%v", err)
	}
}

func TestSchema_Const(t *testing.T) {
	schema := mustSchema(t, map[string]any{"const": "x"})
	if err := validateSchema([]byte(`"x"`), schema); err != nil {
		t.Fatal(err)
	}
	if err := validateSchema([]byte(`"y"`), schema); err == nil {
		t.Fatal("expected fail")
	}
}

func TestSchema_Not(t *testing.T) {
	schema := mustSchema(t, map[string]any{
		"not": map[string]any{"type": "string"},
	})
	if err := validateSchema([]byte(`1`), schema); err != nil {
		t.Fatal(err)
	}
	if err := validateSchema([]byte(`"s"`), schema); err == nil {
		t.Fatal("expected fail")
	}
}

func TestSchema_IfThenElse(t *testing.T) {
	schema := mustSchema(t, map[string]any{
		"if":   map[string]any{"type": "string"},
		"then": map[string]any{"minLength": 2},
		"else": map[string]any{"type": "number"},
	})
	if err := validateSchema([]byte(`"ab"`), schema); err != nil {
		t.Fatal(err)
	}
	if err := validateSchema([]byte(`"a"`), schema); err == nil {
		t.Fatal("then should fail short string")
	}
	if err := validateSchema([]byte(`3`), schema); err != nil {
		t.Fatal(err)
	}
	if err := validateSchema([]byte(`true`), schema); err == nil {
		t.Fatal("else should require number")
	}
}

func TestSchema_MinMaxProperties(t *testing.T) {
	schema := mustSchema(t, map[string]any{
		"type":          "object",
		"minProperties": 1,
		"maxProperties": 2,
	})
	if err := validateSchema([]byte(`{}`), schema); err == nil {
		t.Fatal("minProperties")
	}
	if err := validateSchema([]byte(`{"a":1}`), schema); err != nil {
		t.Fatal(err)
	}
	if err := validateSchema([]byte(`{"a":1,"b":2,"c":3}`), schema); err == nil {
		t.Fatal("maxProperties")
	}
}

func TestSchema_UniqueItems(t *testing.T) {
	schema := mustSchema(t, map[string]any{
		"type":        "array",
		"uniqueItems": true,
	})
	if err := validateSchema([]byte(`[1,2]`), schema); err != nil {
		t.Fatal(err)
	}
	if err := validateSchema([]byte(`[1,1]`), schema); err == nil {
		t.Fatal("expected fail")
	}
}

func TestSchema_FormatAllowlist(t *testing.T) {
	cases := []struct {
		format string
		good   string
		bad    string
	}{
		{"date-time", `"2020-01-02T15:04:05Z"`, `"not-a-date"`},
		{"date", `"2020-01-02"`, `"01-02-2020"`},
		{"email", `"a@b.co"`, `"not-email"`},
		{"uri", `"https://example.com/x"`, `"notauri"`},
		{"uuid", `"550e8400-e29b-41d4-a716-446655440000"`, `"nope"`},
		{"ipv4", `"1.2.3.4"`, `"999.1.1.1"`},
		{"ipv6", `"::1"`, `"1.2.3.4"`},
	}
	for _, tc := range cases {
		schema := mustSchema(t, map[string]any{"type": "string", "format": tc.format})
		if err := validateSchema([]byte(tc.good), schema); err != nil {
			t.Fatalf("%s good: %v", tc.format, err)
		}
		if err := validateSchema([]byte(tc.bad), schema); err == nil {
			t.Fatalf("%s bad should fail", tc.format)
		}
	}
	// unknown format ignored
	schema := mustSchema(t, map[string]any{"type": "string", "format": "custom-x"})
	if err := validateSchema([]byte(`"anything"`), schema); err != nil {
		t.Fatal(err)
	}
}

func TestSchema_BooleanSchemas(t *testing.T) {
	if err := validateSchema([]byte(`1`), mustSchema(t, true)); err != nil {
		t.Fatal(err)
	}
	if err := validateSchema([]byte(`1`), mustSchema(t, false)); err == nil {
		t.Fatal("false schema")
	}
}

func TestSchema_StrictAllowsRefFormat(t *testing.T) {
	raw := mustSchema(t, map[string]any{
		"$defs": map[string]any{"x": map[string]any{"type": "string", "format": "email"}},
		"properties": map[string]any{
			"e": map[string]any{"$ref": "#/$defs/x"},
		},
	})
	if _, err := NewStructuredOutput(raw, WithStrictSchema()); err != nil {
		t.Fatal(err)
	}
}

func TestSchema_StrictStillRejectsUnevaluated(t *testing.T) {
	raw := mustSchema(t, map[string]any{
		"type":                  "object",
		"unevaluatedProperties": false,
	})
	if _, err := NewStructuredOutput(raw, WithStrictSchema()); err == nil {
		t.Fatal("expected strict reject")
	}
}

func TestSchema_GoldenSimpleStillWorks(t *testing.T) {
	schema := mustSchema(t, map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
		"required": []any{"name"},
	})
	if err := validateSchema([]byte(`{"name":"a"}`), schema); err != nil {
		t.Fatal(err)
	}
}
