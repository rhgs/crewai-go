package crewai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// Memory tool names (D-MT1). Opt-in via Crew.EnableMemoryTools or explicit
// NewRecallMemoryTool / NewRememberTool (D-MT2).
const (
	recallMemoryToolName = "recall_memory"
	rememberToolName     = "remember"
)

var (
	recallMemorySchema = json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"Search text. Empty returns the latest committed entries."}}}`)
	rememberSchema     = json.RawMessage(`{"type":"object","properties":{"content":{"type":"string","description":"Text to store in long-term memory immediately."}},"required":["content"]}`)
)

// NewRecallMemoryTool returns a Tool named recall_memory that queries the
// crew's MemoryStore. Input is plain text or JSON {"query":"..."}. An empty
// query returns the latest N entries. When Crew.Embed is set the query is
// also embedded and ranked by cosine (D-MT3). Output is capped at
// MaxToolOutputBytes (D-MT5). Not a FactSource.
//
// Attach with agent.WithTools(NewRecallMemoryTool(crew)) or set
// Crew.EnableMemoryTools (default false).
func NewRecallMemoryTool(crew *Crew) Tool {
	return &recallMemoryTool{crew: crew}
}

// NewRememberTool returns a Tool named remember that Puts content into the
// crew's MemoryStore immediately (D-MT4), bypassing the D-M7 AutoSave
// buffer. Input is plain text or JSON {"content":"..."}. Not a FactSource.
//
// Attach with agent.WithTools(NewRememberTool(crew)) or set
// Crew.EnableMemoryTools (default false).
func NewRememberTool(crew *Crew) Tool {
	return &rememberTool{crew: crew}
}

type recallMemoryTool struct{ crew *Crew }

func (t *recallMemoryTool) Name() string { return recallMemoryToolName }

func (t *recallMemoryTool) Description() string {
	return "Recall entries from long-term memory. Input: a search query, or JSON " +
		`{"query":"<text>"}. Empty query returns the latest entries. ` +
		"Use this when you need facts stored earlier that are not already in context."
}

func (t *recallMemoryTool) Schema() json.RawMessage { return recallMemorySchema }

func (t *recallMemoryTool) Call(ctx context.Context, input string) (string, error) {
	if len(input) > MaxToolArgsBytes {
		return "Error: memory tool input exceeds size limit.", nil
	}
	query, _ := parseMemoryToolArg(input, "query")
	if t == nil || t.crew == nil {
		return "Error: memory store is not configured.", nil
	}
	return t.crew.recallMemory(ctx, query), nil
}

type rememberTool struct{ crew *Crew }

func (t *rememberTool) Name() string { return rememberToolName }

func (t *rememberTool) Description() string {
	return "Store a note in long-term memory immediately (visible to later " +
		"recall_memory calls in this run). Input: the text to remember, or JSON " +
		`{"content":"<text>"}. Do not use this to merge parallel sibling outputs; ` +
		"use task context for that."
}

func (t *rememberTool) Schema() json.RawMessage { return rememberSchema }

func (t *rememberTool) Call(ctx context.Context, input string) (string, error) {
	if len(input) > MaxToolArgsBytes {
		return "Error: memory tool input exceeds size limit.", nil
	}
	content, isJSON := parseMemoryToolArg(input, "content")
	if content == "" {
		if isJSON {
			return "Error: remember requires JSON {\"content\":\"...\"} or plain text.", nil
		}
		return "Error: remember requires non-empty content.", nil
	}
	if t == nil || t.crew == nil {
		return "Error: memory store is not configured.", nil
	}
	return t.crew.rememberMemory(ctx, content), nil
}

var (
	_ Tool           = (*recallMemoryTool)(nil)
	_ Tool           = (*rememberTool)(nil)
	_ SchemaProvider = (*recallMemoryTool)(nil)
	_ SchemaProvider = (*rememberTool)(nil)
)

// parseMemoryToolArg accepts plain text or a JSON object with key. When
// input is a JSON object without a usable string value for key, isJSON is
// true and value is empty so callers can distinguish "empty query" from
// "malformed remember JSON".
func parseMemoryToolArg(input, key string) (value string, isJSON bool) {
	s := strings.TrimSpace(input)
	if s == "" {
		return "", false
	}
	if s[0] != '{' {
		return s, false
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(s), &obj); err != nil {
		return s, false
	}
	raw, ok := obj[key]
	if !ok {
		return "", true
	}
	var str string
	if err := json.Unmarshal(raw, &str); err != nil {
		return "", true
	}
	return strings.TrimSpace(str), true
}

// memoryToolStore returns the store tools should use: the Kickoff-resolved
// store when set, otherwise Crew.MemoryStore. Explicit tools work without
// a Kickoff having run.
func (c *Crew) memoryToolStore() MemoryStore {
	if c == nil {
		return nil
	}
	if c.store != nil {
		return c.store
	}
	return c.MemoryStore
}

func (c *Crew) memoryToolPolicy() *MemoryPolicy {
	if c == nil {
		return NewMemoryPolicy()
	}
	if c.policy != nil {
		return c.policy
	}
	return resolvePolicy(c.MemoryPolicy)
}

// recallMemory runs a budgeted Query for the recall_memory tool (D-MT3,
// D-MT5). Operational failures are returned as observation text.
func (c *Crew) recallMemory(ctx context.Context, query string) string {
	store := c.memoryToolStore()
	if store == nil {
		return "Error: memory store is not configured. Set Crew.Memory or Crew.MemoryStore."
	}
	p := c.memoryToolPolicy()
	q := MemoryQuery{
		Scope:    c.effectiveScope(p),
		Text:     query,
		Limit:    p.DefaultLimit,
		MaxChars: MaxToolOutputBytes,
	}
	if c.Embed != nil && strings.TrimSpace(query) != "" {
		if err := ctx.Err(); err != nil {
			return fmt.Sprintf("Error: memory recall cancelled: %v", redactError(err))
		}
		vecs, err := c.embedLocked(ctx, []string{query})
		if err != nil {
			c.memoryToolLog().WarnContext(ctx, "memory tool embed failed; falling back to text",
				"error", redactError(err))
		} else if len(vecs) > 0 && len(vecs[0]) > 0 {
			emb := make([]float32, len(vecs[0]))
			copy(emb, vecs[0])
			q.Embedding = emb
		}
	}
	hits, err := store.Query(ctx, q)
	if err != nil {
		return fmt.Sprintf("Error: memory query failed: %v", redactError(err))
	}
	if len(hits) == 0 {
		return "No matching memory entries."
	}
	return truncateToolOutput(formatMemoryHits(hits))
}

// rememberMemory Puts content immediately (D-MT4), outside the D-M7
// AutoSave buffer, so a later recall_memory in the same Kickoff (including
// a parallel sibling) can see it. AutoEmbed runs inline when configured.
func (c *Crew) rememberMemory(ctx context.Context, content string) string {
	store := c.memoryToolStore()
	if store == nil {
		return "Error: memory store is not configured. Set Crew.Memory or Crew.MemoryStore."
	}
	p := c.memoryToolPolicy()
	entry := MemoryEntry{
		Scope:     c.effectiveScope(p),
		Agent:     agentRoleFromCtx(ctx),
		Content:   content,
		CreatedAt: time.Now().UTC(),
	}
	if t, ok := warningSinkFromCtx(ctx).(*Task); ok && t != nil {
		entry.Task = t.Name
	}
	entry = c.maybeEmbedEntry(ctx, entry, nil, p)
	stored, err := store.Put(ctx, entry)
	if err != nil {
		return fmt.Sprintf("Error: memory remember failed: %v", redactError(err))
	}
	id := stored.ID
	if id == "" {
		id = "(assigned)"
	}
	return "Remembered id=" + id
}

func (c *Crew) memoryToolLog() *slog.Logger {
	if c != nil && c.logger != nil {
		return c.logger
	}
	return slog.Default()
}

// attachMemoryTools adds recall_memory and remember to each agent that does
// not already have them. Safe to call multiple times (idempotent per agent).
func (c *Crew) attachMemoryTools() {
	if c == nil {
		return
	}
	recall := NewRecallMemoryTool(c)
	remember := NewRememberTool(c)
	attach := func(a *Agent) {
		if a == nil {
			return
		}
		if !hasToolNamed(a.Tools, recallMemoryToolName) {
			a.Tools = append(a.Tools, recall)
		}
		if !hasToolNamed(a.Tools, rememberToolName) {
			a.Tools = append(a.Tools, remember)
		}
	}
	for _, a := range c.Agents {
		attach(a)
	}
	if c.ManagerAgent != nil {
		attach(c.ManagerAgent)
	}
}

func hasToolNamed(tools []Tool, name string) bool {
	for _, t := range tools {
		if t != nil && t.Name() == name {
			return true
		}
	}
	return false
}
