package crewai

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
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

// MaxSchemaPatternLen caps the length of a "pattern" regex string accepted
// by the validator. Longer patterns are rejected as schema errors to avoid
// pathological regular expressions.
const MaxSchemaPatternLen = 512

// knownSchemaKeywords lists keywords the validator understands at a node.
// Used by StrictSchema checks on StructuredOutput.
var knownSchemaKeywords = map[string]struct{}{
	"type": {}, "properties": {}, "required": {}, "enum": {}, "items": {},
	"additionalProperties": {},
	"minLength":            {}, "maxLength": {},
	"minimum": {}, "maximum": {},
	"exclusiveMinimum": {}, "exclusiveMaximum": {},
	"minItems": {}, "maxItems": {},
	"pattern": {},
	"oneOf":   {}, "anyOf": {}, "allOf": {},
	// Meta / ignored (do not cause StrictSchema failure):
	"$schema": {}, "$id": {}, "title": {}, "description": {}, "default": {},
	"examples": {}, "definitions": {}, "$defs": {},
}

// unsupportedStrictKeywords cause StrictSchema construction to fail when
// present anywhere in the schema tree.
var unsupportedStrictKeywords = map[string]struct{}{
	"$ref": {}, "if": {}, "then": {}, "else": {},
	"not": {}, "dependentRequired": {}, "dependentSchemas": {},
	"unevaluatedProperties": {}, "unevaluatedItems": {},
	"prefixItems": {}, "contains": {}, "propertyNames": {},
	"minProperties": {}, "maxProperties": {},
	"uniqueItems": {}, "const": {}, "format": {},
}

// validateSchema validates a raw JSON document against a raw JSON Schema.
// It returns nil if the document satisfies the schema, or a non-nil error
// (ValidationErrors or a single error) on failure.
//
// Supported keywords (stdlib-only subset):
//
//	type, properties, required, enum, items,
//	additionalProperties (bool or nested schema),
//	minLength, maxLength (string length in bytes — len(s)),
//	minimum, maximum, exclusiveMinimum, exclusiveMaximum (numbers as float64),
//	minItems, maxItems,
//	pattern (Go regexp; pattern length capped at MaxSchemaPatternLen),
//	oneOf, anyOf, allOf.
//
// Not supported: $ref, if/then/else, unevaluated*, format, and most draft
// 2020-12 keywords. See StrictSchema on StructuredOutput to fail fast when
// unsupported keywords appear in an author-supplied schema.
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

// checkSchemaSupported walks a schema tree and returns an error if any
// unsupported keyword is present. Used by NewStructuredOutput when
// StrictSchema is true.
func checkSchemaSupported(schema json.RawMessage) error {
	var node any
	if err := json.Unmarshal(schema, &node); err != nil {
		return fmt.Errorf("invalid schema JSON: %w", err)
	}
	var found []string
	walkSchemaKeywords(node, "", &found)
	if len(found) == 0 {
		return nil
	}
	return fmt.Errorf("unsupported schema keywords: %s", strings.Join(found, ", "))
}

func walkSchemaKeywords(node any, path string, found *[]string) {
	m, ok := node.(map[string]any)
	if !ok {
		if arr, ok := node.([]any); ok {
			for i, el := range arr {
				walkSchemaKeywords(el, fmt.Sprintf("%s/%d", path, i), found)
			}
		}
		return
	}
	for k, v := range m {
		p := joinPath(path, k)
		if _, bad := unsupportedStrictKeywords[k]; bad {
			*found = append(*found, p)
			continue
		}
		// Recurse into known nested schema holders.
		switch k {
		case "properties", "$defs", "definitions":
			if props, ok := v.(map[string]any); ok {
				for pk, pv := range props {
					walkSchemaKeywords(pv, joinPath(p, pk), found)
				}
			}
		case "items", "additionalProperties", "not":
			walkSchemaKeywords(v, p, found)
		case "oneOf", "anyOf", "allOf":
			if arr, ok := v.([]any); ok {
				for i, el := range arr {
					walkSchemaKeywords(el, fmt.Sprintf("%s/%d", p, i), found)
				}
			}
		default:
			// ignore scalars / unknown-but-not-unsupported
		}
	}
}

// validateNode recursively validates a decoded JSON value against a schema
// node. All discovered errors are appended to errs (not short-circuited)
// so the repair prompt can present the full list to the model.
func validateNode(value any, schema map[string]any, path string, errs *ValidationErrors) {
	// Combinators first: allOf always applies; oneOf/anyOf replace the
	// rest of the node when present (draft-ish: we still also apply
	// sibling keywords after a successful match).
	if allOf, ok := schema["allOf"].([]any); ok {
		for i, sub := range allOf {
			subMap, ok := sub.(map[string]any)
			if !ok {
				continue
			}
			var subErrs ValidationErrors
			validateNode(value, subMap, path, &subErrs)
			if len(subErrs) > 0 {
				for _, e := range subErrs {
					*errs = append(*errs, e)
				}
				// keep collecting across branches
				_ = i
			}
		}
	}
	if anyOf, ok := schema["anyOf"].([]any); ok && len(anyOf) > 0 {
		if !matchOneOfAnyOf(value, anyOf, false /*requireExactlyOne*/) {
			*errs = append(*errs, &ValidationError{
				Path:    path,
				Message: "value does not match any anyOf schema",
			})
		}
	}
	if oneOf, ok := schema["oneOf"].([]any); ok && len(oneOf) > 0 {
		if !matchOneOfAnyOf(value, oneOf, true /*requireExactlyOne*/) {
			*errs = append(*errs, &ValidationError{
				Path:    path,
				Message: "value does not match exactly one oneOf schema",
			})
		}
	}

	if t, ok := schema["type"]; ok {
		if !checkType(value, t) {
			*errs = append(*errs, &ValidationError{
				Path:    path,
				Message: fmt.Sprintf("expected type %v, got %s", t, jsonTypeOf(value)),
			})
			// Type mismatch: no point checking further type-specific keywords.
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

	// String keywords (length in bytes — D7).
	if s, ok := value.(string); ok {
		validateString(s, schema, path, errs)
	}

	// Number keywords.
	if f, ok := value.(float64); ok {
		validateNumber(f, schema, path, errs)
	}

	switch v := value.(type) {
	case map[string]any:
		validateObject(v, schema, path, errs)
	case []any:
		validateArray(v, schema, path, errs)
	}
}

func matchOneOfAnyOf(value any, alts []any, exactlyOne bool) bool {
	matches := 0
	for _, sub := range alts {
		subMap, ok := sub.(map[string]any)
		if !ok {
			continue
		}
		var subErrs ValidationErrors
		validateNode(value, subMap, "", &subErrs)
		if len(subErrs) == 0 {
			matches++
			if !exactlyOne && matches >= 1 {
				return true
			}
		}
	}
	if exactlyOne {
		return matches == 1
	}
	return matches >= 1
}

func validateString(s string, schema map[string]any, path string, errs *ValidationErrors) {
	// minLength / maxLength count bytes (len), not runes — documented.
	if v, ok := asFloat(schema["minLength"]); ok {
		if float64(len(s)) < v {
			*errs = append(*errs, &ValidationError{
				Path:    path,
				Message: fmt.Sprintf("string length %d is less than minLength %v", len(s), v),
			})
		}
	}
	if v, ok := asFloat(schema["maxLength"]); ok {
		if float64(len(s)) > v {
			*errs = append(*errs, &ValidationError{
				Path:    path,
				Message: fmt.Sprintf("string length %d is greater than maxLength %v", len(s), v),
			})
		}
	}
	if pat, ok := schema["pattern"].(string); ok && pat != "" {
		if len(pat) > MaxSchemaPatternLen {
			*errs = append(*errs, &ValidationError{
				Path:    path,
				Message: fmt.Sprintf("pattern length %d exceeds MaxSchemaPatternLen %d", len(pat), MaxSchemaPatternLen),
			})
			return
		}
		re, err := regexp.Compile(pat)
		if err != nil {
			*errs = append(*errs, &ValidationError{
				Path:    path,
				Message: fmt.Sprintf("invalid pattern: %v", err),
			})
			return
		}
		if !re.MatchString(s) {
			*errs = append(*errs, &ValidationError{
				Path:    path,
				Message: fmt.Sprintf("value does not match pattern %q", pat),
			})
		}
	}
}

func validateNumber(f float64, schema map[string]any, path string, errs *ValidationErrors) {
	if v, ok := asFloat(schema["minimum"]); ok {
		if f < v {
			*errs = append(*errs, &ValidationError{
				Path:    path,
				Message: fmt.Sprintf("value %v is less than minimum %v", f, v),
			})
		}
	}
	if v, ok := asFloat(schema["maximum"]); ok {
		if f > v {
			*errs = append(*errs, &ValidationError{
				Path:    path,
				Message: fmt.Sprintf("value %v is greater than maximum %v", f, v),
			})
		}
	}
	// exclusiveMinimum / exclusiveMaximum: support both draft-04 bool form
	// (paired with minimum/maximum) and draft-06+ numeric form.
	if ex, ok := schema["exclusiveMinimum"]; ok {
		switch e := ex.(type) {
		case bool:
			if e {
				if min, ok := asFloat(schema["minimum"]); ok && f <= min {
					*errs = append(*errs, &ValidationError{
						Path:    path,
						Message: fmt.Sprintf("value %v is not greater than exclusiveMinimum %v", f, min),
					})
				}
			}
		default:
			if v, ok := asFloat(ex); ok && f <= v {
				*errs = append(*errs, &ValidationError{
					Path:    path,
					Message: fmt.Sprintf("value %v is not greater than exclusiveMinimum %v", f, v),
				})
			}
		}
	}
	if ex, ok := schema["exclusiveMaximum"]; ok {
		switch e := ex.(type) {
		case bool:
			if e {
				if max, ok := asFloat(schema["maximum"]); ok && f >= max {
					*errs = append(*errs, &ValidationError{
						Path:    path,
						Message: fmt.Sprintf("value %v is not less than exclusiveMaximum %v", f, max),
					})
				}
			}
		default:
			if v, ok := asFloat(ex); ok && f >= v {
				*errs = append(*errs, &ValidationError{
					Path:    path,
					Message: fmt.Sprintf("value %v is not less than exclusiveMaximum %v", f, v),
				})
			}
		}
	}
}

// validateObject checks required, properties, and additionalProperties.
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

	props, _ := schema["properties"].(map[string]any)
	if props != nil {
		for key, subSchema := range props {
			subMap, ok := subSchema.(map[string]any)
			if !ok {
				continue
			}
			val, exists := obj[key]
			if !exists {
				continue
			}
			p := joinPath(path, key)
			validateNode(val, subMap, p, errs)
		}
	}

	if ap, ok := schema["additionalProperties"]; ok {
		switch apv := ap.(type) {
		case bool:
			if !apv {
				for key := range obj {
					if props != nil {
						if _, known := props[key]; known {
							continue
						}
					}
					*errs = append(*errs, &ValidationError{
						Path:    joinPath(path, key),
						Message: "additional property not allowed",
					})
				}
			}
		case map[string]any:
			for key, val := range obj {
				if props != nil {
					if _, known := props[key]; known {
						continue
					}
				}
				validateNode(val, apv, joinPath(path, key), errs)
			}
		}
	}
}

// validateArray checks items, minItems, maxItems.
func validateArray(arr []any, schema map[string]any, path string, errs *ValidationErrors) {
	if v, ok := asFloat(schema["minItems"]); ok {
		if float64(len(arr)) < v {
			*errs = append(*errs, &ValidationError{
				Path:    path,
				Message: fmt.Sprintf("array length %d is less than minItems %v", len(arr), v),
			})
		}
	}
	if v, ok := asFloat(schema["maxItems"]); ok {
		if float64(len(arr)) > v {
			*errs = append(*errs, &ValidationError{
				Path:    path,
				Message: fmt.Sprintf("array length %d is greater than maxItems %v", len(arr), v),
			})
		}
	}
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
		// type as array (multi-type) — support simple list of strings
		if arr, ok := t.([]any); ok {
			for _, el := range arr {
				if checkType(value, el) {
					return true
				}
			}
			return false
		}
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

// asFloat coerces JSON-decoded numbers (float64) and common numeric
// encodings into float64.
func asFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(n, 64)
		return f, err == nil
	default:
		return 0, false
	}
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
