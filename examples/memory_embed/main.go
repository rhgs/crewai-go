// Memory embeddings example: AutoEmbed at the commit barrier (G8) + cosine
// Query ranking with a fully offline mock embedder (no network).
//
// Run:
//
//	go run ./examples/memory_embed
package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/rhgs/crewai-go"
)

// bagOfWordsEmbed is a tiny deterministic embedder: 3 dims for
// {revenue, risk, hiring}. Real apps swap this for an HTTP call to
// OpenAI/Ollama/etc. — same EmbeddingFunc signature.
func bagOfWordsEmbed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		l := strings.ToLower(t)
		v := []float32{0, 0, 0}
		if strings.Contains(l, "revenue") || strings.Contains(l, "sales") {
			v[0] = 1
		}
		if strings.Contains(l, "risk") || strings.Contains(l, "compliance") {
			v[1] = 1
		}
		if strings.Contains(l, "hiring") || strings.Contains(l, "recruit") {
			v[2] = 1
		}
		// Avoid zero vectors so cosine is defined.
		if v[0] == 0 && v[1] == 0 && v[2] == 0 {
			v[0] = 0.01
		}
		out[i] = v
	}
	return out, nil
}

type fixedLLM string

func (f fixedLLM) Model() string { return "fixed" }
func (f fixedLLM) Call(_ context.Context, _ []crewai.Message) (string, error) {
	return string(f), nil
}

func main() {
	store := crewai.NewMemory()

	// Seed three topic-tagged memories via a short Kickoff with AutoEmbed.
	outputs := []string{
		"Q1 revenue grew 12% year over year across all regions.",
		"Compliance risk review flagged two open audit items.",
		"Hiring plan adds three backend engineers in Q2.",
	}
	for i, content := range outputs {
		a := crewai.NewAgent("Analyst", "", "", fixedLLM(content))
		task := crewai.NewTask(fmt.Sprintf("note %d", i), "", a)
		task.Name = fmt.Sprintf("n%d", i)
		crew := crewai.NewCrew([]*crewai.Agent{a}, []*crewai.Task{task})
		crew.MemoryStore = store
		p := crewai.NewMemoryPolicy()
		p.AutoEmbed = true
		crew.MemoryPolicy = p
		crew.Embed = bagOfWordsEmbed
		if _, err := crew.Kickoff(context.Background(), nil); err != nil {
			log.Fatal(err)
		}
	}

	// Semantic recall: query vector points at "risk".
	qVec, err := bagOfWordsEmbed(context.Background(), []string{"compliance risk"})
	if err != nil {
		log.Fatal(err)
	}
	hits, err := store.Query(context.Background(), crewai.MemoryQuery{
		Embedding: qVec[0],
		Limit:     2,
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Semantic top-2 for query ~ risk/compliance:")
	for i, h := range hits {
		fmt.Printf("  %d. %s\n", i+1, h.Content)
	}
	if len(hits) == 0 || !strings.Contains(strings.ToLower(hits[0].Content), "risk") {
		log.Fatal("expected risk entry on top")
	}
	fmt.Println("OK: cosine ranking preferred the risk memory over revenue/hiring.")
}
