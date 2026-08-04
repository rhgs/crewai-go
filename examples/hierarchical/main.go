// Exemplo hierárquico: um gerente (ManagerLLM) delega cada tarefa ao membro
// da equipe mais adequado. As tarefas não têm agente fixo — a delegação é
// decidida em tempo de execução.
//
// Execução:
//
//	export OPENAI_API_KEY=sk-...
//	go run ./examples/hierarchical
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/rodolphosa/crewai-go"
	"github.com/rodolphosa/crewai-go/llm/openai"
)

func main() {
	llm := openai.New("gpt-4o-mini")

	dev := crewai.NewAgent(
		"Desenvolvedor Go",
		"Implementar e explicar código Go idiomático",
		"Você domina Go e boas práticas de engenharia.",
		llm,
	)
	qa := crewai.NewAgent(
		"Engenheiro de QA",
		"Garantir qualidade escrevendo testes e revisando código",
		"Você é meticuloso e pensa em casos de borda.",
		llm,
	)

	// Sem Agent definido: o gerente decide quem executa.
	implementar := crewai.NewTask(
		"Escreva uma função Go que inverte uma string respeitando runes UTF-8.",
		"Código Go comentado.",
		nil,
	)
	implementar.Name = "Implementação"

	testar := crewai.NewTask(
		"Escreva testes de unidade para a função de inversão de string.",
		"Um arquivo _test.go com casos de borda.",
		nil,
	)
	testar.Name = "Testes"

	crew := crewai.NewCrew(
		[]*crewai.Agent{dev, qa},
		[]*crewai.Task{implementar, testar},
	)
	crew.Process = crewai.Hierarchical
	crew.ManagerLLM = llm // o gerente é criado automaticamente a partir do LLM
	crew.Verbose = true

	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		log.Fatal(err)
	}

	for _, to := range out.TasksOutput {
		fmt.Printf("\n=== %s → delegado a %s ===\n%s\n", to.Task, to.Agent, to.Output)
	}
}
