// Staged example: two researchers gather information in parallel (stage 1),
// then a writer synthesizes their findings into an article (stage 2). The
// output of the first stage is passed as context to the second.
//
// Run:
//
//	export OPENAI_API_KEY=sk-...
//	go run ./examples/staged
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

	researcherA := crewai.NewAgent(
		"Researcher A",
		"Find the advantages of Go for concurrency",
		"You are a technical analyst focused on concurrency.",
		llm,
	)
	researcherB := crewai.NewAgent(
		"Researcher B",
		"Find the challenges of Go for concurrency",
		"You are a technical analyst focused on trade-offs.",
		llm,
	)
	writer := crewai.NewAgent(
		"Technical Writer",
		"Turn research into clear, engaging content",
		"You write for developers, with clarity and precision.",
		llm,
	)

	advantages := crewai.NewTask(
		"List 3 advantages of using Go for concurrent systems.",
		"A list of 3 items with one explanatory sentence each.",
		researcherA,
	)
	advantages.Name = "Advantages"

	challenges := crewai.NewTask(
		"List 3 challenges of using Go for concurrent systems.",
		"A list of 3 items with one explanatory sentence each.",
		researcherB,
	)
	challenges.Name = "Challenges"

	article := crewai.NewTask(
		"Write an introductory blog paragraph using the research.",
		"A paragraph of ~120 words.",
		writer,
	).WithContext(advantages, challenges)

	crew := crewai.NewCrew(
		[]*crewai.Agent{researcherA, researcherB, writer},
		nil,
	)
	crew.Process = crewai.Staged
	crew.Stages = []crewai.Stage{
		{Name: "collect", Tasks: []*crewai.Task{advantages, challenges}},
		{Name: "synthesize", Tasks: []*crewai.Task{article}},
	}
	crew.Verbose = true

	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		log.Fatal(err)
	}

	for _, to := range out.TasksOutput {
		fmt.Printf("\n=== %s (%s) ===\n%s\n", to.Task, to.Agent, to.Output)
	}
}
