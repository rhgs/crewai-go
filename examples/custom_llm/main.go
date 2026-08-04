// Exemplo de LLM customizado: qualquer tipo que implemente a interface
// crewai.LLM pode ser usado. Aqui criamos um LLM "eco" totalmente offline,
// útil para demonstrar a integração sem custo de API.
//
// Execução:
//
//	go run ./examples/custom_llm
package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/rhgs/crewai-go"
)

// echoLLM é um LLM determinístico que não faz nenhuma chamada de rede.
type echoLLM struct{}

func (echoLLM) Model() string { return "echo-1" }

func (echoLLM) Call(_ context.Context, messages []crewai.Message) (string, error) {
	// Devolve a última mensagem do usuário em ordem invertida, apenas para
	// demonstrar que a integração funciona.
	var last string
	for _, m := range messages {
		if m.Role == crewai.RoleUser {
			last = m.Content
		}
	}
	runes := []rune(last)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return "Resposta do echoLLM: " + strings.TrimSpace(string(runes)), nil
}

func main() {
	agente := crewai.NewAgent(
		"Agente de Demonstração",
		"Demonstrar um LLM customizado",
		"Você usa um modelo offline.",
		echoLLM{},
	)
	tarefa := crewai.NewTask("Olá, mundo!", "qualquer coisa", agente)

	crew := crewai.NewCrew([]*crewai.Agent{agente}, []*crewai.Task{tarefa})
	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(out.Final)
}
