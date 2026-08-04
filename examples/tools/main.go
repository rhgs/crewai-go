// Tools example: an agent uses the built-in calculator (and a custom tool)
// via the ReAct protocol to solve the task.
//
// Run:
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

	// Custom tool created from a Go function.
	uppercase := crewai.NewTool(
		"uppercase",
		"Converts the input text to UPPERCASE.",
		func(_ context.Context, in string) (string, error) {
			return strings.ToUpper(in), nil
		},
	)

	analyst := crewai.NewAgent(
		"Financial Analyst",
		"Make accurate calculations and present results",
		"You never calculate in your head: always use the calculator.",
		llm,
	).WithTools(tools.Calculator(), uppercase)

	task := crewai.NewTask(
		"If we invest 1500 with a 12% annual return, what is the value after 1 year? "+
			"Answer in one sentence in UPPERCASE.",
		"A sentence with the final value.",
		analyst,
	)

	crew := crewai.NewCrew([]*crewai.Agent{analyst}, []*crewai.Task{task})
	crew.Verbose = true

	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("\n=== Result ===")
	fmt.Println(out.Final)
}
