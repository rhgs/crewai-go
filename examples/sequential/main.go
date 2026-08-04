// Sequential example: a researcher gathers information and a writer turns
// the result into an article. The output of the 1st task is passed as context
// to the 2nd.
//
// Run:
//
//	export OPENAI_API_KEY=sk-...
//	go run ./examples/sequential
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

	researcher := crewai.NewAgent(
		"Technology Researcher",
		"Discover the most relevant points about a topic",
		"You are a technical analyst, objective and detail-oriented.",
		llm,
	)
	writer := crewai.NewAgent(
		"Technical Writer",
		"Turn research into clear, engaging content",
		"You write for developers, with clarity and precision.",
		llm,
	)

	research := crewai.NewTask(
		"List 5 advantages of using Go for concurrent systems on the {topic} theme.",
		"A list of 5 items with one explanatory sentence each.",
		researcher,
	)
	research.Name = "Research"

	article := crewai.NewTask(
		"Write an introductory blog paragraph using the research.",
		"A paragraph of ~120 words.",
		writer,
	).WithContext(research)

	crew := crewai.NewCrew(
		[]*crewai.Agent{researcher, writer},
		[]*crewai.Task{research, article},
	)
	crew.Process = crewai.Sequential
	crew.Verbose = true
	crew.Memory = true

	out, err := crew.Kickoff(context.Background(), map[string]string{
		"topic": "concurrency",
	})
	if err != nil {
		log.Fatal(err)
	}

	for _, to := range out.TasksOutput {
		fmt.Printf("\n=== %s (%s) ===\n%s\n", to.Task, to.Agent, to.Output)
	}
}
