package crewai

import (
	"log"
	"os"
)

// Logger é a interface de log usada internamente pela crew. Permite silenciar
// ou redirecionar a saída conforme a necessidade da aplicação.
type Logger interface {
	Infof(format string, args ...any)
	Debugf(format string, args ...any)
}

// stdLogger imprime mensagens na saída de erro padrão. Debug só é impresso
// quando verbose está ativo.
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

// nopLogger descarta todas as mensagens.
type nopLogger struct{}

func (nopLogger) Infof(string, ...any)  {}
func (nopLogger) Debugf(string, ...any) {}
