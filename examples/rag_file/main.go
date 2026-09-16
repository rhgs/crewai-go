// RAG-as-a-pattern (D-T9): FileStore + bag-of-words embedder + Query
// wrapped as a tool. No vector DB in core.
//
// Run:
//
//	go run ./examples/rag_file
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
		if v[0] == 0 && v[1] == 0 && v[2] == 0 {
			v[0] = 0.01
		}
		out[i] = v
	}
	return out, nil
}

func main() {
	dir := filepath.Join(os.TempDir(), "crewai-rag-file")
	_ = os.RemoveAll(dir)
	store, err := crewai.OpenFileStore(dir)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	defer os.RemoveAll(dir)

	docs := []string{
		"Q1 revenue grew 12% year over year across all regions.",
		"Compliance risk review flagged two open audit items.",
		"Hiring plan adds three backend engineers in Q2.",
	}
	for _, d := range docs {
		vecs, err := bagOfWordsEmbed(context.Background(), []string{d})
		if err != nil {
			log.Fatal(err)
		}
		if _, err := store.Put(context.Background(), crewai.MemoryEntry{
			Task:      "seed",
			Content:   d,
			Embedding: vecs[0],
		}); err != nil {
			log.Fatal(err)
		}
	}

	rag := crewai.NewTool(
		"memory_query",
		"Semantic recall over the document store. Input: a short query.",
		func(ctx context.Context, q string) (string, error) {
			vecs, err := bagOfWordsEmbed(ctx, []string{q})
			if err != nil {
				return "", err
			}
			hits, err := store.Query(ctx, crewai.MemoryQuery{Embedding: vecs[0], Limit: 2})
			if err != nil {
				return "", err
			}
			var b strings.Builder
			for i, h := range hits {
				fmt.Fprintf(&b, "%d. %s\n", i+1, h.Content)
			}
			return strings.TrimSpace(b.String()), nil
		},
	)

	out, err := rag.Call(context.Background(), "compliance risk")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("query: compliance risk")
	fmt.Println(out)
}
