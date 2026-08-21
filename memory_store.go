package crewai

import (
	"context"
	"errors"
	"time"
)

// Memory hard caps. They protect the prompt and the store from unbounded
// payloads (DoS / context-window flooding) regardless of which MemoryStore
// implementation is used.
const (
	// MaxMemoryEntryBytes bounds the Content size of a single MemoryEntry
	// accepted by Put. Oversized entries are rejected (they are never
	// truncated silently). Default: 32 KiB.
	MaxMemoryEntryBytes = 32 << 10

	// MaxMemoryQueryLimit bounds the number of entries returned by a single
	// Query. Queries with a larger (or non-positive / unset) Limit fall back
	// to DefaultMemoryQueryLimit. Default: 32.
	MaxMemoryQueryLimit = 32

	// DefaultMemoryQueryLimit is applied when MemoryQuery.Limit is zero,
	// negative, or above MaxMemoryQueryLimit.
	DefaultMemoryQueryLimit = 8

	// DefaultMemoryMaxChars caps the total Content characters returned by a
	// Query when MemoryQuery.MaxChars is left at zero. A negative MaxChars
	// disables the cap. Default: 4000.
	DefaultMemoryMaxChars = 4000
)

// ErrMemoryEntryTooLarge is returned by Put when the entry's Content exceeds
// MaxMemoryEntryBytes.
var ErrMemoryEntryTooLarge = errors.New("crewai: memory entry exceeds MaxMemoryEntryBytes")

// MemoryScope identifies a logical partition (tenant/crew/project). The empty
// scope is valid and names the default partition.
type MemoryScope string

// MemoryEntry is the persisted unit of long-term memory. It is a superset of
// the short-term MemoryRecord: it adds identity, scope, timestamps, optional
// metadata, and an optional embedding for semantic recall (M4).
type MemoryEntry struct {
	// ID is the stable identifier assigned by the store when empty.
	ID        string      `json:"id"`
	Scope     MemoryScope `json:"scope,omitempty"`
	Agent     string      `json:"agent,omitempty"`
	Task      string      `json:"task,omitempty"`
	Content   string      `json:"content"`
	CreatedAt time.Time   `json:"created_at"`
	// Metadata is optional small key/value (string values only in v1).
	Metadata map[string]string `json:"metadata,omitempty"`
	// Embedding is optional; nil means "not embedded yet". Stored as float32
	// for compactness when present. The core never computes it — the app
	// provides an EmbeddingFunc (M4).
	Embedding []float32 `json:"embedding,omitempty"`
}

// MemoryQuery controls recall.
type MemoryQuery struct {
	Scope MemoryScope
	// Text filters by keyword/substring (the M1 in-memory store path).
	// Embedding (len>0) requests similarity ranking on stores that support
	// it; the M1 in-memory store ignores Embedding entirely (it computes no
	// embeddings) and falls back to Text / latest-N when Text is empty.
	Text      string
	Embedding []float32
	// Limit is the maximum number of hits; values outside
	// (0, MaxMemoryQueryLimit] are clamped to DefaultMemoryQueryLimit.
	Limit int
	// MaxChars caps the total Content characters across returned hits.
	// 0 = DefaultMemoryMaxChars; negative = no cap.
	MaxChars int
}

// MemoryStore is implemented by built-in and application-provided long-term
// memory stores. All methods must be safe for concurrent use.
//
// M1 provides only this contract plus an in-memory adapter on *Memory; the
// Crew is not wired to MemoryStore until M2 (MemoryPolicy + the D-M7 barrier).
type MemoryStore interface {
	Put(ctx context.Context, e MemoryEntry) (MemoryEntry, error)
	Query(ctx context.Context, q MemoryQuery) ([]MemoryEntry, error)
	// Delete removes by id within scope. An unknown id is not an error
	// (idempotent).
	Delete(ctx context.Context, scope MemoryScope, id string) error
	Close() error
}
