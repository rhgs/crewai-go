// Ollama example: uses a local model or Ollama Cloud.
//
// Local (requires `ollama serve` running and the model downloaded):
//
//	ollama pull llama3.2
//	go run ./examples/ollama
//
// Cloud (requires an account on ollama.com):
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
		// Ollama Cloud: hosted model, authenticated by OLLAMA_API_KEY.
		llm = ollama.NewCloud("gpt-oss:120b")
		fmt.Println("Using Ollama Cloud (gpt-oss:120b)")
	} else {
		// Local Ollama: no auth, at http://localhost:11434.
		llm = ollama.New("llama3.2")
		fmt.Println("Using local Ollama (llama3.2)")
	}

	assistant := crewai.NewAgent(
		"Technical Assistant",
		"Explain programming concepts clearly",
		"You are didactic and straight to the point.",
		llm,
	)
	task := crewai.NewTask(
		"Explain in 2 sentences what a goroutine is in Go.",
		"Two clear sentences.",
		assistant,
	)

	crew := crewai.NewCrew([]*crewai.Agent{assistant}, []*crewai.Task{task})
	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("\n" + out.Final)
}
