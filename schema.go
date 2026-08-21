package crewai

import (
	"encoding/json"
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
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
	"$ref": {}, "const": {}, "format": {},
	"not": {}, "if": {}, "then": {}, "else": {},
	"minProperties": {}, "maxProperties": {}, "uniqueItems": {},
	// Meta / ignored (do not cause StrictSchema failure):
	"$schema": {}, "$id": {}, "title": {}, "description": {}, "default": {},
	"examples": {}, "definitions": {}, "$defs": {},
}

// MaxSchemaRefDepth caps $ref chain depth (D-J12).
const MaxSchemaRefDepth = 32

// MaxSchemaRefExpansions caps total $ref resolutions per validateSchema call.
const MaxSchemaRefExpansions = 256

// unsupportedStrictKeywords cause StrictSchema construction to fail when
// present anywhere in the schema tree.
var unsupportedStrictKeywords = map[string]struct{}{
	"dependentRequired": {}, "dependentSchemas": {},
	"unevaluatedProperties": {}, "unevaluatedItems": {},
	"prefixItems": {}, "contains": {}, "propertyNames": {},
}

// validateSchema validates a raw JSON document against a raw JSON Schema.
// It returns nil if the document satisfies the schema, or a non-nil error
// (ValidationErrors or a single error) on failure.
//
// Supported keywords (stdlib-only subset):
//
//	type, properties, required, enum, const, items,
//	additionalProperties (bool or nested schema),
//	minLength, maxLength (string length in bytes — len(s)),
//	minimum, maximum, exclusiveMinimum, exclusiveMaximum (numbers as float64),
//	minItems, maxItems, uniqueItems, minProperties, maxProperties,
//	pattern (Go regexp; pattern length capped at MaxSchemaPatternLen),
//	format (allowlist: date-time, date, email, uri, uri-reference, uuid, ipv4, ipv6),
//	oneOf, anyOf, allOf, not, if/then/else,
//	$ref (local JSON Pointer fragments only; see MaxSchemaRefDepth).
//
// Boolean schemas: true always matches; false never matches.
//
// Not supported: unevaluated*, remote $ref, dependent*, prefixItems, contains,
// propertyNames, and most remaining draft 2020-12 keywords. See StrictSchema
// on StructuredOutput to fail fast when unsupported keywords appear.
func validateSchema(doc, schema json.RawMessage) error {
	var schemaNode any
	if err := json.Unmarshal(schema, &schemaNode); err != nil {
		return fmt.Errorf("invalid schema: %w", err)
	}

	var docNode any
	if err := json.Unmarshal(doc, &docNode); err != nil {
		return &ValidationError{
			Path:    "",
			Message: fmt.Sprintf("invalid JSON: %v", err),
		}
	}

	vc := &schemaValidation{
		root: schemaNode,
	}
	var errs ValidationErrors
	validateNode(vc, docNode, schemaNode, "", &errs)
	if len(errs) > 0 {
		return errs
	}
	return nil
}

// schemaValidation holds root + $ref bookkeeping for one validateSchema call.
type schemaValidation struct {
	root       any
	refDepth   int
	expansions int
	stack      []string // JSON pointers currently being resolved
}

func (vc *schemaValidation) pushRef(ptr string) error {
	if vc.expansions >= MaxSchemaRefExpansions {
		return ErrSchemaRefDepth
	}
	if vc.refDepth >= MaxSchemaRefDepth {
		return ErrSchemaRefDepth
	}
	for _, s := range vc.stack {
		if s == ptr {
			return ErrSchemaRefCycle
		}
	}
	vc.stack = append(vc.stack, ptr)
	vc.refDepth++
	vc.expansions++
	return nil
}

func (vc *schemaValidation) popRef() {
	if len(vc.stack) == 0 {
		return
	}
	vc.stack = vc.stack[:len(vc.stack)-1]
	if vc.refDepth > 0 {
		vc.refDepth--
	}
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
		case "items", "additionalProperties", "not", "if", "then", "else":
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
func validateNode(vc *schemaValidation, value any, schema any, path string, errs *ValidationErrors) {
	// Boolean schemas (draft-accurate): true matches all; false matches none.
	switch b := schema.(type) {
	case bool:
		if !b {
			*errs = append(*errs, &ValidationError{Path: path, Message: "schema is false"})
		}
		return
	case nil:
		return
	}
	schemaMap, ok := schema.(map[string]any)
	if !ok {
		return
	}

	// $ref: resolve local pointer; apply sibling keywords as intersection (D-J2-B).
	if refRaw, hasRef := schemaMap["$ref"]; hasRef {
		refStr, ok := refRaw.(string)
		if !ok {
			*errs = append(*errs, &ValidationError{Path: path, Message: ErrSchemaRefInvalid.Error()})
			return
		}
		target, ptr, err := resolveLocalRef(vc.root, refStr)
		if err != nil {
			*errs = append(*errs, &ValidationError{Path: path, Message: err.Error()})
			return
		}
		if err := vc.pushRef(ptr); err != nil {
			*errs = append(*errs, &ValidationError{Path: path, Message: err.Error()})
			return
		}
		validateNode(vc, value, target, path, errs)
		vc.popRef()
		// Sibling keywords (excluding $ref) still apply.
		siblings := make(map[string]any, len(schemaMap))
		for k, v := range schemaMap {
			if k == "$ref" {
				continue
			}
			siblings[k] = v
		}
		if len(siblings) > 0 {
			validateNodeKeywords(vc, value, siblings, path, errs)
		}
		return
	}

	validateNodeKeywords(vc, value, schemaMap, path, errs)
}

func validateNodeKeywords(vc *schemaValidation, value any, schema map[string]any, path string, errs *ValidationErrors) {
	// if / then / else
	if ifSch, ok := schema["if"]; ok {
		var ifErrs ValidationErrors
		validateNode(vc, value, ifSch, path, &ifErrs)
		if len(ifErrs) == 0 {
			if thenSch, ok := schema["then"]; ok {
				validateNode(vc, value, thenSch, path, errs)
			}
		} else if elseSch, ok := schema["else"]; ok {
			validateNode(vc, value, elseSch, path, errs)
		}
	}

	// not
	if notSch, ok := schema["not"]; ok {
		var notErrs ValidationErrors
		validateNode(vc, value, notSch, path, &notErrs)
		if len(notErrs) == 0 {
			*errs = append(*errs, &ValidationError{Path: path, Message: "value matches not schema"})
		}
	}

	// Combinators
	if allOf, ok := schema["allOf"].([]any); ok {
		for _, sub := range allOf {
			validateNode(vc, value, sub, path, errs)
		}
	}
	if anyOf, ok := schema["anyOf"].([]any); ok && len(anyOf) > 0 {
		if !matchOneOfAnyOf(vc, value, anyOf, false) {
			*errs = append(*errs, &ValidationError{
				Path:    path,
				Message: "value does not match any anyOf schema",
			})
		}
	}
	if oneOf, ok := schema["oneOf"].([]any); ok && len(oneOf) > 0 {
		if !matchOneOfAnyOf(vc, value, oneOf, true) {
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

	if c, ok := schema["const"]; ok {
		if !reflect.DeepEqual(value, c) {
			*errs = append(*errs, &ValidationError{
				Path:    path,
				Message: fmt.Sprintf("value does not equal const %v", c),
			})
		}
	}

	if s, ok := value.(string); ok {
		validateString(s, schema, path, errs)
	}
	if f, ok := value.(float64); ok {
		validateNumber(f, schema, path, errs)
	}

	switch v := value.(type) {
	case map[string]any:
		validateObject(vc, v, schema, path, errs)
	case []any:
		validateArray(vc, v, schema, path, errs)
	}
}

func matchOneOfAnyOf(vc *schemaValidation, value any, alts []any, exactlyOne bool) bool {
	matches := 0
	for _, sub := range alts {
		var subErrs ValidationErrors
		validateNode(vc, value, sub, "", &subErrs)
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
	if fmtName, ok := schema["format"].(string); ok && fmtName != "" {
		if errMsg := checkFormat(fmtName, s); errMsg != "" {
			*errs = append(*errs, &ValidationError{Path: path, Message: errMsg})
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
func validateObject(vc *schemaValidation, obj map[string]any, schema map[string]any, path string, errs *ValidationErrors) {
	if v, ok := asFloat(schema["minProperties"]); ok {
		if float64(len(obj)) < v {
			*errs = append(*errs, &ValidationError{
				Path:    path,
				Message: fmt.Sprintf("object has %d properties; minProperties %v", len(obj), v),
			})
		}
	}
	if v, ok := asFloat(schema["maxProperties"]); ok {
		if float64(len(obj)) > v {
			*errs = append(*errs, &ValidationError{
				Path:    path,
				Message: fmt.Sprintf("object has %d properties; maxProperties %v", len(obj), v),
			})
		}
	}
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
			validateNode(vc, val, subMap, p, errs)
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
				validateNode(vc, val, apv, joinPath(path, key), errs)
			}
		}
	}
}

// validateArray checks items, minItems, maxItems, uniqueItems.
func validateArray(vc *schemaValidation, arr []any, schema map[string]any, path string, errs *ValidationErrors) {
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
	if uniq, ok := schema["uniqueItems"].(bool); ok && uniq {
		for i := 0; i < len(arr); i++ {
			for j := i + 1; j < len(arr); j++ {
				if reflect.DeepEqual(arr[i], arr[j]) {
					*errs = append(*errs, &ValidationError{
						Path:    path,
						Message: "array items are not unique",
					})
					break
				}
			}
		}
	}
	if items, ok := schema["items"]; ok {
		for i, elem := range arr {
			p := fmt.Sprintf("%s/%d", path, i)
			validateNode(vc, elem, items, p, errs)
		}
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

// resolveLocalRef resolves a local JSON Pointer $ref against root.
// Returns the target node and the normalized pointer used for cycle detection.
func resolveLocalRef(root any, ref string) (any, string, error) {
	if ref == "" {
		return nil, "", ErrSchemaRefInvalid
	}
	// Reject URLs and non-fragment refs.
	if strings.Contains(ref, "://") || (!strings.HasPrefix(ref, "#") && strings.Contains(ref, "#")) {
		return nil, "", ErrSchemaRefInvalid
	}
	if !strings.HasPrefix(ref, "#") {
		// Plain name without fragment — not supported (would be external).
		return nil, "", ErrSchemaRefInvalid
	}
	ptr := ref[1:] // drop '#'
	if ptr == "" {
		return root, "#", nil
	}
	if !strings.HasPrefix(ptr, "/") {
		return nil, "", ErrSchemaRefInvalid
	}
	node := root
	parts := strings.Split(ptr, "/")[1:] // skip empty before first /
	for _, part := range parts {
		part = strings.ReplaceAll(part, "~1", "/")
		part = strings.ReplaceAll(part, "~0", "~")
		switch cur := node.(type) {
		case map[string]any:
			next, ok := cur[part]
			if !ok {
				return nil, "", ErrSchemaRefNotFound
			}
			node = next
		case []any:
			idx, err := strconv.Atoi(part)
			if err != nil || idx < 0 || idx >= len(cur) {
				return nil, "", ErrSchemaRefNotFound
			}
			node = cur[idx]
		default:
			return nil, "", ErrSchemaRefNotFound
		}
	}
	return node, "#" + ptr, nil
}

// checkFormat validates known format values. Unknown formats return "" (ignore).
func checkFormat(name, s string) string {
	switch name {
	case "date-time":
		if _, err := time.Parse(time.RFC3339, s); err != nil {
			if _, err2 := time.Parse(time.RFC3339Nano, s); err2 != nil {
				return fmt.Sprintf("value is not a valid date-time: %v", err)
			}
		}
	case "date":
		if _, err := time.Parse("2006-01-02", s); err != nil {
			return "value is not a valid date (YYYY-MM-DD)"
		}
	case "email":
		if len(s) > 254 {
			return "email exceeds maximum length"
		}
		if _, err := mail.ParseAddress(s); err != nil {
			return "value is not a valid email"
		}
		// mail.ParseAddress allows "Name <a@b>" — require bare addr.
		if strings.Contains(s, "<") || strings.Contains(s, " ") {
			return "value is not a valid email"
		}
	case "uri":
		u, err := url.Parse(s)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return "value is not a valid uri"
		}
	case "uri-reference":
		if _, err := url.Parse(s); err != nil {
			return "value is not a valid uri-reference"
		}
	case "uuid":
		if !uuidRegexp.MatchString(s) {
			return "value is not a valid uuid"
		}
	case "ipv4":
		ip := net.ParseIP(s)
		if ip == nil || ip.To4() == nil {
			return "value is not a valid ipv4"
		}
	case "ipv6":
		ip := net.ParseIP(s)
		if ip == nil || ip.To4() != nil {
			return "value is not a valid ipv6"
		}
	default:
		// Unknown format: ignore (D-J4-A).
	}
	return ""
}

var uuidRegexp = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
