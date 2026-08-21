// Async tasks example: two independent research tasks run in parallel under
// the sequential process (Task.Async), then a merge task waits on both via
// explicit WithContext. Memory is NOT used as the sibling merge channel —
// that is the API invariant promoted from the DEV.to semantic-race thread.
//
// Run (offline mock):
//
//	go run ./examples/async_tasks
//
// Or with a real provider:
//
//	export OPENAI_API_KEY=sk-...
//	USE_OPENAI=1 go run ./examples/async_tasks
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/rhgs/crewai-go"
	"github.com/rhgs/crewai-go/llm/mock"
	"github.com/rhgs/crewai-go/llm/openai"
)

func main() {
	var llm crewai.LLM
	if os.Getenv("USE_OPENAI") != "" {
		llm = openai.New("gpt-4o-mini")
	} else {
		// Offline mock: slow "alpha", fast "beta", then a merge that echoes both.
		llm = &mock.LLM{Handler: func(_ context.Context, msgs []crewai.Message) (string, error) {
			var all string
			for _, m := range msgs {
				all += m.Content
			}
			// Match on the task description first so the merge task (which
			// receives alpha/beta outputs via WithContext) is not misrouted.
			switch {
			case strings.Contains(all, "Combine the alpha and beta"):
				return "merged: saw both inputs", nil
			case strings.Contains(all, "alpha aspect"):
				time.Sleep(80 * time.Millisecond)
				return "alpha-findings", nil
			case strings.Contains(all, "beta aspect"):
				return "beta-findings", nil
			default:
				return "merged: saw both inputs", nil
			}
		}}
	}

	researcher := crewai.NewAgent(
		"Researcher",
		"Gather concise findings",
		"You are brief and factual.",
		llm,
	)

	// Two independent Async tasks — they share a wave and run concurrently.
	alpha := crewai.NewTask(
		"Research the alpha aspect of {topic}.",
		"A short bullet list.",
		researcher,
	).WithAsync()
	alpha.Name = "alpha"

	beta := crewai.NewTask(
		"Research the beta aspect of {topic}.",
		"A short bullet list.",
		researcher,
	).WithAsync()
	beta.Name = "beta"

	// Merge depends on BOTH via WithContext (not Memory).
	merge := crewai.NewTask(
		"Combine the alpha and beta findings into one short paragraph.",
		"One paragraph.",
		researcher,
	).WithContext(alpha, beta)
	merge.Name = "merge"

	crew := crewai.NewCrew(
		[]*crewai.Agent{researcher},
		[]*crewai.Task{alpha, beta, merge},
	)
	// NewCrew defaults: AsyncMaxWorkers=8, AsyncFailFast=true.
	// Explicit override example:
	// crew.WithAsyncMaxWorkers(2)

	start := time.Now()
	out, err := crew.Kickoff(context.Background(), map[string]string{
		"topic": "Go concurrency",
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("elapsed: %s (two Async tasks should overlap)\n", time.Since(start).Round(time.Millisecond))
	for _, to := range out.TasksOutput {
		fmt.Printf("\n=== %s (%s) ===\n%s\n", to.Task, to.Agent, to.Output)
	}
	fmt.Printf("\nFinal: %s\n", out.Final)
}
