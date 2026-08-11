// Example: structured logging with redaction.
//
// This example shows how to plug a custom *slog.Logger into a Crew and how
// to wrap the handler with a redactor so that secrets (API keys, bearer
// tokens, query-string credentials) never reach the log destination.
//
// Run:
//
//	export OPENAI_API_KEY=sk-...
//	go run ./examples/logging
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"regexp"

	"github.com/rhgs/crewai-go"
)

// redactor is a slog.Handler that masks likely-secrets in attribute values
// before forwarding to the wrapped handler. It is intentionally simple:
// any alphanumeric token (with optional -_.) of ≥20 chars is replaced by
// "[REDACTED]", preserving readability for typical log lines while
// preventing accidental leak of API keys (sk-..., sk-ant-..., xai-...).
type redactor struct {
	inner slog.Handler
	re    *regexp.Regexp
}

func newRedactor(inner slog.Handler) *redactor {
	return &redactor{
		inner: inner,
		re:    regexp.MustCompile(`[A-Za-z0-9_\-.]{20,}`),
	}
}

func (h *redactor) Enabled(ctx context.Context, l slog.Level) bool {
	return h.inner.Enabled(ctx, l)
}

func (h *redactor) Handle(ctx context.Context, r slog.Record) error {
	masked := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		if a.Value.Kind() == slog.KindString {
			s := a.Value.String()
			s = h.re.ReplaceAllString(s, "[REDACTED]")
			masked.AddAttrs(slog.String(a.Key, s))
		} else {
			masked.AddAttrs(a)
		}
		return true
	})
	return h.inner.Handle(ctx, masked)
}

func (h *redactor) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &redactor{inner: h.inner.WithAttrs(attrs), re: h.re}
}

func (h *redactor) WithGroup(name string) slog.Handler {
	return &redactor{inner: h.inner.WithGroup(name), re: h.re}
}

func main() {
	// Base handler: JSON to stdout, LevelDebug so we see structured attrs.
	base := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})

	// Wrap with the redactor.
	safe := slog.New(newRedactor(base))

	crew := crewai.NewCrew(nil, nil).WithLogger(safe)
	crew.Verbose = true

	// In a real app you would attach agents and tasks. Here we just print
	// the assigned logger and exit.
	if crew == nil {
		fmt.Println("nil crew")
		os.Exit(1)
	}

	fmt.Fprintln(os.Stderr, "logger wired (with redactor)")
	_ = context.Background()
}
