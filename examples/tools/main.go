// Exemplo com ferramentas: um agente usa a calculadora embutida (e uma
// ferramenta customizada) via o protocolo ReAct para resolver a tarefa.
//
// Execução:
//
//	export OPENAI_API_KEY=sk-...
//	go run ./examples/tools
package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/rhgs/crewai-go"
	"github.com/rhgs/crewai-go/llm/openai"
	"github.com/rhgs/crewai-go/tools"
)

func main() {
	llm := openai.New("gpt-4o-mini")

	// Ferramenta customizada criada a partir de uma função Go.
	maiusculas := crewai.NewTool(
		"maiusculas",
		"Converte o texto de entrada para MAIÚSCULAS.",
		func(_ context.Context, in string) (string, error) {
			return strings.ToUpper(in), nil
		},
	)

	analista := crewai.NewAgent(
		"Analista Financeiro",
		"Fazer cálculos precisos e apresentar resultados",
		"Você jamais calcula de cabeça: sempre usa a calculadora.",
		llm,
	).WithTools(tools.Calculator(), maiusculas)

	tarefa := crewai.NewTask(
		"Se investirmos 1500 e o retorno for de 12% ao ano, qual o valor após 1 ano? "+
			"Responda em uma frase em MAIÚSCULAS.",
		"Uma frase com o valor final.",
		analista,
	)

	crew := crewai.NewCrew([]*crewai.Agent{analista}, []*crewai.Task{tarefa})
	crew.Verbose = true

	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("\n=== Resultado ===")
	fmt.Println(out.Final)
}
