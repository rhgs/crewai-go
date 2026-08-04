package crewai_test

import (
	"context"
	"fmt"

	"github.com/rodolphosa/crewai-go"
	"github.com/rodolphosa/crewai-go/llm/mock"
)

// ExampleCrew_Kickoff demonstra uma crew sequencial de duas tarefas usando o
// LLM mock (determinístico, sem rede).
func ExampleCrew_Kickoff() {
	// Em produção, troque o mock por openai.New(...) ou anthropic.New(...).
	llm := mock.New(
		"Go tem goroutines leves e canais.", // saída da 1ª tarefa
		"Go é ótimo para concorrência.",     // saída da 2ª tarefa
	)

	pesquisador := crewai.NewAgent("Pesquisador", "pesquisar", "", llm)
	redator := crewai.NewAgent("Redator", "escrever", "", llm)

	pesquisa := crewai.NewTask("Pesquise sobre concorrência em Go.", "fatos", pesquisador)
	pesquisa.Name = "Pesquisa"
	resumo := crewai.NewTask("Resuma em uma frase.", "frase", redator).
		WithContext(pesquisa)

	crew := crewai.NewCrew(
		[]*crewai.Agent{pesquisador, redator},
		[]*crewai.Task{pesquisa, resumo},
	)

	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		panic(err)
	}
	fmt.Println(out.Final)
	// Output: Go é ótimo para concorrência.
}

// ExampleNewTool demonstra a criação de uma ferramenta a partir de uma função.
func ExampleNewTool() {
	saudacao := crewai.NewTool(
		"saudacao",
		"Cumprimenta pelo nome. Entrada: o nome.",
		func(_ context.Context, nome string) (string, error) {
			return "Olá, " + nome + "!", nil
		},
	)
	out, _ := saudacao.Call(context.Background(), "Ada")
	fmt.Println(out)
	// Output: Olá, Ada!
}
