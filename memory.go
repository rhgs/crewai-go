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
// satisfying MemoryStore, or use the built-in FileStore (M3).
type Memory struct {
	mu      sync.RWMutex
	records []memorySlot
}

// memorySlot is the internal unit: a MemoryRecord plus the store-bridge
// fields (ID, Scope, CreatedAt, Embedding) needed to implement MemoryStore
// without breaking the public MemoryRecord shape.
type memorySlot struct {
	rec       MemoryRecord
	id        string
	scope     MemoryScope
	createdAt time.Time
	embedding []float32
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

// Save adds a record to the memory (default / empty scope).
func (m *Memory) Save(rec MemoryRecord) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = append(m.records, memorySlot{rec: rec})
}

// Records returns a copy of all stored records.
func (m *Memory) Records() []MemoryRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]MemoryRecord, len(m.records))
	for i, s := range m.records {
		out[i] = s.rec
	}
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
		for i, s := range m.records {
			out[i] = s.rec
		}
		return out
	}
	q := strings.ToLower(query)
	var out []MemoryRecord
	for _, s := range m.records {
		r := s.rec
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
	for _, s := range m.records {
		r := s.rec
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

// Put stores an entry. The returned entry embeds a store-assigned ID (when
// empty), CreatedAt set to now when zero, and the caller's Scope.
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
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	} else {
		e.CreatedAt = e.CreatedAt.UTC()
	}
	e.ID = id

	var emb []float32
	if len(e.Embedding) > 0 {
		emb = make([]float32, len(e.Embedding))
		copy(emb, e.Embedding)
		e.Embedding = emb // return the stored copy, not the caller's buffer
	}
	m.records = append(m.records, memorySlot{
		rec: MemoryRecord{
			Agent:   e.Agent,
			Task:    e.Task,
			Content: e.Content,
		},
		id:        id,
		scope:     e.Scope,
		createdAt: e.CreatedAt,
		embedding: emb,
	})
	return e, nil
}

// Query returns the entries matching q over the same in-memory data used by
// the rest of the package. Scope partitions results.
//
// When q.Embedding is non-empty, results are ranked by cosine similarity
// (M4) and substring Text is ignored for filtering (semantic path). When
// q.Embedding is empty, behavior matches M1/M2: empty Text ⇒ latest N;
// non-empty Text ⇒ case-insensitive substring on Content/Task.
func (m *Memory) Query(_ context.Context, q MemoryQuery) ([]MemoryEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	limit, maxChars := clampQueryBounds(q)

	// Materialize scope-matching entries in insertion order.
	semantic := len(q.Embedding) > 0
	text := strings.ToLower(strings.TrimSpace(q.Text))
	var candidates []MemoryEntry
	for i, s := range m.records {
		if s.scope != q.Scope {
			continue
		}
		r := s.rec
		if !semantic && text != "" &&
			!strings.Contains(strings.ToLower(r.Content), text) &&
			!strings.Contains(strings.ToLower(r.Task), text) {
			continue
		}
		var emb []float32
		if len(s.embedding) > 0 {
			emb = make([]float32, len(s.embedding))
			copy(emb, s.embedding)
		}
		candidates = append(candidates, MemoryEntry{
			ID:        m.idForLocked(i),
			Scope:     s.scope,
			Agent:     r.Agent,
			Task:      r.Task,
			Content:   r.Content,
			CreatedAt: s.createdAt,
			Embedding: emb,
		})
	}
	if semantic {
		return rankByEmbedding(q.Embedding, candidates, limit, maxChars), nil
	}
	return takeLatestN(candidates, limit, maxChars), nil
}

// Delete removes the entry with the given ID within scope. An unknown ID is
// not an error. Scope must match exactly (empty = default partition).
func (m *Memory) Delete(_ context.Context, scope MemoryScope, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, s := range m.records {
		if m.idForLocked(i) == id && s.scope == scope {
			m.records = append(m.records[:i], m.records[i+1:]...)
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
	if m.records[i].id == "" {
		m.records[i].id = newMemoryID()
	}
	return m.records[i].id
}

// generateIDLocked returns a fresh ID that is not already assigned. Callers
// must hold m.mu.
func (m *Memory) generateIDLocked() string {
	for {
		candidate := newMemoryID()
		used := false
		for _, s := range m.records {
			if s.id == candidate {
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
func newMemoryID() string {
	var b [16]byte
	// crypto/rand.Read never returns an error on supported platforms.
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
