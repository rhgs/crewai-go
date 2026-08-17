package crewai

import (
	"io"
	"log/slog"
	"testing"
)

// testLogger returns a *slog.Logger that discards all output. It is the
// drop-in replacement for the removed nopLogger used by tests that
// invoke executor functions directly. The level is LevelError so info
// and debug are suppressed unless a test inspects the buffer.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))
}

// capturingLogger returns a *slog.Logger writing structured text to buf
// at LevelDebug. It is used by tests that need to assert on log lines.
func capturingLogger(buf io.Writer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
}

// discardIfUnused ensures the slog package import is never flagged even
// if a future test stops using testLogger directly.
var _ = func(t *testing.T) *slog.Logger { return testLogger() }
