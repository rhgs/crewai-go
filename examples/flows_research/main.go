// Offline Flow demo: bootstrap → research ∥ news → router → write → done.
//
// Run:
//
//	go run ./examples/flows_research
package main

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/rhgs/crewai-go"
)

type researchState struct {
	mu      sync.Mutex
	notes   []string
	route   string
	article string
}

func (s *researchState) add(note string) {
	s.mu.Lock()
	s.notes = append(s.notes, note)
	s.mu.Unlock()
}

func main() {
	f := crewai.NewFlow[researchState]().
		Start("bootstrap", func(_ context.Context, s *researchState) error {
			s.route = "write"
			s.add("topic: crewai-go flows")
			return nil
		}).
		Listen("research", func(_ context.Context, s *researchState) error {
			s.add("research: typed Flow[S], no reflection")
			return nil
		}, "bootstrap").
		Listen("news", func(_ context.Context, s *researchState) error {
			s.add("news: P3 train F")
			return nil
		}, "bootstrap").
		Router("choose", func(_ context.Context, s *researchState) ([]string, error) {
			return []string{s.route}, nil
		}, "research", "news").
		Listen("write", func(_ context.Context, s *researchState) error {
			s.mu.Lock()
			s.article = "Flows ship as Flow[S] + Start/Listen/Router."
			s.mu.Unlock()
			s.add("wrote")
			return nil
		}, "choose").
		Listen("skip", func(_ context.Context, s *researchState) error {
			s.add("should-not-run")
			return nil
		}, "choose")

	res, err := f.Run(context.Background(), researchState{})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("article:", res.State.article)
	fmt.Println("notes:", res.State.notes)
	fmt.Print("steps:")
	for _, st := range res.Steps {
		if st.Skipped {
			fmt.Printf(" %s(skipped)", st.Name)
			continue
		}
		fmt.Printf(" %s", st.Name)
	}
	fmt.Println()
}
