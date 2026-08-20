// Example: structured logging with redaction.
//
// This example shows how to plug a custom *slog.Logger into a Crew and how
// to wrap the handler with crewai.RedactHandler so that secrets (API keys,
// bearer tokens, query-string credentials) never reach the log destination.
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

	"github.com/rhgs/crewai-go"
)

func main() {
	// Base handler: JSON to stdout, LevelDebug so we see structured attrs.
	base := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})

	// Wrap with the library redactor (best-effort, opt-in).
	safe := slog.New(crewai.RedactHandler(base))

	crew := crewai.NewCrew(nil, nil).WithLogger(safe)
	crew.Verbose = true

	// In a real app you would attach agents and tasks. Here we just print
	// the assigned logger and exit.
	if crew == nil {
		fmt.Println("nil crew")
		os.Exit(1)
	}

	fmt.Fprintln(os.Stderr, "logger wired (with crewai.RedactHandler)")
	_ = context.Background()
}
