package crewai

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// ValidationError describes a single JSON Schema validation failure.
type ValidationError struct {
	// Path is a JSON-pointer-style path to the offending element,
	// e.g. "/name" or "/items/0".
	Path string
	// Message is a human-readable description of the failure.
	Message string
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	if e.Path == "" {
		return e.Message
	}
	return fmt.Sprintf("%s: %s", e.Path, e.Message)
}

// ValidationErrors is a collection of one or more validation errors.
type ValidationErrors []*ValidationError

// Error implements the error interface, joining all individual errors.
func (e ValidationErrors) Error() string {
	if len(e) == 0 {
		return "no validation errors"
	}
	if len(e) == 1 {
		return e[0].Error()
	}
	var b strings.Builder
	for i, ve := range e {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(ve.Error())
	}
	return b.String()
}

// validateSchema validates a raw JSON document against a raw JSON Schema.
// It returns nil if the document satisfies the schema, or a non-nil error
// (ValidationErrors or a single error) on failure.
//
// The validator uses encoding/json and manual traversal. It supports a
// subset of JSON Schema keywords: type, properties, required, enum, items.
// It is NOT a full JSON Schema implementation and intentionally omits
// keywords such as additionalProperties, oneOf/anyOf, pattern,
// minimum/maximum, minItems/maxItems, and others.
func validateSchema(doc, schema json.RawMessage) error {
	var schemaNode any
	if err := json.Unmarshal(schema, &schemaNode); err != nil {
		return fmt.Errorf("invalid schema: %w", err)
	}
	schemaMap, ok := schemaNode.(map[string]any)
	if !ok {
		// A non-object schema (e.g. a bare boolean) is not supported;
		// treat as pass (no constraints).
		return nil
	}

	var docNode any
	if err := json.Unmarshal(doc, &docNode); err != nil {
		return &ValidationError{
			Path:    "",
			Message: fmt.Sprintf("invalid JSON: %v", err),
		}
	}

	var errs ValidationErrors
	validateNode(docNode, schemaMap, "", &errs)
	if len(errs) > 0 {
		return errs
	}
	return nil
}

// validateNode recursively validates a decoded JSON value against a schema
// node. All discovered errors are appended to errs (not short-circuited)
// so the repair prompt can present the full list to the model.
func validateNode(value any, schema map[string]any, path string, errs *ValidationErrors) {
	if t, ok := schema["type"]; ok {
		if !checkType(value, t) {
			*errs = append(*errs, &ValidationError{
				Path:    path,
				Message: fmt.Sprintf("expected type %v, got %s", t, jsonTypeOf(value)),
			})
			// Type mismatch: no point checking further sub-keywords.
			return
		}
	}

	if e, ok := schema["enum"]; ok {
		if !checkEnum(value, e) {
			*errs = append(*errs, &ValidationError{
				Path:    path,
				Message: fmt.Sprintf("value not in enum %v", e),
			})
		}
	}

	switch v := value.(type) {
	case map[string]any:
		validateObject(v, schema, path, errs)
	case []any:
		validateArray(v, schema, path, errs)
	}
}

// validateObject checks "required" and "properties" keywords for an object.
func validateObject(obj map[string]any, schema map[string]any, path string, errs *ValidationErrors) {
	if req, ok := schema["required"].([]any); ok {
		for _, r := range req {
			key, ok := r.(string)
			if !ok {
				continue
			}
			if _, exists := obj[key]; !exists {
				p := joinPath(path, key)
				*errs = append(*errs, &ValidationError{
					Path:    p,
					Message: "missing required field",
				})
			}
		}
	}

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		return
	}
	for key, subSchema := range props {
		subMap, ok := subSchema.(map[string]any)
		if !ok {
			continue
		}
		val, exists := obj[key]
		if !exists {
			// Not present; "required" already handles missing fields.
			continue
		}
		p := joinPath(path, key)
		validateNode(val, subMap, p, errs)
	}
}

// validateArray checks the "items" keyword for an array.
func validateArray(arr []any, schema map[string]any, path string, errs *ValidationErrors) {
	items, ok := schema["items"].(map[string]any)
	if !ok {
		return
	}
	for i, elem := range arr {
		p := fmt.Sprintf("%s/%d", path, i)
		validateNode(elem, items, p, errs)
	}
}

// checkType returns true if the value matches the JSON Schema type string.
func checkType(value any, t any) bool {
	typeStr, ok := t.(string)
	if !ok {
		// Unknown type declaration; skip.
		return true
	}
	switch typeStr {
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "number":
		_, ok := value.(float64)
		return ok
	case "integer":
		f, ok := value.(float64)
		return ok && f == float64(int64(f))
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "null":
		return value == nil
	default:
		return true
	}
}

// checkEnum returns true if the value equals one of the enum entries.
func checkEnum(value any, enum any) bool {
	arr, ok := enum.([]any)
	if !ok {
		return true
	}
	for _, e := range arr {
		if reflect.DeepEqual(value, e) {
			return true
		}
	}
	return false
}

// jsonTypeOf returns the JSON type name of a Go value produced by
// encoding/json.Unmarshal into an any.
func jsonTypeOf(value any) string {
	switch value.(type) {
	case map[string]any:
		return "object"
	case []any:
		return "array"
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "boolean"
	case nil:
		return "null"
	default:
		return "unknown"
	}
}

// joinPath joins a parent path and a key into a JSON-pointer-style path.
func joinPath(parent, key string) string {
	if parent == "" {
		return "/" + key
	}
	return parent + "/" + key
}
