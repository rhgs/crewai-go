package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rodolphosa/crewai-go"
)

// CurrentTime devolve uma ferramenta que informa a data e hora atuais.
// O layout segue o padrão do pacote time; se vazio, usa RFC3339.
func CurrentTime(layout string) crewai.Tool {
	if layout == "" {
		layout = time.RFC3339
	}
	return crewai.NewTool(
		"hora_atual",
		"Informa a data e a hora atuais. A entrada é ignorada.",
		func(_ context.Context, _ string) (string, error) {
			return time.Now().Format(layout), nil
		},
	)
}

// WordCount devolve uma ferramenta que conta palavras e caracteres de um texto.
func WordCount() crewai.Tool {
	return crewai.NewTool(
		"contador_palavras",
		"Conta o número de palavras e de caracteres do texto de entrada.",
		func(_ context.Context, input string) (string, error) {
			words := len(strings.Fields(input))
			chars := len([]rune(input))
			return fmt.Sprintf("palavras=%d caracteres=%d", words, chars), nil
		},
	)
}
