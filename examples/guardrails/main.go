// Guardrails example: a provenance guardrail that blocks outputs without
// source URLs.
//
// Run:
//
//	export OPENAI_API_KEY=sk-...
//	go run ./examples/guardrails
package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/rhgs/crewai-go"
	"github.com/rhgs/crewai-go/llm/openai"
)

// provenanceGuardrail rejects any output that does not contain at least one
// source URL (http:// or https://). This is a code-enforced guarantee:
// even if the model omits a source, the output is never published.
func provenanceGuardrail(_ context.Context, out *crewai.CrewOutput) error {
	if !strings.Contains(out.Final, "http://") && !strings.Contains(out.Final, "https://") {
		return fmt.Errorf("output lacks source URL; refusing to publish unprovenanced claim")
	}
	return nil
}

func main() {
	llm := openai.New("gpt-4o-mini")

	researcher := crewai.NewAgent(
		"Researcher",
		"Research with sources",
		"You are a meticulous researcher who always cites sources.",
		llm,
	)

	task := crewai.NewTask(
		"Summarize the key facts about the Go programming language with source URLs.",
		"A summary with at least one source URL.",
		researcher,
	)

	crew := crewai.NewCrew([]*crewai.Agent{researcher}, []*crewai.Task{task})
	crew.Guardrails = []crewai.Guardrail{provenanceGuardrail}

	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		log.Fatalf("blocked: %v", err)
	}

	fmt.Println("\n=== Result ===")
	fmt.Println(out.Final)
	fmt.Printf("\n(completed in %s)\n", out.Duration)
}
