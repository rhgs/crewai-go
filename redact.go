package crewai

import (
	"context"
	"errors"
	"log/slog"
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

// RedactHandler wraps inner and applies redactString to the record message
// and to every string attribute before forwarding. Non-string attributes are
// passed through unchanged. Groups and nested attrs from WithAttrs on the
// inner handler are not re-walked — wrap at the outermost handler.
//
// Redaction is best-effort, not a confidentiality boundary. Opt-in: the
// default Crew logger does not use RedactHandler.
//
// Typical wiring:
//
//	log := slog.New(crewai.RedactHandler(
//		slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}),
//	))
//	crew.WithLogger(log)
func RedactHandler(inner slog.Handler) slog.Handler {
	if inner == nil {
		return nil
	}
	return &redactHandler{inner: inner}
}

type redactHandler struct {
	inner slog.Handler
}

func (h *redactHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *redactHandler) Handle(ctx context.Context, r slog.Record) error {
	msg := redactString(r.Message)
	out := slog.NewRecord(r.Time, r.Level, msg, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(redactAttr(a))
		return true
	})
	return h.inner.Handle(ctx, out)
}

func (h *redactHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	masked := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		masked[i] = redactAttr(a)
	}
	return &redactHandler{inner: h.inner.WithAttrs(masked)}
}

func (h *redactHandler) WithGroup(name string) slog.Handler {
	return &redactHandler{inner: h.inner.WithGroup(name)}
}

func redactAttr(a slog.Attr) slog.Attr {
	switch a.Value.Kind() {
	case slog.KindString:
		return slog.String(a.Key, redactString(a.Value.String()))
	case slog.KindGroup:
		group := a.Value.Group()
		if len(group) == 0 {
			return a
		}
		masked := make([]slog.Attr, len(group))
		for i, ga := range group {
			masked[i] = redactAttr(ga)
		}
		return slog.Group(a.Key, attrsToAny(masked)...)
	default:
		// Also redact error values rendered as strings when KindAny holds error.
		if err, ok := a.Value.Any().(error); ok && err != nil {
			return slog.Any(a.Key, redactError(err))
		}
		return a
	}
}

func attrsToAny(attrs []slog.Attr) []any {
	out := make([]any, len(attrs))
	for i, a := range attrs {
		out[i] = a
	}
	return out
}
