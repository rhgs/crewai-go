// Command native_tools demonstrates native tool calling with a mock LLM.
// No API key needed — uses llm/mock.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/rhgs/crewai-go"
	"github.com/rhgs/crewai-go/llm/mock"
)

func main() {
	// Create a tool the agent can call.
	calc := crewai.NewTool(
		"calculator",
		"Evaluates an arithmetic expression. Input: the expression as a string.",
		func(_ context.Context, input string) (string, error) {
			return "4", nil // simplified; real usage would evaluate
		},
	)

	// Create a mock LLM with scripted tool call responses.
	m := &mock.LLM{
		ModelName: "mock",
		ToolCallResponses: []*crewai.ToolCallResponse{
			// First call: the model requests to use the calculator.
			{
				ToolCalls: []crewai.ToolCall{{
					ID: "call_1",
					Function: crewai.ToolCallFunction{
						Name:      "calculator",
						Arguments: json.RawMessage(`"2+2"`),
					},
				}},
			},
			// Second call: the model has the result and produces the final answer.
			{
				Content: "The result of 2+2 is 4.",
			},
		},
	}

	// Create an agent with native tool calling enabled.
	agent := crewai.NewAgent("Math Agent", "Solve math problems", "You are a math expert.", m)
	agent.ToolMode = crewai.ToolModeNative
	agent.Tools = []crewai.Tool{calc}

	// Create a task and crew.
	task := crewai.NewTask("Calculate 2+2 and explain the result.", "A short answer.", agent)
	crew := crewai.NewCrew([]*crewai.Agent{agent}, []*crewai.Task{task})

	// Run the crew.
	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Final output:", out.Final)
	fmt.Println()

	// Show the tool call traces.
	for _, taskOut := range out.TasksOutput {
		for _, trace := range taskOut.ToolTraces {
			fmt.Printf("Tool trace: %s(%s) -> %s (failed=%v, duration=%v)\n",
				trace.Tool, string(trace.Args), trace.Output, trace.Failed, trace.Duration)
		}
	}
}
