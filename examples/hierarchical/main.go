// Hierarchical example: a manager (ManagerLLM) delegates each task to the
// most suitable team member. The tasks have no fixed agent — delegation is
// decided at runtime.
//
// Run:
//
//	export OPENAI_API_KEY=sk-...
//	go run ./examples/hierarchical
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

	dev := crewai.NewAgent(
		"Go Developer",
		"Implement and explain idiomatic Go code",
		"You master Go and engineering best practices.",
		llm,
	)
	qa := crewai.NewAgent(
		"QA Engineer",
		"Ensure quality by writing tests and reviewing code",
		"You are meticulous and think about edge cases.",
		llm,
	)

	// No Agent set: the manager decides who runs it.
	implement := crewai.NewTask(
		"Write a Go function that reverses a string respecting UTF-8 runes.",
		"Commented Go code.",
		nil,
	)
	implement.Name = "Implementation"

	test := crewai.NewTask(
		"Write unit tests for the string reversal function.",
		"A _test.go file with edge cases.",
		nil,
	)
	test.Name = "Tests"

	crew := crewai.NewCrew(
		[]*crewai.Agent{dev, qa},
		[]*crewai.Task{implement, test},
	)
	crew.Process = crewai.Hierarchical
	crew.ManagerLLM = llm // the manager is created automatically from the LLM
	crew.Verbose = true

	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		log.Fatal(err)
	}

	for _, to := range out.TasksOutput {
		fmt.Printf("\n=== %s → delegated to %s ===\n%s\n", to.Task, to.Agent, to.Output)
	}
}
