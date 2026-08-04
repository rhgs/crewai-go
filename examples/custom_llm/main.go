// Custom LLM example: any type implementing the crewai.LLM interface can be
// used. Here we create a fully offline "echo" LLM, useful for demonstrating
// the integration with no API cost.
//
// Run:
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

// echoLLM is a deterministic LLM that makes no network calls.
type echoLLM struct{}

func (echoLLM) Model() string { return "echo-1" }

func (echoLLM) Call(_ context.Context, messages []crewai.Message) (string, error) {
	// Returns the last user message reversed, just to demonstrate that the
	// integration works.
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
	return "echoLLM response: " + strings.TrimSpace(string(runes)), nil
}

func main() {
	agent := crewai.NewAgent(
		"Demo Agent",
		"Demonstrate a custom LLM",
		"You use an offline model.",
		echoLLM{},
	)
	task := crewai.NewTask("Hello, world!", "anything", agent)

	crew := crewai.NewCrew([]*crewai.Agent{agent}, []*crewai.Task{task})
	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(out.Final)
}
