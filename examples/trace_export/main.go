// Offline TraceRecorder demo: metadata-only JSONL from a mock Kickoff.
//
// Run:
//
//	go run ./examples/trace_export
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rhgs/crewai-go"
)

type echoLLM struct{}

func (echoLLM) Model() string { return "echo" }
func (echoLLM) Call(_ context.Context, _ []crewai.Message) (string, error) {
	return "Final Answer: traced", nil
}

func main() {
	rec := crewai.NewTraceRecorder()
	a := crewai.NewAgent("Writer", "write", "brief", echoLLM{})
	task := crewai.NewTask("say hello", "one line", a)
	task.Name = "hello"

	crew := crewai.NewCrew([]*crewai.Agent{a}, []*crewai.Task{task}).WithTracer(rec)
	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("final:", out.Final)

	path := filepath.Join(os.TempDir(), "crewai-trace.jsonl")
	if err := rec.Save(path); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("records:", len(rec.Records()), "saved", path)
	for _, r := range rec.Records() {
		fmt.Printf("  task=%s agent=%s bodies=%v duration_ms=%d\n",
			r.Task, r.Agent, r.Output != "", r.DurationMs)
	}
}
