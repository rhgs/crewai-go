package crewai_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/rhgs/crewai-go"
)

func TestOpenFileStoreEmptyRootRejected(t *testing.T) {
	if _, err := crewai.OpenFileStore(""); !errors.Is(err, crewai.ErrFileStoreRoot) {
		t.Fatalf("empty root error = %v, want %v", err, crewai.ErrFileStoreRoot)
	}
	if _, err := crewai.OpenFileStore("   "); !errors.Is(err, crewai.ErrFileStoreRoot) {
		t.Fatalf("blank root error = %v, want %v", err, crewai.ErrFileStoreRoot)
	}
}

func TestFileStorePutQueryRestartDurability(t *testing.T) {
	dir := t.TempDir()
	fs, err := crewai.OpenFileStore(dir)
	if err != nil {
		t.Fatalf("OpenFileStore: %v", err)
	}
	ctx := context.Background()
	e1, err := fs.Put(ctx, crewai.MemoryEntry{Agent: "A", Task: "t1", Content: "first"})
	if err != nil {
		t.Fatalf("Put 1: %v", err)
	}
	e2, err := fs.Put(ctx, crewai.MemoryEntry{Agent: "B", Task: "t2", Content: "second"})
	if err != nil {
		t.Fatalf("Put 2: %v", err)
	}
	if e1.ID == "" || e2.ID == "" || e1.ID == e2.ID {
		t.Fatalf("ids = %q %q", e1.ID, e2.ID)
	}
	if err := fs.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Re-open: entries must survive process restart.
	fs2, err := crewai.OpenFileStore(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer fs2.Close()
	hits, err := fs2.Query(ctx, crewai.MemoryQuery{Limit: 8})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("hits = %d, want 2; %+v", len(hits), hits)
	}
	// Latest-first.
	if hits[0].Content != "second" || hits[1].Content != "first" {
		t.Errorf("order = [%q %q], want [second first]", hits[0].Content, hits[1].Content)
	}
	// IDs stable across restart.
	if hits[0].ID != e2.ID || hits[1].ID != e1.ID {
		t.Errorf("ids after restart = [%q %q], want [%q %q]", hits[0].ID, hits[1].ID, e2.ID, e1.ID)
	}
}

func TestFileStoreScopePartitionAndCrewName(t *testing.T) {
	dir := t.TempDir()
	fs, err := crewai.OpenFileStore(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ctx := context.Background()
	if _, err := fs.Put(ctx, crewai.MemoryEntry{Scope: "crew-a", Content: "alpha"}); err != nil {
		t.Fatalf("Put a: %v", err)
	}
	if _, err := fs.Put(ctx, crewai.MemoryEntry{Scope: "crew-b", Content: "beta"}); err != nil {
		t.Fatalf("Put b: %v", err)
	}
	if _, err := fs.Put(ctx, crewai.MemoryEntry{Content: "default"}); err != nil {
		t.Fatalf("Put default: %v", err)
	}
	a, _ := fs.Query(ctx, crewai.MemoryQuery{Scope: "crew-a", Limit: 8})
	b, _ := fs.Query(ctx, crewai.MemoryQuery{Scope: "crew-b", Limit: 8})
	d, _ := fs.Query(ctx, crewai.MemoryQuery{Limit: 8})
	if len(a) != 1 || a[0].Content != "alpha" {
		t.Errorf("crew-a = %+v", a)
	}
	if len(b) != 1 || b[0].Content != "beta" {
		t.Errorf("crew-b = %+v", b)
	}
	if len(d) != 1 || d[0].Content != "default" {
		t.Errorf("default = %+v", d)
	}
	if err := fs.Close(); err != nil {
		t.Fatal(err)
	}

	// Scope with characters that need escaping must round-trip via meta.Scope.
	fs3, err := crewai.OpenFileStore(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer fs3.Close()
	weird := crewai.MemoryScope("tenant/prod:1")
	if _, err := fs3.Put(ctx, crewai.MemoryEntry{Scope: weird, Content: "weird"}); err != nil {
		t.Fatalf("Put weird: %v", err)
	}
	if err := fs3.Close(); err != nil {
		t.Fatal(err)
	}
	fs4, err := crewai.OpenFileStore(dir)
	if err != nil {
		t.Fatalf("reopen2: %v", err)
	}
	defer fs4.Close()
	got, err := fs4.Query(ctx, crewai.MemoryQuery{Scope: weird, Limit: 8})
	if err != nil {
		t.Fatalf("Query weird: %v", err)
	}
	if len(got) != 1 || got[0].Content != "weird" {
		t.Errorf("escaped scope round-trip = %+v", got)
	}
}

func TestFileStoreDeleteTombstoneSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	fs, err := crewai.OpenFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	e, err := fs.Put(ctx, crewai.MemoryEntry{Content: "keep-me"})
	if err != nil {
		t.Fatal(err)
	}
	e2, err := fs.Put(ctx, crewai.MemoryEntry{Content: "drop-me"})
	if err != nil {
		t.Fatal(err)
	}
	if err := fs.Delete(ctx, "", e2.ID); err != nil {
		t.Fatal(err)
	}
	// Idempotent.
	if err := fs.Delete(ctx, "", e2.ID); err != nil {
		t.Fatal(err)
	}
	if err := fs.Delete(ctx, "", "no-such-id"); err != nil {
		t.Fatal(err)
	}
	hits, _ := fs.Query(ctx, crewai.MemoryQuery{Limit: 8})
	if len(hits) != 1 || hits[0].ID != e.ID {
		t.Fatalf("after delete = %+v", hits)
	}
	if err := fs.Close(); err != nil {
		t.Fatal(err)
	}

	fs2, err := crewai.OpenFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer fs2.Close()
	hits, _ = fs2.Query(ctx, crewai.MemoryQuery{Limit: 8})
	if len(hits) != 1 || hits[0].Content != "keep-me" {
		t.Errorf("after restart delete must stick: %+v", hits)
	}
}

func TestFileStoreCorruptLineSkipped(t *testing.T) {
	dir := t.TempDir()
	// Seed a valid store, then inject a corrupt line into the JSONL.
	fs, err := crewai.OpenFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := fs.Put(ctx, crewai.MemoryEntry{Content: "good-before"}); err != nil {
		t.Fatal(err)
	}
	if err := fs.Close(); err != nil {
		t.Fatal(err)
	}

	entries := filepath.Join(dir, "scopes", "_default", "entries.jsonl")
	f, err := os.OpenFile(entries, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("this is not json\n{\"id\":\"\",\"content\":\"no-id\"}\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	// Append a valid entry after the corrupt lines via a second open… but
	// first reopen to exercise the skip path, then Put another good line.
	fs2, err := crewai.OpenFileStore(dir)
	if err != nil {
		t.Fatalf("reopen after corrupt: %v", err)
	}
	if fs2.CorruptSkipped() < 1 {
		t.Errorf("CorruptSkipped = %d, want ≥ 1", fs2.CorruptSkipped())
	}
	if _, err := fs2.Put(ctx, crewai.MemoryEntry{Content: "good-after"}); err != nil {
		t.Fatal(err)
	}
	hits, err := fs2.Query(ctx, crewai.MemoryQuery{Limit: 8})
	if err != nil {
		t.Fatal(err)
	}
	// good-before + good-after; corrupt lines must not appear.
	if len(hits) != 2 {
		t.Fatalf("hits = %+v, want 2 good entries", hits)
	}
	for _, h := range hits {
		if !strings.HasPrefix(h.Content, "good-") {
			t.Errorf("unexpected content %q", h.Content)
		}
	}
	if err := fs2.Close(); err != nil {
		t.Fatal(err)
	}

	// Counter survives into meta.json.
	fs3, err := crewai.OpenFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer fs3.Close()
	if fs3.CorruptSkipped() < 1 {
		t.Errorf("corrupt counter lost after restart: %d", fs3.CorruptSkipped())
	}
}

func TestFileStoreRejectsOversizedAndClosed(t *testing.T) {
	dir := t.TempDir()
	fs, err := crewai.OpenFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	big := strings.Repeat("x", crewai.MaxMemoryEntryBytes+1)
	if _, err := fs.Put(ctx, crewai.MemoryEntry{Content: big}); !errors.Is(err, crewai.ErrMemoryEntryTooLarge) {
		t.Fatalf("oversized error = %v", err)
	}
	if err := fs.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.Put(ctx, crewai.MemoryEntry{Content: "x"}); !errors.Is(err, crewai.ErrFileStoreClosed) {
		t.Fatalf("Put after Close = %v, want %v", err, crewai.ErrFileStoreClosed)
	}
	if _, err := fs.Query(ctx, crewai.MemoryQuery{}); !errors.Is(err, crewai.ErrFileStoreClosed) {
		t.Fatalf("Query after Close = %v", err)
	}
	if err := fs.Delete(ctx, "", "x"); !errors.Is(err, crewai.ErrFileStoreClosed) {
		t.Fatalf("Delete after Close = %v", err)
	}
	// Close is idempotent.
	if err := fs.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestFileStoreQueryTextAndLimits(t *testing.T) {
	dir := t.TempDir()
	fs, err := crewai.OpenFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	ctx := context.Background()
	for i, c := range []string{"alpha one", "beta two", "alpha three"} {
		if _, err := fs.Put(ctx, crewai.MemoryEntry{Task: "t", Content: c}); err != nil {
			t.Fatalf("Put %d: %v", i, err)
		}
	}
	hits, err := fs.Query(ctx, crewai.MemoryQuery{Text: "alpha", Limit: 8})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("text filter = %+v, want 2", hits)
	}
	// MaxChars small enough for only the newest alpha.
	hits, err = fs.Query(ctx, crewai.MemoryQuery{Text: "alpha", MaxChars: 12, Limit: 8})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Content != "alpha three" {
		t.Errorf("MaxChars filter = %+v", hits)
	}
}

func TestFileStoreConcurrentPutQuery(t *testing.T) {
	dir := t.TempDir()
	fs, err := crewai.OpenFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	ctx := context.Background()
	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				if _, err := fs.Put(ctx, crewai.MemoryEntry{
					Agent:   "w",
					Content: strings.Repeat("c", 8) + string(rune('a'+w)),
				}); err != nil {
					t.Errorf("Put: %v", err)
					return
				}
				if _, err := fs.Query(ctx, crewai.MemoryQuery{Limit: 4}); err != nil {
					t.Errorf("Query: %v", err)
					return
				}
			}
		}(w)
	}
	wg.Wait()
}

func TestFileStoreModes0600(t *testing.T) {
	dir := t.TempDir()
	fs, err := crewai.OpenFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fs.Put(context.Background(), crewai.MemoryEntry{Content: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := fs.Close(); err != nil {
		t.Fatal(err)
	}
	entries := filepath.Join(dir, "scopes", "_default", "entries.jsonl")
	meta := filepath.Join(dir, "scopes", "_default", "meta.json")
	for _, p := range []string{entries, meta} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("%s mode = %o, want 0600", p, perm)
		}
	}
	// Scope dir 0700.
	info, err := os.Stat(filepath.Join(dir, "scopes", "_default"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("scope dir mode = %o, want 0700", perm)
	}
}

func TestFileStoreWithCrewKickoff(t *testing.T) {
	// End-to-end: external FileStore + MemoryPolicy across two Kickoffs.
	dir := t.TempDir()
	store, err := crewai.OpenFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	llm1 := mockLLM("run-one-output")
	a := crewai.NewAgent("A", "", "", llm1)
	task := crewai.NewTask("do work", "", a)
	task.Name = "t1"
	crew := crewai.NewCrew([]*crewai.Agent{a}, []*crewai.Task{task})
	crew.Name = "persist-crew"
	crew.MemoryStore = store
	crew.MemoryPolicy = crewai.NewMemoryPolicy()

	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatalf("kickoff 1: %v", err)
	}

	// Second Kickoff on a fresh Crew sharing the same store must inject
	// the prior run via queryCommitted (no Task.Context).
	var secondPrompt string
	llm2 := &captureLLM{out: "run-two-output", capture: &secondPrompt}
	a2 := crewai.NewAgent("A", "", "", llm2)
	task2 := crewai.NewTask("recall prior", "", a2)
	crew2 := crewai.NewCrew([]*crewai.Agent{a2}, []*crewai.Task{task2})
	crew2.Name = "persist-crew"
	crew2.MemoryStore = store
	crew2.MemoryPolicy = crewai.NewMemoryPolicy()

	if _, err := crew2.Kickoff(context.Background(), nil); err != nil {
		t.Fatalf("kickoff 2: %v", err)
	}
	if !strings.Contains(secondPrompt, "run-one-output") {
		t.Errorf("second Kickoff did not inject FileStore memory; prompt = %q", secondPrompt)
	}
	if !strings.Contains(secondPrompt, "## Memory (recalled)") {
		t.Errorf("expected memory block header; prompt = %q", secondPrompt)
	}
}

func TestFileStoreRootAndMetadataClone(t *testing.T) {
	dir := t.TempDir()
	fs, err := crewai.OpenFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	if fs.Root() == "" {
		t.Fatal("Root empty")
	}
	ctx := context.Background()
	e, err := fs.Put(ctx, crewai.MemoryEntry{
		Content:   "with-meta",
		Metadata:  map[string]string{"k": "v"},
		Embedding: []float32{0.1, 0.2},
	})
	if err != nil {
		t.Fatal(err)
	}
	hits, err := fs.Query(ctx, crewai.MemoryQuery{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Metadata["k"] != "v" || len(hits[0].Embedding) != 2 {
		t.Fatalf("clone = %+v", hits)
	}
	// Mutating the returned maps/slices must not affect the store.
	hits[0].Metadata["k"] = "mutated"
	hits[0].Embedding[0] = 9
	hits2, _ := fs.Query(ctx, crewai.MemoryQuery{Limit: 1})
	if hits2[0].Metadata["k"] != "v" || hits2[0].Embedding[0] != 0.1 {
		t.Errorf("store mutated via Query result: %+v", hits2[0])
	}
	_ = e
}

// mockLLM returns a fixed string for every Call.
type fixedLLM string

func mockLLM(s string) crewai.LLM { return fixedLLM(s) }

func (f fixedLLM) Model() string { return "fixed" }
func (f fixedLLM) Call(context.Context, []crewai.Message) (string, error) {
	return string(f), nil
}

// captureLLM records the concatenated prompt and returns a fixed output.
type captureLLM struct {
	out     string
	capture *string
}

func (c *captureLLM) Model() string { return "capture" }
func (c *captureLLM) Call(_ context.Context, msgs []crewai.Message) (string, error) {
	var b strings.Builder
	for _, m := range msgs {
		b.WriteString(m.Content)
		b.WriteByte('\n')
	}
	if c.capture != nil {
		*c.capture = b.String()
	}
	return c.out, nil
}
