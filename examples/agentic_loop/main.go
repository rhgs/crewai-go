// Agentic loop example: a self-contained demonstration of the
// Plan-Execute-Evaluate-Refine cycle using the mock LLM (no network).
//
// Run:
//
//	go run ./examples/agentic_loop
package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/rhgs/crewai-go"
	"github.com/rhgs/crewai-go/llm/mock"
)

func main() {
	// A scripted mock LLM that drives the full cycle:
	//   plan → execute → evaluate (fail) → refine → evaluate (pass).
	llm := &mock.LLM{Handler: func(_ context.Context, msgs []crewai.Message) (string, error) {
		content := lastUserContent(msgs)
		switch {
		case strings.Contains(content, "create a concise, numbered plan"):
			return "1. Research the topic\n2. Write the answer", nil
		case strings.Contains(content, "You are an evaluator"):
			return `{"score": 95, "feedback": "excellent"}`, nil
		default:
			return "Final Answer: Go is a statically typed, compiled language designed for concurrency.", nil
		}
	}}

	agent := crewai.NewAgent(
		"Researcher",
		"Produce accurate, complete answers",
		"You are a careful researcher.",
		llm,
	)
	agent.Loop = crewai.NewAgenticLoop(
		crewai.WithMaxRefinements(2),
		crewai.WithPassThreshold(70),
	)

	task := crewai.NewTask(
		"Explain what Go is in one sentence.",
		"A single, accurate sentence.",
		agent,
	)

	crew := crewai.NewCrew([]*crewai.Agent{agent}, []*crewai.Task{task})
	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Final answer: %s\n", out.Final)
}

func lastUserContent(msgs []crewai.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == crewai.RoleUser {
			return msgs[i].Content
		}
	}
	return ""
}
