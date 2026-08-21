// Offline demo of Crew.WithStream: two Async research tasks print deltas
// demuxed by Task label, then a merge task streams the final summary.
package main

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/rhgs/crewai-go"
	"github.com/rhgs/crewai-go/llm/mock"
)

func streamLLM(parts ...string) *mock.LLM {
	m := mock.New("fallback")
	chunks := make([]crewai.StreamChunk, 0, len(parts)+1)
	for i, p := range parts {
		c := crewai.StreamChunk{Delta: p}
		if i == len(parts)-1 {
			c.Done = true
		}
		chunks = append(chunks, c)
	}
	// Single scripted CallStream sequence (one Kickoff call per task LLM).
	m.StreamChunks = [][]crewai.StreamChunk{chunks}
	return m
}

func main() {
	// Separate mock LLMs so concurrent Async CallStream calls do not share
	// a streamIndex (each task owns its scripted deltas).
	llmA := streamLLM("alpha ", "findings")
	llmB := streamLLM("beta ", "notes")
	llmMerge := streamLLM("summary: ", "alpha+beta")

	aAgent := crewai.NewAgent("researcher-a", "gather notes", "concise", llmA)
	bAgent := crewai.NewAgent("researcher-b", "gather notes", "concise", llmB)
	mAgent := crewai.NewAgent("writer", "merge notes", "concise", llmMerge)

	a := crewai.NewTask("research topic A", "short notes", aAgent).WithAsync()
	a.Name = "research-a"
	b := crewai.NewTask("research topic B", "short notes", bAgent).WithAsync()
	b.Name = "research-b"
	merge := crewai.NewTask("merge findings", "one paragraph", mAgent).WithContext(a, b)
	merge.Name = "merge"

	var mu sync.Mutex
	crew := crewai.NewCrew(
		[]*crewai.Agent{aAgent, bAgent, mAgent},
		[]*crewai.Task{a, b, merge},
	).WithStream(func(c crewai.StreamChunk) {
		mu.Lock()
		defer mu.Unlock()
		if c.Delta != "" {
			fmt.Fprintf(os.Stdout, "[%s/%s] %s\n", c.Task, c.Agent, c.Delta)
		}
		if c.Err != nil {
			fmt.Fprintln(os.Stderr, "stream error:", c.Err)
		}
	})

	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "kickoff:", err)
		os.Exit(1)
	}
	fmt.Println("--- final ---")
	fmt.Println(out.Final)
}
