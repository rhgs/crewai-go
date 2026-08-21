package crewai

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// MemoryPolicy configures automatic save and inject of long-term memory
// during Kickoff. Nil on Crew means "use NewMemoryPolicy() defaults" at
// Kickoff start. A literal MemoryPolicy{} is NOT those defaults (bool zero
// is false) — construct via NewMemoryPolicy when you want the library
// defaults and then override fields.
type MemoryPolicy struct {
	// AutoSave stores each successful task output (default true).
	// Failures are never saved (G2). AutoSave errors are warned and
	// captured; they do not abort Kickoff (G11 / P3).
	AutoSave bool

	// AutoEmbed runs EmbeddingFunc on save when both are set (M4). Default
	// false. When true, embedding runs serially at the barrier fold (G8),
	// not inside worker goroutines.
	AutoEmbed bool

	// InjectWhenEmptyContext keeps today's behavior when true (default):
	// inject recalled memory only if task.Context is empty. When false,
	// memory is always merged with any explicit context (D-M3=C).
	// Regardless of this flag, inject never sees uncommitted / in-wave
	// entries (D-M7).
	InjectWhenEmptyContext bool

	// DefaultLimit is the Query Limit used for automatic injection.
	// Values outside (0, MaxMemoryQueryLimit] fall back to
	// DefaultMemoryQueryLimit inside the store.
	DefaultLimit int

	// DefaultMaxChars caps total Content characters injected. 0 means
	// DefaultMemoryMaxChars; negative means no cap (store path).
	DefaultMaxChars int

	// Scope defaults to Crew.Name when non-empty at Kickoff (G3); empty
	// otherwise (default partition). Explicit Scope on the policy wins.
	Scope MemoryScope
}

// NewMemoryPolicy returns the library defaults for automatic memory:
// AutoSave=true, InjectWhenEmptyContext=true, DefaultLimit/
// DefaultMaxChars set to the package defaults. Prefer this (or a nil
// Crew.MemoryPolicy) over a zero MemoryPolicy{} literal.
func NewMemoryPolicy() *MemoryPolicy {
	return &MemoryPolicy{
		AutoSave:               true,
		InjectWhenEmptyContext: true,
		DefaultLimit:           DefaultMemoryQueryLimit,
		DefaultMaxChars:        DefaultMemoryMaxChars,
	}
}

// resolvePolicy returns p if non-nil, otherwise NewMemoryPolicy().
func resolvePolicy(p *MemoryPolicy) *MemoryPolicy {
	if p != nil {
		return p
	}
	return NewMemoryPolicy()
}

// effectiveScope returns the scope used for this Kickoff: explicit policy
// Scope wins; otherwise Crew.Name when set (G3); otherwise empty.
func (c *Crew) effectiveScope(p *MemoryPolicy) MemoryScope {
	if p != nil && p.Scope != "" {
		return p.Scope
	}
	if c.Name != "" {
		return MemoryScope(c.Name)
	}
	return ""
}

// queryCommitted formats the committed MemoryStore snapshot for prompt
// injection. It never reads the in-wave buffer (D-M7): only entries already
// folded into the store are visible. Empty Text ⇒ latest N (D-M4).
func (c *Crew) queryCommitted(ctx context.Context, p *MemoryPolicy) string {
	if c.store == nil || p == nil {
		return ""
	}
	hits, err := c.store.Query(ctx, MemoryQuery{
		Scope:    c.effectiveScope(p),
		Limit:    p.DefaultLimit,
		MaxChars: p.DefaultMaxChars,
	})
	if err != nil {
		c.logger.WarnContext(ctx, "memory query failed", "error", redactError(err))
		return ""
	}
	return formatMemoryHits(hits)
}

// formatMemoryHits renders Query hits as a compact prompt block. Order is
// whatever the store returned (latest-N for the in-memory store).
func formatMemoryHits(hits []MemoryEntry) string {
	if len(hits) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Memory (recalled)\n")
	// Query returns latest-first; reverse so the prompt reads oldest→newest
	// within the recalled window (stable, declaration-ish order for the
	// committed snapshot).
	for i := len(hits) - 1; i >= 0; i-- {
		e := hits[i]
		b.WriteString("- [")
		b.WriteString(e.Agent)
		b.WriteString("] ")
		b.WriteString(e.Content)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

// bufferEntry is one successful task output waiting to be committed at the
// wave/stage barrier (D-M7). Failed/cancelled tasks leave no buffer entry.
type bufferEntry struct {
	task  *Task
	entry MemoryEntry
}

// beginMemoryBuffer arms the per-group buffer so execute stashes AutoSave
// entries instead of writing the store immediately. Must be paired with
// commitMemoryBuffer after the barrier join (or discardMemoryBuffer on
// abort paths that must not commit).
func (c *Crew) beginMemoryBuffer() {
	c.memBufMu.Lock()
	c.buffering = true
	c.memBuf = nil
	c.memBufMu.Unlock()
}

// discardMemoryBuffer drops any uncommitted entries and turns buffering off.
// Used when a fail-fast group aborts: failed/cancelled buffers never commit
// (risk register / G2).
func (c *Crew) discardMemoryBuffer() {
	c.memBufMu.Lock()
	c.buffering = false
	c.memBuf = nil
	c.memBufMu.Unlock()
}

// stashMemoryBuffer records a successful AutoSave candidate for later
// commit. Safe for concurrent use from runTaskGroup workers.
func (c *Crew) stashMemoryBuffer(task *Task, entry MemoryEntry) {
	c.memBufMu.Lock()
	defer c.memBufMu.Unlock()
	c.memBuf = append(c.memBuf, bufferEntry{task: task, entry: entry})
}

// commitMemoryBuffer writes buffered entries to the store in the order of
// the provided tasks slice (declaration order within the group). Entries
// for tasks not in tasks, or tasks without a buffer slot, are skipped.
// Buffering is turned off before any Put so a nested path cannot re-stash.
// When AutoEmbed is set, each entry is embedded serially here (G8) before
// Put — never inside worker goroutines.
func (c *Crew) commitMemoryBuffer(ctx context.Context, tasks []*Task, p *MemoryPolicy) {
	c.memBufMu.Lock()
	buf := c.memBuf
	c.memBuf = nil
	c.buffering = false
	c.memBufMu.Unlock()

	if c.store == nil || p == nil || !p.AutoSave || len(buf) == 0 {
		return
	}

	// Index buffer by task pointer for O(1) lookup while folding in
	// declaration order.
	byTask := make(map[*Task]MemoryEntry, len(buf))
	for _, b := range buf {
		byTask[b.task] = b.entry
	}
	for _, t := range tasks {
		e, ok := byTask[t]
		if !ok {
			continue
		}
		e = c.maybeEmbedEntry(ctx, e, t, p)
		if _, err := c.store.Put(ctx, e); err != nil {
			// G11: warn+capture, do not abort Kickoff.
			c.logger.WarnContext(ctx, "memory AutoSave failed",
				"task", taskLabel(t, 0), "error", redactError(err))
			if t != nil {
				t.AddWarning(fmt.Sprintf("memory AutoSave failed: %v", err))
			}
		}
	}
}

// maybeEmbedEntry runs Embed serially when AutoEmbed is enabled. Soft-fails
// on embedder errors / short results (entry saved without embedding).
func (c *Crew) maybeEmbedEntry(ctx context.Context, e MemoryEntry, t *Task, p *MemoryPolicy) MemoryEntry {
	if p == nil || !p.AutoEmbed || c.Embed == nil || e.Content == "" {
		return e
	}
	// Respect cancellation between serial embeds without aborting Kickoff.
	if err := ctx.Err(); err != nil {
		c.logger.WarnContext(ctx, "memory AutoEmbed skipped",
			"task", taskLabel(t, 0), "error", redactError(err))
		return e
	}
	vecs, err := c.Embed(ctx, []string{e.Content})
	if err != nil {
		c.logger.WarnContext(ctx, "memory AutoEmbed failed",
			"task", taskLabel(t, 0), "error", redactError(err))
		if t != nil {
			t.AddWarning(fmt.Sprintf("memory AutoEmbed failed: %v", err))
		}
		return e
	}
	if len(vecs) < 1 || len(vecs[0]) == 0 {
		c.logger.WarnContext(ctx, "memory AutoEmbed returned empty vector",
			"task", taskLabel(t, 0))
		return e
	}
	// Copy so the embedder cannot retain a live alias into the store.
	emb := make([]float32, len(vecs[0]))
	copy(emb, vecs[0])
	e.Embedding = emb
	return e
}

// autoSaveEntry builds the MemoryEntry for a successful task output.
func (c *Crew) autoSaveEntry(agent *Agent, task *Task, result string, p *MemoryPolicy) MemoryEntry {
	agentRole := ""
	if agent != nil {
		agentRole = agent.Role
	}
	taskName := ""
	if task != nil {
		taskName = task.Name
	}
	return MemoryEntry{
		Scope:     c.effectiveScope(p),
		Agent:     agentRole,
		Task:      taskName,
		Content:   result,
		CreatedAt: time.Now().UTC(), // finish time (G6); commit order is declaration
	}
}
