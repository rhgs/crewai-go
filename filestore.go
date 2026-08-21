package crewai

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
)

// FileStore is a stdlib-only durable MemoryStore backed by JSONL files under
// a caller-trusted root directory (D-M2=A). Layout:
//
//	{root}/
//	  scopes/{urlsafeScope}/
//	    entries.jsonl   # append-only MemoryEntry JSON lines (+ tombstones)
//	    meta.json       # schema version, corrupt-skip counter
//
// v1 is single-writer (G7): concurrent Puts/Queries on one *FileStore are
// mutex-safe, but two processes must not share the same root. The root path
// is caller-trusted — never pass model-controlled paths. Files are created
// with mode 0600 and directories with 0700.
//
// Corrupt JSONL lines are skipped on Open and counted in meta (D-M5). The
// application owns Close (D-M6).
type FileStore struct {
	root string

	mu     sync.RWMutex
	closed bool
	// byScope holds the in-RAM index loaded from disk. Key is the raw
	// MemoryScope (empty = default partition).
	byScope map[MemoryScope]*scopeIndex
}

// scopeIndex is the RAM view of one scope partition.
type scopeIndex struct {
	// entries preserves insertion/commit order for latest-N Query.
	entries []MemoryEntry
	// byID maps id → index into entries (-1 after logical delete).
	byID map[string]int
	// corruptSkipped is the running count of bad lines seen for this scope.
	corruptSkipped int
	// dirty marks that meta.json should be rewritten on the next Put/Delete/Close.
	dirty bool
}

// fileMeta is the on-disk meta.json payload.
type fileMeta struct {
	Version        int         `json:"version"`
	Scope          MemoryScope `json:"scope"` // raw logical scope (may differ from dir name)
	CorruptSkipped int         `json:"corrupt_skipped"`
	EntryCount     int         `json:"entry_count"`
}

// tombstone is an append-only delete marker in the JSONL stream.
type tombstone struct {
	ID      string `json:"id"`
	Deleted bool   `json:"deleted"`
}

// FileStore schema version written into meta.json.
const fileStoreSchemaVersion = 1

// defaultScopeDir is the on-disk directory name for the empty MemoryScope.
const defaultScopeDir = "_default"

// ErrFileStoreClosed is returned when a method is called after Close.
var ErrFileStoreClosed = errors.New("crewai: file store closed")

// ErrFileStoreRoot is returned when OpenFileStore receives an empty or
// unusable root path.
var ErrFileStoreRoot = errors.New("crewai: file store root path rejected")

// OpenFileStore opens (or creates) a FileStore rooted at dir. dir is cleaned
// and resolved to an absolute path; the directory is created with mode 0700
// if missing. Existing scopes are loaded into RAM (corrupt lines skipped,
// D-M5). The returned store is safe for concurrent use within one process.
func OpenFileStore(dir string) (*FileStore, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, ErrFileStoreRoot
	}
	abs, err := filepath.Abs(filepath.Clean(dir))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrFileStoreRoot, err)
	}
	// Reject paths that look empty after clean (e.g. "." of a missing drive
	// is fine; truly empty is already handled).
	if abs == "" {
		return nil, ErrFileStoreRoot
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, fmt.Errorf("crewai: creating file store root: %w", err)
	}

	fs := &FileStore{
		root:    abs,
		byScope: make(map[MemoryScope]*scopeIndex),
	}
	if err := fs.loadAll(); err != nil {
		return nil, err
	}
	return fs, nil
}

// Root returns the absolute root directory of the store.
func (fs *FileStore) Root() string { return fs.root }

// Put appends e to the scope's JSONL and updates the RAM index. Empty ID is
// assigned; Content over MaxMemoryEntryBytes is rejected. Safe for concurrent
// use within one process.
func (fs *FileStore) Put(ctx context.Context, e MemoryEntry) (MemoryEntry, error) {
	if err := ctx.Err(); err != nil {
		return MemoryEntry{}, err
	}
	if len(e.Content) > MaxMemoryEntryBytes {
		return MemoryEntry{}, ErrMemoryEntryTooLarge
	}

	fs.mu.Lock()
	defer fs.mu.Unlock()
	if fs.closed {
		return MemoryEntry{}, ErrFileStoreClosed
	}

	idx := fs.scopeLocked(e.Scope)
	if e.ID == "" {
		e.ID = fs.freshIDLocked()
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	} else {
		e.CreatedAt = e.CreatedAt.UTC()
	}

	// If the ID already exists in this scope, replace in place (upsert) so
	// Delete+Put cycles and re-embeds stay stable. Otherwise append.
	if i, ok := idx.byID[e.ID]; ok && i >= 0 {
		idx.entries[i] = e
	} else {
		idx.byID[e.ID] = len(idx.entries)
		idx.entries = append(idx.entries, e)
	}
	idx.dirty = true

	if err := fs.appendLineLocked(e.Scope, e); err != nil {
		// Roll back the RAM mutation on disk failure so Query stays honest.
		// Best-effort: remove the just-added entry if it was an append.
		if i, ok := idx.byID[e.ID]; ok && i == len(idx.entries)-1 {
			idx.entries = idx.entries[:len(idx.entries)-1]
			delete(idx.byID, e.ID)
		}
		return MemoryEntry{}, err
	}
	if err := fs.writeMetaLocked(e.Scope, idx); err != nil {
		return MemoryEntry{}, err
	}
	return e, nil
}

// Query returns matching entries from the RAM index (loaded at Open and
// kept current by Put/Delete). Semantics match *Memory: empty Text ⇒ latest
// N; Limit/MaxChars clamped; Scope partitions. Embedding is ignored in M3
// (M4 adds cosine ranking).
func (fs *FileStore) Query(ctx context.Context, q MemoryQuery) ([]MemoryEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	if fs.closed {
		return nil, ErrFileStoreClosed
	}

	limit := q.Limit
	if limit <= 0 || limit > MaxMemoryQueryLimit {
		limit = DefaultMemoryQueryLimit
	}
	maxChars := q.MaxChars
	if maxChars == 0 {
		maxChars = DefaultMemoryMaxChars
	}

	idx := fs.byScope[q.Scope]
	if idx == nil {
		return nil, nil
	}

	text := strings.ToLower(strings.TrimSpace(q.Text))
	// Collect live entry indexes in insertion order, then walk newest-first.
	var live []int
	for i, e := range idx.entries {
		if i2, ok := idx.byID[e.ID]; !ok || i2 != i {
			continue // tombstoned or replaced slot
		}
		if text != "" &&
			!strings.Contains(strings.ToLower(e.Content), text) &&
			!strings.Contains(strings.ToLower(e.Task), text) {
			continue
		}
		live = append(live, i)
	}

	var out []MemoryEntry
	total := 0
	for n := len(live) - 1; n >= 0 && len(out) < limit; n-- {
		e := idx.entries[live[n]]
		if maxChars >= 0 && total+len(e.Content) > maxChars {
			break
		}
		out = append(out, cloneEntry(e))
		total += len(e.Content)
	}
	return out, nil
}

// Delete removes the entry with the given id within scope. Unknown id is a
// no-op. A tombstone line is appended so a subsequent Open does not revive
// the entry (D-M5 load path honors tombstones).
func (fs *FileStore) Delete(ctx context.Context, scope MemoryScope, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if id == "" {
		return nil
	}
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if fs.closed {
		return ErrFileStoreClosed
	}

	idx := fs.byScope[scope]
	if idx == nil {
		return nil
	}
	i, ok := idx.byID[id]
	if !ok || i < 0 {
		return nil
	}
	// Logical delete: drop from byID so Query skips it. The slot in entries
	// stays so indexes of later entries remain stable until next Open.
	delete(idx.byID, id)
	idx.dirty = true

	if err := fs.appendLineLocked(scope, tombstone{ID: id, Deleted: true}); err != nil {
		// Re-instate on disk failure.
		idx.byID[id] = i
		return err
	}
	return fs.writeMetaLocked(scope, idx)
}

// Close flushes meta for dirty scopes and marks the store closed. Safe to
// call once; subsequent method calls return ErrFileStoreClosed. The root
// directory is not removed.
func (fs *FileStore) Close() error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if fs.closed {
		return nil
	}
	var first error
	for scope, idx := range fs.byScope {
		if !idx.dirty {
			continue
		}
		if err := fs.writeMetaLocked(scope, idx); err != nil && first == nil {
			first = err
		}
	}
	fs.closed = true
	return first
}

// CorruptSkipped returns the total number of corrupt JSONL lines skipped
// across all scopes (D-M5). Useful for tests and ops dashboards.
func (fs *FileStore) CorruptSkipped() int {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	n := 0
	for _, idx := range fs.byScope {
		n += idx.corruptSkipped
	}
	return n
}

// ---------------------------------------------------------------------------
// internals
// ---------------------------------------------------------------------------

func (fs *FileStore) scopeLocked(scope MemoryScope) *scopeIndex {
	idx := fs.byScope[scope]
	if idx == nil {
		idx = &scopeIndex{
			byID: make(map[string]int),
		}
		fs.byScope[scope] = idx
	}
	return idx
}

func (fs *FileStore) freshIDLocked() string {
	// Reuse the package helper; collision against the whole store is checked.
	for {
		id := newMemoryID()
		used := false
		for _, idx := range fs.byScope {
			if _, ok := idx.byID[id]; ok {
				used = true
				break
			}
		}
		if !used {
			return id
		}
	}
}

func (fs *FileStore) scopeDir(scope MemoryScope) string {
	name := scopeDirName(scope)
	return filepath.Join(fs.root, "scopes", name)
}

func (fs *FileStore) entriesPath(scope MemoryScope) string {
	return filepath.Join(fs.scopeDir(scope), "entries.jsonl")
}

func (fs *FileStore) metaPath(scope MemoryScope) string {
	return filepath.Join(fs.scopeDir(scope), "meta.json")
}

// scopeDirName maps a MemoryScope to a single path segment. Empty scope uses
// defaultScopeDir. Non-empty scopes are percent-style escaped to a safe
// subset so "/", "..", and control chars cannot escape the scopes/ tree.
func scopeDirName(scope MemoryScope) string {
	s := string(scope)
	if s == "" {
		return defaultScopeDir
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			// Escape everything else (including '/' and unicode) as _uXXXX_.
			if r > unicode.MaxASCII || !unicode.IsPrint(r) || r == '/' || r == '\\' {
				fmt.Fprintf(&b, "_u%04X_", r)
			} else {
				// Other printable ASCII (e.g. space, colon) → underscore run.
				b.WriteByte('_')
			}
		}
	}
	out := b.String()
	// Defend against "." / ".." after escaping collapse.
	if out == "." || out == ".." || out == "" {
		return defaultScopeDir
	}
	return out
}

func (fs *FileStore) loadAll() error {
	scopesRoot := filepath.Join(fs.root, "scopes")
	entries, err := os.ReadDir(scopesRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("crewai: reading file store scopes: %w", err)
	}
	for _, de := range entries {
		if !de.IsDir() {
			continue
		}
		name := de.Name()
		dir := filepath.Join(scopesRoot, name)
		// Prefer the raw scope recorded in meta.json (survives escaping).
		// Fall back to dir name, with _default → empty.
		scope := MemoryScope(name)
		if name == defaultScopeDir {
			scope = ""
		}
		if raw, err := os.ReadFile(filepath.Join(dir, "meta.json")); err == nil {
			var meta fileMeta
			if json.Unmarshal(raw, &meta) == nil {
				// meta.Scope is authoritative when meta exists; empty meta.Scope
				// with a non-default dir name still means empty only for
				// defaultScopeDir (legacy files without the field keep dir name).
				if meta.Scope != "" || name == defaultScopeDir {
					scope = meta.Scope
				}
			}
		}
		if err := fs.loadScope(scope, dir); err != nil {
			return err
		}
	}
	return nil
}

func (fs *FileStore) loadScope(scope MemoryScope, dir string) error {
	idx := &scopeIndex{byID: make(map[string]int)}
	// meta.json is optional on first create.
	if raw, err := os.ReadFile(filepath.Join(dir, "meta.json")); err == nil {
		var meta fileMeta
		if json.Unmarshal(raw, &meta) == nil {
			idx.corruptSkipped = meta.CorruptSkipped
		}
	}

	f, err := os.Open(filepath.Join(dir, "entries.jsonl"))
	if err != nil {
		if os.IsNotExist(err) {
			fs.byScope[scope] = idx
			return nil
		}
		return fmt.Errorf("crewai: opening entries.jsonl: %w", err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	// Allow long lines up to a bit over MaxMemoryEntryBytes + JSON framing.
	sc.Buffer(make([]byte, 0, 64*1024), MaxMemoryEntryBytes+64*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		// Tombstone first (small struct).
		var tomb tombstone
		if err := json.Unmarshal([]byte(line), &tomb); err == nil && tomb.Deleted && tomb.ID != "" {
			if i, ok := idx.byID[tomb.ID]; ok {
				delete(idx.byID, tomb.ID)
				_ = i
			}
			continue
		}
		var e MemoryEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil || e.ID == "" {
			// D-M5: skip corrupt / unusable lines and count them.
			idx.corruptSkipped++
			idx.dirty = true
			continue
		}
		// Force the entry's Scope to the partition it was loaded from so a
		// mismatched field cannot leak across scopes on Query.
		e.Scope = scope
		if i, ok := idx.byID[e.ID]; ok {
			idx.entries[i] = e
		} else {
			idx.byID[e.ID] = len(idx.entries)
			idx.entries = append(idx.entries, e)
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("crewai: scanning entries.jsonl: %w", err)
	}
	fs.byScope[scope] = idx
	// Persist updated corrupt counter if we skipped anything new.
	if idx.dirty {
		if err := fs.writeMetaLocked(scope, idx); err != nil {
			return err
		}
	}
	return nil
}

func (fs *FileStore) appendLineLocked(scope MemoryScope, v any) error {
	dir := fs.scopeDir(scope)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("crewai: creating scope dir: %w", err)
	}
	path := fs.entriesPath(scope)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("crewai: opening entries.jsonl for append: %w", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	// Encoder adds a trailing newline.
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("crewai: encoding memory entry: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("crewai: syncing entries.jsonl: %w", err)
	}
	return nil
}

func (fs *FileStore) writeMetaLocked(scope MemoryScope, idx *scopeIndex) error {
	dir := fs.scopeDir(scope)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("crewai: creating scope dir: %w", err)
	}
	// Count live entries.
	live := 0
	for _, i := range idx.byID {
		if i >= 0 {
			live++
		}
	}
	meta := fileMeta{
		Version:        fileStoreSchemaVersion,
		Scope:          scope,
		CorruptSkipped: idx.corruptSkipped,
		EntryCount:     live,
	}
	raw, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	path := fs.metaPath(scope)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fmt.Errorf("crewai: writing meta.json: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("crewai: renaming meta.json: %w", err)
	}
	idx.dirty = false
	return nil
}

// cloneEntry returns a shallow copy with cloned Metadata/Embedding slices so
// callers cannot mutate the store's RAM index.
func cloneEntry(e MemoryEntry) MemoryEntry {
	out := e
	if e.Metadata != nil {
		out.Metadata = make(map[string]string, len(e.Metadata))
		for k, v := range e.Metadata {
			out.Metadata[k] = v
		}
	}
	if e.Embedding != nil {
		out.Embedding = make([]float32, len(e.Embedding))
		copy(out.Embedding, e.Embedding)
	}
	return out
}

// compile-time guarantee.
var _ MemoryStore = (*FileStore)(nil)
