// Memory tools (M5 / D-MT1–D-MT5): opt-in recall_memory + remember.
// Writes go to MemoryStore immediately (D-MT4), outside the D-M7 AutoSave
// buffer. Memory=true alone does not attach the tools (D-MT2).
//
// Run (offline):
//
//	go run ./examples/memory_tools
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/rhgs/crewai-go"
)

// scriptedLLM returns canned ReAct turns, then a Final Answer.
type scriptedLLM struct {
	turns []string
	i     int
}

func (s *scriptedLLM) Model() string { return "scripted" }
func (s *scriptedLLM) Call(_ context.Context, _ []crewai.Message) (string, error) {
	if s.i >= len(s.turns) {
		return "Final Answer: done", nil
	}
	out := s.turns[s.i]
	s.i++
	return out, nil
}

func main() {
	llm := &scriptedLLM{turns: []string{
		"Thought: store the finding\nAction: remember\nAction Input: Acme Q1 revenue grew 12%\n",
		"Thought: I need that number\nAction: recall_memory\nAction Input: revenue\n",
		"Thought: got it\nFinal Answer: Acme grew 12% in Q1.",
	}}
	agent := crewai.NewAgent(
		"Analyst",
		"Record and recall notes",
		"You write short notes into memory and look them up when asked.",
		llm,
	)
	task := crewai.NewTask(
		"Remember the Q1 finding, then recall it and answer with the figure.",
		"one sentence with the percentage",
		agent,
	)

	crew := crewai.NewCrew([]*crewai.Agent{agent}, []*crewai.Task{task})
	crew.Name = "finance-crew"
	crew.Memory = true
	crew.EnableMemoryTools = true // D-MT2: explicit opt-in

	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("final:", out.Final)

	// Explicit tools also work without EnableMemoryTools:
	store := crewai.NewMemory()
	crew2 := crewai.NewCrew(nil, nil)
	crew2.MemoryStore = store
	remember := crewai.NewRememberTool(crew2)
	recall := crewai.NewRecallMemoryTool(crew2)
	if _, err := remember.Call(context.Background(), "hiring plan adds 3 engineers"); err != nil {
		log.Fatal(err)
	}
	got, err := recall.Call(context.Background(), "hiring")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("explicit recall:")
	fmt.Println(got)
}
