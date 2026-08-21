package crewai

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync"
	"time"
)

// Memory stores records produced during a crew's execution so that later
// tasks can recover context from earlier ones.
//
// This is a simple, concurrency-safe, in-memory implementation. For advanced
// scenarios (semantic search/embeddings), provide your own implementation
// satisfying the same minimal interface used by the Crew.
type Memory struct {
	mu      sync.RWMutex
	records []MemoryRecord
	// ids is parallel to records. It stores the stable ID each record
	// received the first time it crossed the MemoryStore bridge so the same
	// record keeps the same ID across repeated reads and Query/Put. Save()
	// appends an empty id; it is filled lazily on first read of that record.
	ids []string
}

// MemoryRecord is an annotation stored in memory.
type MemoryRecord struct {
	// Agent is the role of the agent that produced the record.
	Agent string
	// Task is the short name/description of the related task.
	Task string
	// Content is the memorized content (usually the task output).
	Content string
}

// NewMemory creates an empty memory.
func NewMemory() *Memory {
	return &Memory{}
}

// Save adds a record to the memory.
func (m *Memory) Save(rec MemoryRecord) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = append(m.records, rec)
	m.ids = append(m.ids, "")
}

// Records returns a copy of all stored records.
func (m *Memory) Records() []MemoryRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]MemoryRecord, len(m.records))
	copy(out, m.records)
	return out
}

// Search performs a simple text search (substring, case-insensitive) and
// returns the records that contain the query. An empty query returns all
// records.
func (m *Memory) Search(query string) []MemoryRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if strings.TrimSpace(query) == "" {
		out := make([]MemoryRecord, len(m.records))
		copy(out, m.records)
		return out
	}
	q := strings.ToLower(query)
	var out []MemoryRecord
	for _, r := range m.records {
		if strings.Contains(strings.ToLower(r.Content), q) ||
			strings.Contains(strings.ToLower(r.Task), q) {
			out = append(out, r)
		}
	}
	return out
}

// String returns a readable representation of the entire memory, useful for
// injecting as context into prompts.
func (m *Memory) String() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var b strings.Builder
	for _, r := range m.records {
		b.WriteString("- [")
		b.WriteString(r.Agent)
		b.WriteString("] ")
		b.WriteString(r.Content)
		b.WriteString("\n")
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// MemoryStore bridge (M1)
//
// *Memory also satisfies the long-term MemoryStore contract so applications
// can depend on the interface while the Crew's Crew.Memory bool keeps working
// unchanged. Put maps to Save with an assigned ID and timestamps; Query maps
// to Search over the same *Memory, applying Limit/MaxChars and scope; Delete
// removes by the stable ID of an entry; Close is a no-op.
// ---------------------------------------------------------------------------

// compile-time guarantee that the bridge is complete.
var _ MemoryStore = (*Memory)(nil)

// AsStore returns m itself as a MemoryStore. The store shares m's data.
func (m *Memory) AsStore() MemoryStore { return m }

// Put stores an entry in the default short-term memory. The returned entry
// embeds a copy of e.Content (never aliasing beyond the 32 KiB cap), a
// CreatedAt set to now when zero, and a stable store-assigned ID.
func (m *Memory) Put(_ context.Context, e MemoryEntry) (MemoryEntry, error) {
	if len(e.Content) > MaxMemoryEntryBytes {
		return MemoryEntry{}, ErrMemoryEntryTooLarge
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	id := e.ID
	if id == "" {
		id = m.generateIDLocked()
	}
	now := time.Now()
	if e.CreatedAt.IsZero() {
		e.CreatedAt = now
	}
	e.ID = id
	e.CreatedAt = e.CreatedAt.UTC()

	m.records = append(m.records, MemoryRecord{
		Agent:   e.Agent,
		Task:    e.Task,
		Content: e.Content,
	})
	m.ids = append(m.ids, id)
	return e, nil
}

// Query returns the entries matching q over the same in-memory data used by
// the rest of the package. When q.Text is empty it returns the latest
// entries in stable reverse order (most recent first), which is what M2's
// "latest N" injection needs. Scope partitions results; an entry with an
// empty scope belongs to the default partition.
func (m *Memory) Query(_ context.Context, q MemoryQuery) ([]MemoryEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	limit := q.Limit
	if limit <= 0 || limit > MaxMemoryQueryLimit {
		limit = DefaultMemoryQueryLimit
	}
	maxChars := q.MaxChars
	if maxChars == 0 {
		maxChars = DefaultMemoryMaxChars
	}

	// Build the matching entry list in insertion order first; the latest-N
	// step below reverses it. We materialize IDs lazily so records created by
	// Save() (not only by Put) keep a stable ID.
	indices := make([]int, 0, len(m.records))
	text := strings.ToLower(strings.TrimSpace(q.Text))
	for i, r := range m.records {
		if text != "" &&
			!strings.Contains(strings.ToLower(r.Content), text) &&
			!strings.Contains(strings.ToLower(r.Task), text) {
			continue
		}
		indices = append(indices, i)
	}

	var out []MemoryEntry
	totalChars := 0
	// Latest N: iterate matching indices from newest to oldest.
	for n := len(indices) - 1; n >= 0 && len(out) < limit; n-- {
		i := indices[n]
		r := m.records[i]
		if scopeOf(r) != q.Scope {
			continue
		}
		if maxChars >= 0 && totalChars+len(r.Content) > maxChars {
			// The next newest entries only get larger-or-equal; stop.
			break
		}
		out = append(out, MemoryEntry{
			ID:        m.idForLocked(i),
			Scope:     scopeOf(r),
			Agent:     r.Agent,
			Task:      r.Task,
			Content:   r.Content,
			CreatedAt: time.Time{}, // Creation time is informational (G6);
			// the in-memory store keeps insertion order, not wall-clock.
		})
		totalChars += len(r.Content)
	}
	return out, nil
}

// Delete removes the entry with the given ID within scope. An unknown ID is
// not an error. Scope must match exactly (empty = default partition).
func (m *Memory) Delete(_ context.Context, scope MemoryScope, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range m.records {
		if m.idForLocked(i) == id && scopeOf(r) == scope {
			m.records = append(m.records[:i], m.records[i+1:]...)
			m.ids = append(m.ids[:i], m.ids[i+1:]...)
			return nil
		}
	}
	return nil
}

// Close is a no-op: the in-memory store owns no external resources.
func (m *Memory) Close() error { return nil }

// idForLocked returns the stable ID of record i, generating and storing one
// on first demand. Callers must hold m.mu.
func (m *Memory) idForLocked(i int) string {
	if m.ids[i] == "" {
		m.ids[i] = newMemoryID()
	}
	return m.ids[i]
}

// generateIDLocked returns a fresh ID that is not already assigned. Callers
// must hold m.mu.
func (m *Memory) generateIDLocked() string {
	for {
		candidate := newMemoryID()
		used := false
		for _, id := range m.ids {
			if id == candidate {
				used = true
				break
			}
		}
		if !used {
			return candidate
		}
	}
}

// newMemoryID returns a random 16-byte hex ID (32 chars) using crypto/rand.
// This is not a UUID; it is a stable opaque identifier for one store
// partition, matching the plan's "uuid-ish hex from crypto/rand" note.
func newMemoryID() string {
	var b [16]byte
	// crypto/rand.Read never returns an error on supported platforms.
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// scopeOf returns the scope a short-term record belongs to. Records created
// via Save() carry no scope metadata (the short-term Memory type has no
// scope field), so they all live in the default (empty) partition.
func scopeOf(MemoryRecord) MemoryScope { return "" }
