package crewai_test

import (
	"context"
	"fmt"

	"github.com/rhgs/crewai-go"
	"github.com/rhgs/crewai-go/llm/mock"
)

// ExampleCrew_Kickoff demonstrates a sequential crew of two tasks using the
// mock LLM (deterministic, no network).
func ExampleCrew_Kickoff() {
	// In production, swap the mock for openai.New(...) or anthropic.New(...).
	llm := mock.New(
		"Go has lightweight goroutines and channels.", // 1st task output
		"Go is great for concurrency.",                // 2nd task output
	)

	researcher := crewai.NewAgent("Researcher", "research", "", llm)
	writer := crewai.NewAgent("Writer", "write", "", llm)

	research := crewai.NewTask("Research concurrency in Go.", "facts", researcher)
	research.Name = "Research"
	summary := crewai.NewTask("Summarize in one sentence.", "sentence", writer).
		WithContext(research)

	crew := crewai.NewCrew(
		[]*crewai.Agent{researcher, writer},
		[]*crewai.Task{research, summary},
	)

	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		panic(err)
	}
	fmt.Println(out.Final)
	// Output: Go is great for concurrency.
}

// ExampleNewTool demonstrates creating a tool from a function.
func ExampleNewTool() {
	greet := crewai.NewTool(
		"greet",
		"Greets by name. Input: the name.",
		func(_ context.Context, name string) (string, error) {
			return "Hello, " + name + "!", nil
		},
	)
	out, _ := greet.Call(context.Background(), "Ada")
	fmt.Println(out)
	// Output: Hello, Ada!
}
