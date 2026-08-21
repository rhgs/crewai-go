// Offline demo of Crew.WithEvents: prints lifecycle CrewEvents as lines.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/rhgs/crewai-go"
	"github.com/rhgs/crewai-go/llm/mock"
)

func main() {
	llm := mock.New("Final Answer: hello from events demo")
	agent := crewai.NewAgent("writer", "write", "brief", llm)
	task := crewai.NewTask("say hello", "one line", agent)
	task.Name = "hello"

	crew := crewai.NewCrew([]*crewai.Agent{agent}, []*crewai.Task{task}).
		WithEvents(func(ev crewai.CrewEvent) {
			b, _ := json.Marshal(ev)
			fmt.Println(string(b))
		})

	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("--- final ---")
	fmt.Println(out.Final)
}
