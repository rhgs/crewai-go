package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rhgs/crewai-go"
)

// CurrentTime returns a tool that reports the current date and time.
// The layout follows the time package convention; if empty, RFC3339 is used.
func CurrentTime(layout string) crewai.Tool {
	if layout == "" {
		layout = time.RFC3339
	}
	return crewai.NewTool(
		"current_time",
		"Reports the current date and time. The input is ignored.",
		func(_ context.Context, _ string) (string, error) {
			return time.Now().Format(layout), nil
		},
	)
}

// WordCount returns a tool that counts words and characters in the input text.
func WordCount() crewai.Tool {
	return crewai.NewTool(
		"word_count",
		"Counts the number of words and characters in the input text.",
		func(_ context.Context, input string) (string, error) {
			words := len(strings.Fields(input))
			chars := len([]rune(input))
			return fmt.Sprintf("words=%d characters=%d", words, chars), nil
		},
	)
}
