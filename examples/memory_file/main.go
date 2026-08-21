// FileStore example: persist memory across two Kickoffs using the stdlib
// JSONL backend. The app owns Open/Close (D-M6). The root path is
// caller-trusted — never pass model-controlled paths.
//
// Run (offline):
//
//	go run ./examples/memory_file
//
// Optional root:
//
//	MEMORY_DIR=/tmp/crew-mem go run ./examples/memory_file
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/rhgs/crewai-go"
)

// echoLLM returns a short deterministic answer derived from the last user turn.
type echoLLM struct{ tag string }

func (e echoLLM) Model() string { return "echo-" + e.tag }
func (e echoLLM) Call(_ context.Context, msgs []crewai.Message) (string, error) {
	var last string
	for _, m := range msgs {
		if m.Role == crewai.RoleUser {
			last = m.Content
		}
	}
	// Keep it short so MaxChars budgets stay relevant.
	line := strings.TrimSpace(last)
	if len(line) > 80 {
		line = line[:80] + "…"
	}
	return fmt.Sprintf("[%s] noted: %s", e.tag, line), nil
}

func main() {
	dir := os.Getenv("MEMORY_DIR")
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "crewai-memory-file-example")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		log.Fatal(err)
	}
	fmt.Println("FileStore root:", dir)

	store, err := crewai.OpenFileStore(dir)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	// --- Kickoff 1: write a memory entry ---
	a1 := crewai.NewAgent("Analyst", "Record findings", "You are brief.", echoLLM{tag: "run1"})
	t1 := crewai.NewTask("Summarize the Q1 revenue trend for Acme.", "one sentence", a1)
	t1.Name = "q1"

	crew1 := crewai.NewCrew([]*crewai.Agent{a1}, []*crewai.Task{t1})
	crew1.Name = "finance-crew" // becomes default MemoryPolicy.Scope
	crew1.MemoryStore = store
	crew1.MemoryPolicy = crewai.NewMemoryPolicy()

	out1, err := crew1.Kickoff(context.Background(), nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Kickoff 1 final:", out1.Final)

	// --- Kickoff 2: fresh crew, same store — injects committed memory ---
	var saw string
	a2 := crewai.NewAgent("Analyst", "Recall prior work", "You are brief.",
		&captureEcho{tag: "run2", saw: &saw})
	t2 := crewai.NewTask("What did we already learn about Acme?", "one sentence", a2)
	t2.Name = "recall"

	crew2 := crewai.NewCrew([]*crewai.Agent{a2}, []*crewai.Task{t2})
	crew2.Name = "finance-crew"
	crew2.MemoryStore = store
	crew2.MemoryPolicy = crewai.NewMemoryPolicy()

	out2, err := crew2.Kickoff(context.Background(), nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Kickoff 2 final:", out2.Final)
	if strings.Contains(saw, "## Memory (recalled)") && strings.Contains(saw, "run1") {
		fmt.Println("OK: second Kickoff injected FileStore memory from the first run.")
	} else {
		fmt.Println("WARN: memory block not observed in second prompt; saw =", saw)
	}

	// Direct store query (ops / debugging).
	hits, err := store.Query(context.Background(), crewai.MemoryQuery{
		Scope: "finance-crew",
		Limit: 5,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Store holds %d entr(y/ies) in scope finance-crew\n", len(hits))
	for _, h := range hits {
		fmt.Printf("  - [%s] %s\n", h.Agent, h.Content)
	}
}

// captureEcho is an echo LLM that also records the full prompt.
type captureEcho struct {
	tag string
	saw *string
}

func (c *captureEcho) Model() string { return "echo-" + c.tag }
func (c *captureEcho) Call(_ context.Context, msgs []crewai.Message) (string, error) {
	var b strings.Builder
	var last string
	for _, m := range msgs {
		b.WriteString(m.Content)
		b.WriteByte('\n')
		if m.Role == crewai.RoleUser {
			last = m.Content
		}
	}
	if c.saw != nil {
		*c.saw = b.String()
	}
	line := strings.TrimSpace(last)
	if len(line) > 80 {
		line = line[:80] + "…"
	}
	return fmt.Sprintf("[%s] noted: %s", c.tag, line), nil
}
