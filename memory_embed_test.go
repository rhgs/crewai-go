package crewai_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/rhgs/crewai-go"
	"github.com/rhgs/crewai-go/llm/mock"
)

func TestCosineSimilarityBasic(t *testing.T) {
	// Exported behavior is exercised via Query ranking; unit-check via
	// known vectors that Query will order.
	// a·b high, a·c low.
	store := crewai.NewMemory()
	ctx := context.Background()
	_, _ = store.Put(ctx, crewai.MemoryEntry{
		Content: "cats", Embedding: []float32{1, 0, 0},
	})
	_, _ = store.Put(ctx, crewai.MemoryEntry{
		Content: "dogs", Embedding: []float32{0.9, 0.1, 0},
	})
	_, _ = store.Put(ctx, crewai.MemoryEntry{
		Content: "quantum", Embedding: []float32{0, 0, 1},
	})

	hits, err := store.Query(ctx, crewai.MemoryQuery{
		Embedding: []float32{1, 0, 0},
		Limit:     3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) < 2 {
		t.Fatalf("hits = %+v", hits)
	}
	// cats (exact) should rank above dogs; quantum (orthogonal) dropped
	// when positive scores exist.
	if hits[0].Content != "cats" {
		t.Errorf("top hit = %q, want cats", hits[0].Content)
	}
	if hits[1].Content != "dogs" {
		t.Errorf("second = %q, want dogs", hits[1].Content)
	}
	for _, h := range hits {
		if h.Content == "quantum" {
			t.Errorf("orthogonal entry should be trimmed when positives exist: %+v", hits)
		}
	}
}

func TestCosineDimMismatchScoresZero(t *testing.T) {
	store := crewai.NewMemory()
	ctx := context.Background()
	_, _ = store.Put(ctx, crewai.MemoryEntry{
		Content: "short-vec", Embedding: []float32{1, 0},
	})
	_, _ = store.Put(ctx, crewai.MemoryEntry{
		Content: "match", Embedding: []float32{1, 0, 0},
	})
	hits, err := store.Query(ctx, crewai.MemoryQuery{
		Embedding: []float32{1, 0, 0},
		Limit:     8,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Content != "match" {
		t.Errorf("dim mismatch must not rank; hits = %+v", hits)
	}
}

func TestSemanticQueryFallsBackWhenNoEmbeddings(t *testing.T) {
	// All entries lack embeddings → rankByEmbedding treats all scores as 0
	// and returns latest-N of the candidate set (no silent empty).
	store := crewai.NewMemory()
	ctx := context.Background()
	_, _ = store.Put(ctx, crewai.MemoryEntry{Content: "one"})
	_, _ = store.Put(ctx, crewai.MemoryEntry{Content: "two"})
	hits, err := store.Query(ctx, crewai.MemoryQuery{
		Embedding: []float32{1, 0},
		Limit:     8,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("no-embedding fallback = %+v, want 2 latest", hits)
	}
	if hits[0].Content != "two" || hits[1].Content != "one" {
		t.Errorf("latest-N order = [%q %q]", hits[0].Content, hits[1].Content)
	}
}

func TestFileStoreSemanticQuery(t *testing.T) {
	dir := t.TempDir()
	fs, err := crewai.OpenFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, _ = fs.Put(ctx, crewai.MemoryEntry{
		Content: "alpha", Embedding: []float32{1, 0},
	})
	_, _ = fs.Put(ctx, crewai.MemoryEntry{
		Content: "beta", Embedding: []float32{0, 1},
	})
	if err := fs.Close(); err != nil {
		t.Fatal(err)
	}
	// Survive restart with embeddings on disk.
	fs2, err := crewai.OpenFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer fs2.Close()
	hits, err := fs2.Query(ctx, crewai.MemoryQuery{
		Embedding: []float32{0, 1},
		Limit:     2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) < 1 || hits[0].Content != "beta" {
		t.Errorf("FileStore semantic = %+v, want beta first", hits)
	}
}

func TestAutoEmbedAtBarrierSerial(t *testing.T) {
	// G8: AutoEmbed runs serially at the barrier — peak concurrency of the
	// embedder must be 1 even when two Async tasks finish in parallel.
	var inside atomic.Int32
	var peak atomic.Int32
	var calls atomic.Int32
	embed := func(_ context.Context, texts []string) ([][]float32, error) {
		cur := inside.Add(1)
		for {
			p := peak.Load()
			if cur <= p || peak.CompareAndSwap(p, cur) {
				break
			}
		}
		// No sleep needed: if embed were called from workers concurrently,
		// peak would exceed 1 under -race / scheduling pressure. Serial
		// barrier makes peak == 1 by construction.
		calls.Add(1)
		inside.Add(-1)
		out := make([][]float32, len(texts))
		for i := range texts {
			out[i] = []float32{1, 0, 0}
		}
		return out, nil
	}

	llm := mock.New("slow-out", "fast-out")
	// Use a handler that lets both Async tasks overlap before barrier.
	llm = &mock.LLM{Handler: func(_ context.Context, msgs []crewai.Message) (string, error) {
		var all string
		for _, m := range msgs {
			all += m.Content
		}
		if strings.Contains(all, "SLOW") {
			return "slow-out", nil
		}
		return "fast-out", nil
	}}
	a := crewai.NewAgent("A", "", "", llm)
	t1 := crewai.NewTask("SLOW", "", a).WithAsync()
	t1.Name = "s"
	t2 := crewai.NewTask("FAST", "", a).WithAsync()
	t2.Name = "f"

	crew := crewai.NewCrew([]*crewai.Agent{a}, []*crewai.Task{t1, t2})
	crew.Memory = true
	p := crewai.NewMemoryPolicy()
	p.AutoEmbed = true
	crew.MemoryPolicy = p
	crew.Embed = embed

	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatalf("Kickoff: %v", err)
	}
	if calls.Load() != 2 {
		t.Errorf("embed calls = %d, want 2", calls.Load())
	}
	if peak.Load() != 1 {
		t.Errorf("embed peak concurrency = %d, want 1 (G8 serial barrier)", peak.Load())
	}
	// Embeddings must be stored.
	recs := crew.MemorySnapshot()
	hits, err := recs.Query(context.Background(), crewai.MemoryQuery{
		Embedding: []float32{1, 0, 0},
		Limit:     8,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("stored embedded entries = %d, want 2", len(hits))
	}
	for _, h := range hits {
		if len(h.Embedding) != 3 {
			t.Errorf("entry %q missing embedding: %+v", h.Content, h)
		}
	}
}

func TestAutoEmbedSoftFailDoesNotAbort(t *testing.T) {
	embed := func(context.Context, []string) ([][]float32, error) {
		return nil, errors.New("embedder down")
	}
	llm := mock.New("ok")
	a := crewai.NewAgent("A", "", "", llm)
	task := crewai.NewTask("t", "", a)
	crew := crewai.NewCrew([]*crewai.Agent{a}, []*crewai.Task{task})
	crew.Memory = true
	p := crewai.NewMemoryPolicy()
	p.AutoEmbed = true
	crew.MemoryPolicy = p
	crew.Embed = embed

	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("AutoEmbed failure must not abort Kickoff: %v", err)
	}
	if out.Final != "ok" {
		t.Errorf("Final = %q", out.Final)
	}
	// Entry still saved (without embedding).
	if len(crew.MemorySnapshot().Records()) != 1 {
		t.Errorf("AutoSave should still land; records = %+v", crew.MemorySnapshot().Records())
	}
	// Warning captured on the task.
	found := false
	for _, w := range out.Warnings {
		if strings.Contains(w, "AutoEmbed") {
			found = true
		}
	}
	if !found {
		// Also check task-level via TasksOutput.
		for _, to := range out.TasksOutput {
			for _, w := range to.Warnings {
				if strings.Contains(w, "AutoEmbed") {
					found = true
				}
			}
		}
	}
	if !found {
		t.Errorf("expected AutoEmbed warning; Warnings = %v TasksOutput = %+v",
			out.Warnings, out.TasksOutput)
	}
}

func TestAutoEmbedSkippedWhenFlagFalse(t *testing.T) {
	var calls atomic.Int32
	embed := func(context.Context, []string) ([][]float32, error) {
		calls.Add(1)
		return [][]float32{{1}}, nil
	}
	llm := mock.New("ok")
	a := crewai.NewAgent("A", "", "", llm)
	task := crewai.NewTask("t", "", a)
	crew := crewai.NewCrew([]*crewai.Agent{a}, []*crewai.Task{task})
	crew.Memory = true
	// Default AutoEmbed=false.
	crew.MemoryPolicy = crewai.NewMemoryPolicy()
	crew.Embed = embed
	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Errorf("Embed called %d times with AutoEmbed=false", calls.Load())
	}
}
