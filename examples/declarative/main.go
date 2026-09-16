// Offline declarative crew: JSON-subset file + mock LLM map.
//
// Run:
//
//	go run ./examples/declarative
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
	return "Final Answer: declarative ok", nil
}

func main() {
	dir := tdir()
	path := filepath.Join(dir, "crew.json")
	doc := `{
  "agents": [
    {"name": "writer", "role": "Writer", "goal": "write clearly", "llm": "echo"}
  ],
  "tasks": [
    {"name": "draft", "description": "Draft a one-line status.", "agent": "writer", "expected_output": "one line"}
  ],
  "crew": {"name": "desk", "process": "sequential"}
}`
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	cfg, err := crewai.LoadCrewFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	crew, err := cfg.Build(crewai.WithLLMMap(map[string]crewai.LLM{"echo": echoLLM{}}))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(out.Final)
}

func tdir() string {
	d, err := os.MkdirTemp("", "crewai-decl-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	return d
}
