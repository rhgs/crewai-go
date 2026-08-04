// Exemplo sequencial: um pesquisador coleta informações e um redator
// transforma o resultado em um artigo. A saída da 1ª tarefa é passada como
// contexto para a 2ª.
//
// Execução:
//
//	export OPENAI_API_KEY=sk-...
//	go run ./examples/sequential
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/rhgs/crewai-go"
	"github.com/rhgs/crewai-go/llm/openai"
)

func main() {
	llm := openai.New("gpt-4o-mini")

	pesquisador := crewai.NewAgent(
		"Pesquisador de Tecnologia",
		"Descobrir os pontos mais relevantes sobre um tema",
		"Você é analista técnico, objetivo e detalhista.",
		llm,
	)
	redator := crewai.NewAgent(
		"Redator Técnico",
		"Transformar pesquisa em conteúdo claro e envolvente",
		"Você escreve para desenvolvedores, com clareza e precisão.",
		llm,
	)

	pesquisa := crewai.NewTask(
		"Liste 5 vantagens de usar Go para sistemas concorrentes sobre o tema {tema}.",
		"Uma lista de 5 itens com uma frase de explicação cada.",
		pesquisador,
	)
	pesquisa.Name = "Pesquisa"

	artigo := crewai.NewTask(
		"Escreva um parágrafo introdutório de blog usando a pesquisa.",
		"Um parágrafo de ~120 palavras.",
		redator,
	).WithContext(pesquisa)

	crew := crewai.NewCrew(
		[]*crewai.Agent{pesquisador, redator},
		[]*crewai.Task{pesquisa, artigo},
	)
	crew.Process = crewai.Sequential
	crew.Verbose = true
	crew.Memory = true

	out, err := crew.Kickoff(context.Background(), map[string]string{
		"tema": "concorrência",
	})
	if err != nil {
		log.Fatal(err)
	}

	for _, to := range out.TasksOutput {
		fmt.Printf("\n=== %s (%s) ===\n%s\n", to.Task, to.Agent, to.Output)
	}
}
