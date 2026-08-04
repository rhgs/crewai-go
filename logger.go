package crewai

import (
	"log"
	"os"
)

// Logger is the logging interface used internally by the crew. It allows
// silencing or redirecting output as needed by the application.
type Logger interface {
	Infof(format string, args ...any)
	Debugf(format string, args ...any)
}

// stdLogger prints messages to standard error. Debug is only printed when
// verbose is on.
type stdLogger struct {
	verbose bool
	l       *log.Logger
}

func newStdLogger(verbose bool) *stdLogger {
	return &stdLogger{
		verbose: verbose,
		l:       log.New(os.Stderr, "[crewai] ", log.Ltime),
	}
}

func (s *stdLogger) Infof(format string, args ...any) {
	if s.verbose {
		s.l.Printf(format, args...)
	}
}

func (s *stdLogger) Debugf(format string, args ...any) {
	if s.verbose {
		s.l.Printf(format, args...)
	}
}

// nopLogger discards all messages.
type nopLogger struct{}

func (nopLogger) Infof(string, ...any)  {}
func (nopLogger) Debugf(string, ...any) {}
