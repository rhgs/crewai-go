package crewai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// Fact is a piece of data produced by a deterministic connector tool, not
// by the LLM. It always carries provenance so a wrong value can never be
// presented as a "fact the model remembered".
type Fact struct {
	// Claim is the human-readable statement of the fact.
	Claim string `json:"claim"`
	// SourceOrg is the organization that provided the data.
	SourceOrg string `json:"source_org"`
	// SourceURL is the URL or identifier of the source.
	SourceURL string `json:"source_url"`
	// CollectedAt is when the connector collected the fact.
	CollectedAt time.Time `json:"collected_at"`
	// PayloadHash is a SHA-256 hash of the raw payload the connector
	// received, for deduplication and tamper detection.
	PayloadHash string `json:"payload_hash"`
}

// FactSource is an optional interface a Tool may implement to declare that
// it produces facts. When a tool implements FactSource, after a successful
// Call the executor collects its Facts() and attaches them to the task
// output. Tools that do not implement FactSource contribute no facts.
type FactSource interface {
	// Facts returns the facts produced by the last successful Call.
	// The executor calls this AFTER Call returns without error.
	Facts() []Fact
}

// FactSourceTool is a Tool that also implements FactSource. After a
// successful Call, factsFn is called to extract facts from the output.
type FactSourceTool struct {
	name        string
	description string
	fn          func(ctx context.Context, input string) (string, error)
	factsFn     func(ctx context.Context, output string) []Fact

	mu       sync.Mutex
	lastFacts []Fact
}

// NewFactSourceTool creates a tool that implements FactSource. After a
// successful Call, factsFn is called with the tool's output to produce
// facts. If factsFn is nil, no facts are produced.
func NewFactSourceTool(
	name, description string,
	fn func(ctx context.Context, input string) (string, error),
	factsFn func(ctx context.Context, output string) []Fact,
) *FactSourceTool {
	return &FactSourceTool{
		name:        name,
		description: description,
		fn:          fn,
		factsFn:     factsFn,
	}
}

// Name implements Tool.
func (t *FactSourceTool) Name() string { return t.name }

// Description implements Tool.
func (t *FactSourceTool) Description() string { return t.description }

// Call implements Tool. After a successful call, it stores the facts
// produced by factsFn for later retrieval via Facts().
func (t *FactSourceTool) Call(ctx context.Context, input string) (string, error) {
	out, err := t.fn(ctx, input)
	t.mu.Lock()
	if err != nil || t.factsFn == nil {
		t.lastFacts = nil
	} else {
		t.lastFacts = t.factsFn(ctx, out)
	}
	t.mu.Unlock()
	return out, err
}

// Facts implements FactSource. Returns the facts from the last successful
// Call, or nil if the last call failed or factsFn is nil.
func (t *FactSourceTool) Facts() []Fact {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.lastFacts
}

// NewFact creates a Fact with the given claim, source org, source URL, and
// raw payload. It computes the PayloadHash (SHA-256) from the raw payload
// and sets CollectedAt to the current time.
func NewFact(claim, sourceOrg, sourceURL string, rawPayload []byte) Fact {
	h := sha256.Sum256(rawPayload)
	return Fact{
		Claim:       claim,
		SourceOrg:   sourceOrg,
		SourceURL:   sourceURL,
		CollectedAt: time.Now(),
		PayloadHash: hex.EncodeToString(h[:]),
	}
}

// AllFactsProvenanced returns nil if every fact in the slice has
// SourceURL and PayloadHash filled. Otherwise it returns an error
// identifying the first fact missing provenance.
func AllFactsProvenanced(facts []Fact) error {
	for i, f := range facts {
		if f.SourceURL == "" {
			return fmt.Errorf("fact %d (claim %q): missing source_url", i, f.Claim)
		}
		if f.PayloadHash == "" {
			return fmt.Errorf("fact %d (claim %q): missing payload_hash", i, f.Claim)
		}
	}
	return nil
}

// dedupFacts appends new facts to existing, skipping duplicates by
// PayloadHash (keeping the first occurrence). Facts with an empty
// PayloadHash are skipped.
func dedupFacts(existing []Fact, newFacts []Fact) []Fact {
	seen := make(map[string]bool, len(existing))
	for _, f := range existing {
		seen[f.PayloadHash] = true
	}
	for _, f := range newFacts {
		if f.PayloadHash == "" || seen[f.PayloadHash] {
			continue
		}
		seen[f.PayloadHash] = true
		existing = append(existing, f)
	}
	return existing
}