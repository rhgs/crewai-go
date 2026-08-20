// Example: inter-agent delegation via delegate_to_coworker.
//
// The writer may ask the researcher (AllowDelegation=true) for help
// mid-reasoning. Crew.EnableDelegationTool auto-attaches the tool to
// every agent at Kickoff. Targets must set AllowDelegation.
//
// Live run:
//
//	export OPENAI_API_KEY=sk-...
//	go run ./examples/delegation
//
// Without a key, prints the tool wiring and exits 0.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/rhgs/crewai-go"
	"github.com/rhgs/crewai-go/llm/openai"
)

func main() {
	if os.Getenv("OPENAI_API_KEY") == "" {
		printWiring()
		return
	}

	llm := openai.New("gpt-4o-mini")

	researcher := crewai.NewAgent(
		"Researcher",
		"Find concise factual answers",
		"You look up facts carefully and answer briefly.",
		llm,
	)
	researcher.AllowDelegation = true // eligible target

	writer := crewai.NewAgent(
		"Writer",
		"Write a short brief using research help when needed",
		"You write clearly and may ask the Researcher for facts.",
		llm,
	)

	task := crewai.NewTask(
		"Write a two-sentence brief about why Go's goroutines are useful. "+
			"If you need a precise definition, delegate to the Researcher.",
		"A two-sentence brief.",
		writer,
	)

	crew := crewai.NewCrew(
		[]*crewai.Agent{writer, researcher},
		[]*crewai.Task{task},
	)
	crew.EnableDelegationTool = true
	crew.Verbose = true

	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "kickoff:", err)
		os.Exit(1)
	}
	fmt.Println(out.Final)
}

func printWiring() {
	researcher := crewai.NewAgent("Researcher", "g", "b", nil)
	researcher.AllowDelegation = true
	writer := crewai.NewAgent("Writer", "g", "b", nil)
	crew := crewai.NewCrew([]*crewai.Agent{writer, researcher}, nil)
	tool := crewai.NewDelegationTool(crew)
	writer.WithTools(tool)
	fmt.Printf("tool=%s\n", tool.Name())
	fmt.Printf("researcher.AllowDelegation=%v\n", researcher.AllowDelegation)
	fmt.Println("OK — set OPENAI_API_KEY to run a live Kickoff")
}
