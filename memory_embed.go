package crewai

import (
	"context"
	"math"
	"sort"
)

// EmbeddingFunc embeds one or more texts into float32 vectors. Provided by
// the application (HTTP call to OpenAI/Ollama/etc.) — the core never bundles
// an embedding model. Must be safe for the caller's concurrency model; the
// Crew invokes it serially at the memory commit barrier when AutoEmbed is
// set (G8), never from parallel workers.
//
// The returned slice must have one vector per input text, in the same order.
// A nil or short result is treated as a soft failure (entry saved without
// embedding; warn+capture on the AutoEmbed path).
type EmbeddingFunc func(ctx context.Context, texts []string) ([][]float32, error)

// cosineSimilarity returns the cosine similarity of a and b in [-1, 1].
// Dim mismatch, empty vectors, or zero-norm inputs yield 0 (they are not
// ranked as matches). Stdlib only — no external vector package.
func cosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		fa := float64(a[i])
		fb := float64(b[i])
		dot += fa * fb
		na += fa * fa
		nb += fb * fb
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// clampQueryBounds applies Default/Max limits to a MemoryQuery's Limit and
// MaxChars. Shared by *Memory and FileStore.
func clampQueryBounds(q MemoryQuery) (limit, maxChars int) {
	limit = q.Limit
	if limit <= 0 || limit > MaxMemoryQueryLimit {
		limit = DefaultMemoryQueryLimit
	}
	maxChars = q.MaxChars
	if maxChars == 0 {
		maxChars = DefaultMemoryMaxChars
	}
	return limit, maxChars
}

// rankByEmbedding sorts candidates by cosine similarity to query (desc),
// breaking ties by higher original index (more recent first, matching
// latest-N stability). Entries with nil/mismatched embeddings score 0 and
// sort last among equals. Applies Limit and MaxChars to the ranked list.
// candidates must already be scope-filtered; text filtering is the caller's
// choice before calling this (semantic path typically skips substring).
func rankByEmbedding(query []float32, candidates []MemoryEntry, limit, maxChars int) []MemoryEntry {
	if len(candidates) == 0 || len(query) == 0 {
		return nil
	}
	type scored struct {
		e     MemoryEntry
		score float64
		idx   int // original position for stable tie-break
	}
	items := make([]scored, len(candidates))
	for i, e := range candidates {
		items[i] = scored{
			e:     e,
			score: cosineSimilarity(query, e.Embedding),
			idx:   i,
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].score != items[j].score {
			return items[i].score > items[j].score
		}
		// Higher original index = more recently inserted among equals.
		return items[i].idx > items[j].idx
	})

	var out []MemoryEntry
	total := 0
	for _, it := range items {
		if len(out) >= limit {
			break
		}
		// Drop zero-score entries when at least one positive match exists;
		// if everything is zero, still return latest-N of the candidate set
		// so Query never silently returns nothing when embeddings are absent.
		if maxChars >= 0 && total+len(it.e.Content) > maxChars {
			break
		}
		out = append(out, it.e)
		total += len(it.e.Content)
	}
	// If we have any positive-score hit, trim trailing zeros so "no embedding
	// on entry" does not pad the result with irrelevant rows.
	hasPos := false
	for _, it := range items {
		if it.score > 0 {
			hasPos = true
			break
		}
	}
	if hasPos {
		trimmed := out[:0]
		total = 0
		for _, it := range items {
			if it.score <= 0 {
				continue
			}
			if len(trimmed) >= limit {
				break
			}
			if maxChars >= 0 && total+len(it.e.Content) > maxChars {
				break
			}
			trimmed = append(trimmed, it.e)
			total += len(it.e.Content)
		}
		out = trimmed
	}
	return out
}

// takeLatestN returns the newest entries first (candidates in insertion
// order), applying Limit and MaxChars. Used when no query Embedding is set.
func takeLatestN(candidates []MemoryEntry, limit, maxChars int) []MemoryEntry {
	var out []MemoryEntry
	total := 0
	for n := len(candidates) - 1; n >= 0 && len(out) < limit; n-- {
		e := candidates[n]
		if maxChars >= 0 && total+len(e.Content) > maxChars {
			break
		}
		out = append(out, e)
		total += len(e.Content)
	}
	return out
}
