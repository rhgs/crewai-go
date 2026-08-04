// Basic example: a single agent executes a single task.
//
// Run:
//
//	export OPENAI_API_KEY=sk-...
//	go run ./examples/basic
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

	poet := crewai.NewAgent(
		"Poet",
		"Write short, memorable poems",
		"You are an award-winning poet, master of brevity.",
		llm,
	)

	task := crewai.NewTask(
		"Write a haiku about the Go programming language.",
		"A haiku (3 lines) in English.",
		poet,
	)

	crew := crewai.NewCrew([]*crewai.Agent{poet}, []*crewai.Task{task})
	crew.Verbose = true

	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("\n=== Result ===")
	fmt.Println(out.Final)
	fmt.Printf("\n(completed in %s)\n", out.Duration)
}
