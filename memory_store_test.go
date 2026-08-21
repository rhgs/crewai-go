package crewai_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rhgs/crewai-go"
)

// seedMemoryWithIDs pre-populates m from the bridge and returns the IDs that
// Put assigned, in insertion order. Content of record i is "content-i" and
// Task is "unique-task-i" so substring queries can stay unique per record.
func seedMemoryWithIDs(t *testing.T, m *crewai.Memory, n int) []string {
	t.Helper()
	ids := make([]string, n)
	for i := 0; i < n; i++ {
		e, err := m.Put(context.Background(), crewai.MemoryEntry{
			Agent:   "A",
			Task:    fmt.Sprintf("unique-task-%d", i),
			Content: fmt.Sprintf("content-%d", i),
		})
		if err != nil {
			t.Fatalf("Put %d: %v", i, err)
		}
		ids[i] = e.ID
	}
	return ids
}

// TestMemoryStoreInterfaceCompile also documents the compile-time guarantee.
func TestMemoryStoreInterfaceCompile(t *testing.T) {
	var _ crewai.MemoryStore = crewai.NewMemory()
}

func TestMemoryPutAssignsIDAndTimestamps(t *testing.T) {
	m := crewai.NewMemory()
	before := time.Now().UTC()
	e, err := m.Put(context.Background(), crewai.MemoryEntry{
		Agent: "Analyst", Task: "research", Content: "Go is fast",
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if e.ID == "" {
		t.Error("Put should assign an ID")
	}
	if e.CreatedAt.IsZero() || e.CreatedAt.Before(before) {
		t.Errorf("Put should fill CreatedAt; got %v (before=%v)", e.CreatedAt, before)
	}
}

func TestMemoryPutRejectsOversized(t *testing.T) {
	m := crewai.NewMemory()
	big := strings.Repeat("x", crewai.MaxMemoryEntryBytes+1)
	if _, err := m.Put(context.Background(), crewai.MemoryEntry{Content: big}); !errors.Is(err, crewai.ErrMemoryEntryTooLarge) {
		t.Fatalf("Put oversized error = %v, want %v", err, crewai.ErrMemoryEntryTooLarge)
	}
	ok := strings.Repeat("x", crewai.MaxMemoryEntryBytes)
	if _, err := m.Put(context.Background(), crewai.MemoryEntry{Content: ok}); err != nil {
		t.Fatalf("Put at exact cap should succeed: %v", err)
	}
}

func TestMemoryQueryLatestNStableOrder(t *testing.T) {
	m := crewai.NewMemory()
	ids := seedMemoryWithIDs(t, m, 5)

	got, err := m.Query(context.Background(), crewai.MemoryQuery{Limit: 2})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 hits, got %d", len(got))
	}
	// Latest two in stable reverse order: record 4, record 3.
	if got[0].ID != ids[4] || got[1].ID != ids[3] {
		t.Errorf("latest-2 order = [%q %q], want [%q %q]", got[0].ID, got[1].ID, ids[4], ids[3])
	}

	// Without an explicit limit the default applies (5 within DefaultLimit 8).
	got, err = m.Query(context.Background(), crewai.MemoryQuery{})
	if err != nil {
		t.Fatalf("Query default: %v", err)
	}
	if len(got) != 5 {
		t.Errorf("default query with 5 records = %d hits, want 5", len(got))
	}
}

func TestMemoryQueryTextMatchesContentAndTask(t *testing.T) {
	m := crewai.NewMemory()
	seedMemoryWithIDs(t, m, 3)

	byContent, err := m.Query(context.Background(), crewai.MemoryQuery{Text: "content-1"})
	if err != nil {
		t.Fatalf("Query by content: %v", err)
	}
	if len(byContent) != 1 || byContent[0].Task != "unique-task-1" {
		t.Errorf("query by content = %+v", byContent)
	}

	byTask, err := m.Query(context.Background(), crewai.MemoryQuery{Text: "unique-task-2"})
	if err != nil {
		t.Fatalf("Query by task: %v", err)
	}
	if len(byTask) != 1 || byTask[0].Content != "content-2" {
		t.Errorf("query by task = %+v", byTask)
	}
}

func TestMemoryQueryLimitClamped(t *testing.T) {
	m := crewai.NewMemory()
	seedMemoryWithIDs(t, m, 40)

	got, err := m.Query(context.Background(), crewai.MemoryQuery{Limit: 100})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != crewai.DefaultMemoryQueryLimit {
		t.Errorf("limit above cap = %d hits, want %d", len(got), crewai.DefaultMemoryQueryLimit)
	}

	got, err = m.Query(context.Background(), crewai.MemoryQuery{Limit: -3})
	if err != nil {
		t.Fatalf("Query negative limit: %v", err)
	}
	if len(got) != crewai.DefaultMemoryQueryLimit {
		t.Errorf("negative limit = %d hits, want %d", len(got), crewai.DefaultMemoryQueryLimit)
	}
}

func TestMemoryQueryMaxCharsEnforced(t *testing.T) {
	m := crewai.NewMemory()
	// Each record is len("content-N") == 9 chars; newest first.
	seedMemoryWithIDs(t, m, 10)

	got, err := m.Query(context.Background(), crewai.MemoryQuery{MaxChars: 25, Limit: 32})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("maxChars 25 = %d hits, want 2 (18 chars)", len(got))
	}

	// Negative MaxChars disables the cap; only Limit applies.
	got, err = m.Query(context.Background(), crewai.MemoryQuery{MaxChars: -1, Limit: 5})
	if err != nil {
		t.Fatalf("Query uncapped: %v", err)
	}
	if len(got) != 5 {
		t.Errorf("uncapped query = %d hits, want 5", len(got))
	}
}

func TestMemoryQueryScopePartitionsResults(t *testing.T) {
	m := crewai.NewMemory()
	// Save() records always land in the default scope.
	m.Save(crewai.MemoryRecord{Agent: "A", Content: "short-term record"})
	ids := seedMemoryWithIDs(t, m, 2)

	if _, err := crewai.NewMemory().Query(context.Background(), crewai.MemoryQuery{}); err != nil {
		t.Fatalf("empty memory Query: %v", err)
	}

	def, err := m.Query(context.Background(), crewai.MemoryQuery{Limit: 32})
	if err != nil {
		t.Fatalf("default scope query: %v", err)
	}
	if len(def) != 3 {
		t.Errorf("default scope sees %d entries, want 3 (Save + 2 Put)", len(def))
	}

	other, err := m.Query(context.Background(), crewai.MemoryQuery{Scope: "tenant-a"})
	if err != nil {
		t.Fatalf("other scope query: %v", err)
	}
	if len(other) != 0 {
		t.Errorf("non-empty scope should see 0 entries, got %d", len(other))
	}

	// Put records are visible in the default scope and keep their IDs.
	seen := map[string]bool{}
	for _, e := range def {
		seen[e.ID] = true
	}
	for _, id := range ids {
		if !seen[id] {
			t.Errorf("default scope query missing Put id %q", id)
		}
	}
}

func TestMemoryDeleteByIDWithinScope(t *testing.T) {
	m := crewai.NewMemory()
	ids := seedMemoryWithIDs(t, m, 3)

	if err := m.Delete(context.Background(), "", "nonexistent-id"); err != nil {
		t.Errorf("Delete unknown id should be nil, got %v", err)
	}
	if err := m.Delete(context.Background(), "tenant-a", ids[1]); err != nil {
		t.Errorf("Delete in another scope should be nil (idempotent), got %v", err)
	}
	if len(m.Records()) != 3 {
		t.Fatalf("delete other scope must not remove, got %d records", len(m.Records()))
	}

	if err := m.Delete(context.Background(), "", ids[1]); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(m.Records()) != 2 {
		t.Errorf("after delete expected 2 records, got %d", len(m.Records()))
	}
	got, _ := m.Query(context.Background(), crewai.MemoryQuery{Text: "content-1"})
	if len(got) != 0 {
		t.Errorf("deleted record still queryable: %+v", got)
	}
	// Deleting again is a no-op.
	if err := m.Delete(context.Background(), "", ids[1]); err != nil {
		t.Errorf("second Delete should be nil, got %v", err)
	}
}

func TestMemoryStoreBridgeSeesSavesWithStableID(t *testing.T) {
	m := crewai.NewMemory()
	m.Save(crewai.MemoryRecord{Agent: "A", Task: "t", Content: "save path"})

	first, err := m.Query(context.Background(), crewai.MemoryQuery{})
	if err != nil {
		t.Fatalf("first Query: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(first))
	}
	id := first[0].ID
	if id == "" {
		t.Error("Save record crossing the bridge must have an ID")
	}

	second, _ := m.Query(context.Background(), crewai.MemoryQuery{})
	if second[0].ID != id {
		t.Errorf("ID unstable across queries: %q then %q", id, second[0].ID)
	}
	if err := m.Delete(context.Background(), "", id); err != nil {
		t.Fatalf("Delete by Save-derived id: %v", err)
	}
	if len(m.Records()) != 0 {
		t.Errorf("expected 0 records after delete, got %d", len(m.Records()))
	}
}

func TestMemoryPutDoesNotAliasCallerBuffer(t *testing.T) {
	m := crewai.NewMemory()
	content := "buffer content"
	if _, err := m.Put(context.Background(), crewai.MemoryEntry{Content: content}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	// Mutating the caller's view must not change the stored record.
	content = "mutated"
	if got := m.Records()[0].Content; got != "buffer content" {
		t.Errorf("aliasing detected: stored %q", got)
	}
}

func TestMemoryAsStoreSharesData(t *testing.T) {
	m := crewai.NewMemory()
	m.Save(crewai.MemoryRecord{Agent: "A", Content: "via Save"})
	var store crewai.MemoryStore = m.AsStore()
	entries, err := store.Query(context.Background(), crewai.MemoryQuery{Text: "via Save"})
	if err != nil {
		t.Fatalf("Query via store: %v", err)
	}
	if len(entries) != 1 || entries[0].Content != "via Save" {
		t.Errorf("AsStore should expose the same data, got %+v", entries)
	}
}

func TestMemoryCloseNoop(t *testing.T) {
	m := crewai.NewMemory()
	if err := m.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestMemoryStoreConcurrentPutQueryDelete(t *testing.T) {
	m := crewai.NewMemory()
	var producers sync.WaitGroup
	var consumer sync.WaitGroup
	ids := make(chan string, 64)

	// Producers: parallel Put + Query bursts.
	for w := 0; w < 4; w++ {
		producers.Add(1)
		go func(w int) {
			defer producers.Done()
			for i := 0; i < 25; i++ {
				e, err := m.Put(context.Background(), crewai.MemoryEntry{
					Agent: fmt.Sprintf("w%d", w), Content: fmt.Sprintf("w%d-%d", w, i),
				})
				if err != nil {
					t.Errorf("Put: %v", err)
					return
				}
				ids <- e.ID
				if _, err := m.Query(context.Background(), crewai.MemoryQuery{}); err != nil {
					t.Errorf("Query: %v", err)
				}
			}
		}(w)
	}

	// Consumer: drains IDs via Delete until producers close the channel.
	consumer.Add(1)
	go func() {
		defer consumer.Done()
		for id := range ids {
			if err := m.Delete(context.Background(), "", id); err != nil {
				t.Errorf("Delete: %v", err)
			}
		}
	}()

	producers.Wait()
	close(ids)
	consumer.Wait()
}
