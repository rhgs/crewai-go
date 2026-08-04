package crewai

import (
	"strings"
	"sync"
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
