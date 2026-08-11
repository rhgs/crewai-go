package crewai

import (
	"errors"
	"regexp"
	"strings"
)

// redactError returns a sanitized representation of err suitable for logging
// in user-visible destinations. Truncates to a single line, caps the length,
// and masks tokens that look like secrets (API keys, bearer tokens, long
// alphanumeric tokens). Returns nil when err is nil.
//
// Use this anywhere a provider error is passed to a logger:
//
//	logger.WarnContext(ctx, "delegation failed", "error", redactError(err))
//
// The original error is preserved for callers — only its log representation
// is sanitized.
func redactError(err error) error {
	if err == nil {
		return nil
	}
	s := err.Error()
	s = strings.SplitN(s, "\n", 2)[0] // take only the first line
	s = redactString(s)
	if s == "" {
		return errors.New("[redacted]")
	}
	return errors.New(s)
}

// redactString sanitizes a single-line message. It masks:
//   - long alphanumeric tokens (≥20 chars without spaces), often API keys
//     such as sk-..., sk-ant-..., xai-...
//   - "Bearer <token>" in HTTP-style messages
//   - "key=value" pairs where value is long alphanumeric
//
// Strings are kept readable by keeping the first and last 4 characters of
// any redacted token when the token is long enough.
func redactString(s string) string {
	if s == "" {
		return s
	}
	// Mask long alphanumeric sequences (likely API keys / tokens).
	// We allow internal `-`, `_`, `.` so things like sk-proj-xxx are caught.
	longToken := regexp.MustCompile(`[A-Za-z0-9_\-.]{20,}`)
	s = longToken.ReplaceAllStringFunc(s, func(m string) string {
		if len(m) >= 24 {
			// Keep first 4 and last 4 visible to preserve identifiability.
			return m[:4] + "…[REDACTED]…" + m[len(m)-4:]
		}
		return "[REDACTED]"
	})
	// Mask Bearer tokens. Only the token portion is replaced.
	bearer := regexp.MustCompile(`(Bearer\s+)[A-Za-z0-9._\-]+`)
	s = bearer.ReplaceAllString(s, "${1}[REDACTED]")
	// Mask key=value in query strings where value is long alphanumeric.
	queryKV := regexp.MustCompile(`([?&](?:api_?key|token|key|secret)=)[A-Za-z0-9._\-]+`)
	s = queryKV.ReplaceAllString(s, "${1}[REDACTED]")
	// Cap the length with a word-boundary aware ellipsis.
	if len(s) > 160 {
		s = s[:157] + "..."
	}
	return s
}
