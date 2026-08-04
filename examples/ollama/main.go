// Exemplo Ollama: usa um modelo local ou o Ollama Cloud.
//
// Local (requer `ollama serve` rodando e o modelo baixado):
//
//	ollama pull llama3.2
//	go run ./examples/ollama
//
// Cloud (requer conta no ollama.com):
//
//	export OLLAMA_API_KEY=...
//	OLLAMA_CLOUD=1 go run ./examples/ollama
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/rhgs/crewai-go"
	"github.com/rhgs/crewai-go/llm/ollama"
)

func main() {
	var llm crewai.LLM
	if os.Getenv("OLLAMA_CLOUD") != "" {
		// Ollama Cloud: modelo hospedado, autenticado por OLLAMA_API_KEY.
		llm = ollama.NewCloud("gpt-oss:120b")
		fmt.Println("Usando Ollama Cloud (gpt-oss:120b)")
	} else {
		// Ollama local: sem autenticação, em http://localhost:11434.
		llm = ollama.New("llama3.2")
		fmt.Println("Usando Ollama local (llama3.2)")
	}

	assistente := crewai.NewAgent(
		"Assistente Técnico",
		"Explicar conceitos de programação com clareza",
		"Você é didático e direto ao ponto.",
		llm,
	)
	tarefa := crewai.NewTask(
		"Explique em 2 frases o que é uma goroutine em Go.",
		"Duas frases claras.",
		assistente,
	)

	crew := crewai.NewCrew([]*crewai.Agent{assistente}, []*crewai.Task{tarefa})
	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("\n" + out.Final)
}
