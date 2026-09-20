package crewai

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
)

func TestParseMemoryToolArg(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, key, want string
		isJSON        bool
	}{
		{"plain text", "query", "plain text", false},
		{`{"query":"hello"}`, "query", "hello", true},
		{`{"content":"note"}`, "content", "note", true},
		{`{"query":"  x  "}`, "query", "x", true},
		{"", "query", "", false},
		{`{"other":"x"}`, "query", "", true},
		{`{"content":1}`, "content", "", true},
		{"{not json", "query", "{not json", false},
	}
	for _, c := range cases {
		got, isJSON := parseMemoryToolArg(c.in, c.key)
		if got != c.want || isJSON != c.isJSON {
			t.Errorf("parseMemoryToolArg(%q,%q) = (%q,%v), want (%q,%v)",
				c.in, c.key, got, isJSON, c.want, c.isJSON)
		}
	}
}

func TestRecallMemoryTool_NameSchema(t *testing.T) {
	tool := NewRecallMemoryTool(nil)
	if tool.Name() != recallMemoryToolName {
		t.Fatalf("name = %q", tool.Name())
	}
	if sp, ok := tool.(SchemaProvider); !ok || len(sp.Schema()) == 0 {
		t.Fatal("SchemaProvider missing")
	}
	if !strings.Contains(tool.Description(), "recall") && !strings.Contains(strings.ToLower(tool.Description()), "memory") {
		t.Errorf("description = %q", tool.Description())
	}
}

func TestRememberTool_NameSchema(t *testing.T) {
	tool := NewRememberTool(nil)
	if tool.Name() != rememberToolName {
		t.Fatalf("name = %q", tool.Name())
	}
	if sp, ok := tool.(SchemaProvider); !ok || len(sp.Schema()) == 0 {
		t.Fatal("SchemaProvider missing")
	}
}

func TestMemoryToolHelpers_NilCrew(t *testing.T) {
	var c *Crew
	if c.memoryToolStore() != nil {
		t.Fatal("nil store")
	}
	if c.memoryToolPolicy() == nil {
		t.Fatal("nil policy should still return defaults")
	}
	var rec *recallMemoryTool
	out, err := rec.Call(context.Background(), "q")
	if err != nil || !strings.Contains(out, "not configured") {
		t.Fatalf("nil recall: %q %v", out, err)
	}
	var rem *rememberTool
	out, err = rem.Call(context.Background(), "note")
	if err != nil || !strings.Contains(out, "not configured") {
		t.Fatalf("nil remember: %q %v", out, err)
	}
}

func TestMemoryTools_NilCrew(t *testing.T) {
	ctx := context.Background()
	out, err := NewRecallMemoryTool(nil).Call(ctx, "q")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "not configured") {
		t.Errorf("recall nil crew = %q", out)
	}
	out, err = NewRememberTool(nil).Call(ctx, "note")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "not configured") {
		t.Errorf("remember nil crew = %q", out)
	}
}

func TestMemoryTools_NoStore(t *testing.T) {
	crew := NewCrew(nil, nil)
	ctx := context.Background()
	out, err := NewRecallMemoryTool(crew).Call(ctx, "q")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "not configured") {
		t.Errorf("recall = %q", out)
	}
	out, err = NewRememberTool(crew).Call(ctx, "note")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "not configured") {
		t.Errorf("remember = %q", out)
	}
}

func TestRemember_EmptyContent(t *testing.T) {
	store := NewMemory()
	crew := NewCrew(nil, nil)
	crew.MemoryStore = store
	ctx := context.Background()

	out, err := NewRememberTool(crew).Call(ctx, "   ")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "non-empty") {
		t.Errorf("blank = %q", out)
	}
	out, err = NewRememberTool(crew).Call(ctx, `{"content":""}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "content") {
		t.Errorf("empty json = %q", out)
	}
}

func TestRememberThenRecall_Standalone(t *testing.T) {
	store := NewMemory()
	crew := NewCrew(nil, nil)
	crew.Name = "finance"
	crew.MemoryStore = store
	ctx := ContextWithAgentRole(context.Background(), "Analyst")

	out, err := NewRememberTool(crew).Call(ctx, "Q1 revenue grew 12%")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "Remembered id=") {
		t.Fatalf("remember = %q", out)
	}

	hits, err := store.Query(ctx, MemoryQuery{Scope: "finance", Limit: 8})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Content != "Q1 revenue grew 12%" {
		t.Fatalf("store = %+v", hits)
	}
	if hits[0].Agent != "Analyst" {
		t.Errorf("agent = %q", hits[0].Agent)
	}
	if hits[0].Scope != "finance" {
		t.Errorf("scope = %q", hits[0].Scope)
	}

	recalled, err := NewRecallMemoryTool(crew).Call(ctx, "revenue")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(recalled, "Q1 revenue grew 12%") {
		t.Errorf("recall = %q", recalled)
	}

	empty, err := NewRecallMemoryTool(crew).Call(ctx, "no-such-token")
	if err != nil {
		t.Fatal(err)
	}
	if empty != "No matching memory entries." {
		t.Errorf("miss = %q", empty)
	}
}

func TestRecall_JSONQueryAndEmptyLatest(t *testing.T) {
	store := NewMemory()
	crew := NewCrew(nil, nil)
	crew.MemoryStore = store
	ctx := context.Background()
	_, _ = store.Put(ctx, MemoryEntry{Content: "alpha"})
	_, _ = store.Put(ctx, MemoryEntry{Content: "beta"})

	out, err := NewRecallMemoryTool(crew).Call(ctx, `{"query":""}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "alpha") || !strings.Contains(out, "beta") {
		t.Errorf("latest = %q", out)
	}
	out, err = NewRecallMemoryTool(crew).Call(ctx, `{"query":"beta"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "beta") || strings.Contains(out, "alpha") {
		t.Errorf("filtered = %q", out)
	}
}

func TestRemember_Oversized(t *testing.T) {
	store := NewMemory()
	crew := NewCrew(nil, nil)
	crew.MemoryStore = store
	big := strings.Repeat("x", MaxMemoryEntryBytes+1)
	out, err := NewRememberTool(crew).Call(context.Background(), big)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "remember failed") {
		t.Errorf("oversized = %q", out)
	}
}

func TestRemember_ImmediatePutBypassesDM7Buffer(t *testing.T) {
	// D-MT4: remember Puts immediately even while a wave buffer is open.
	store := NewMemory()
	crew := NewCrew(nil, nil)
	crew.MemoryStore = store
	crew.beginMemoryBuffer()
	defer crew.discardMemoryBuffer()

	out, err := NewRememberTool(crew).Call(context.Background(), "live note")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "Remembered id=") {
		t.Fatalf("remember = %q", out)
	}
	hits, err := store.Query(context.Background(), MemoryQuery{Text: "live", Limit: 8})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("remember must be visible before barrier, hits=%+v", hits)
	}
}

func TestRecall_EmbedsQueryWhenEmbedSet(t *testing.T) {
	store := NewMemory()
	ctx := context.Background()
	_, _ = store.Put(ctx, MemoryEntry{Content: "cats", Embedding: []float32{1, 0}})
	_, _ = store.Put(ctx, MemoryEntry{Content: "quantum", Embedding: []float32{0, 1}})

	var calls atomic.Int32
	crew := NewCrew(nil, nil)
	crew.MemoryStore = store
	crew.Embed = func(_ context.Context, texts []string) ([][]float32, error) {
		calls.Add(1)
		if len(texts) != 1 || texts[0] != "feline" {
			t.Errorf("embed texts = %v", texts)
		}
		return [][]float32{{1, 0}}, nil
	}

	out, err := NewRecallMemoryTool(crew).Call(ctx, "feline")
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Errorf("embed calls = %d", calls.Load())
	}
	if !strings.Contains(out, "cats") {
		t.Errorf("semantic recall = %q", out)
	}
	if strings.Contains(out, "quantum") {
		t.Errorf("orthogonal should be trimmed: %q", out)
	}
}

func TestRecall_EmbedFailureFallsBackToText(t *testing.T) {
	store := NewMemory()
	ctx := context.Background()
	_, _ = store.Put(ctx, MemoryEntry{Content: "revenue grew"})
	crew := NewCrew(nil, nil)
	crew.MemoryStore = store
	crew.Embed = func(context.Context, []string) ([][]float32, error) {
		return nil, errors.New("embed down")
	}
	out, err := NewRecallMemoryTool(crew).Call(ctx, "revenue")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "revenue grew") {
		t.Errorf("fallback = %q", out)
	}
}

func TestRecall_CancelledContext(t *testing.T) {
	store := NewMemory()
	crew := NewCrew(nil, nil)
	crew.MemoryStore = store
	crew.Embed = func(context.Context, []string) ([][]float32, error) {
		t.Fatal("embed should not run after cancel")
		return nil, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out, err := NewRecallMemoryTool(crew).Call(ctx, "q")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "cancelled") {
		t.Errorf("cancelled = %q", out)
	}
}

func TestRecall_QueryError(t *testing.T) {
	crew := NewCrew(nil, nil)
	crew.MemoryStore = errStore{}
	out, err := NewRecallMemoryTool(crew).Call(context.Background(), "q")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "query failed") {
		t.Errorf("query err = %q", out)
	}
}

type errStore struct{ msg string }

func (s errStore) Put(context.Context, MemoryEntry) (MemoryEntry, error) {
	msg := s.msg
	if msg == "" {
		msg = "put boom"
	}
	return MemoryEntry{}, errors.New(msg)
}
func (s errStore) Query(context.Context, MemoryQuery) ([]MemoryEntry, error) {
	msg := s.msg
	if msg == "" {
		msg = "query boom"
	}
	return nil, errors.New(msg)
}
func (errStore) Delete(context.Context, MemoryScope, string) error { return nil }
func (errStore) Close() error                                      { return nil }

type emptyIDStore struct{}

func (emptyIDStore) Put(_ context.Context, e MemoryEntry) (MemoryEntry, error) {
	e.ID = ""
	return e, nil
}
func (emptyIDStore) Query(context.Context, MemoryQuery) ([]MemoryEntry, error) {
	return nil, nil
}
func (emptyIDStore) Delete(context.Context, MemoryScope, string) error { return nil }
func (emptyIDStore) Close() error                                      { return nil }

func TestCrew_EnableMemoryTools_AttachesOnce(t *testing.T) {
	llm := &llmStub{responses: []string{"Final Answer: done"}}
	a := NewAgent("A", "g", "", llm)
	b := NewAgent("B", "g", "", &llmStub{responses: []string{"Final Answer: done"}})
	task := NewTask("say hi", "hi", a)
	crew := NewCrew([]*Agent{a, b}, []*Task{task})
	crew.Memory = true
	crew.EnableMemoryTools = true
	mgr := NewAgent("Mgr", "g", "", &llmStub{responses: []string{"Final Answer: done"}})
	crew.ManagerAgent = mgr

	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	for _, ag := range []*Agent{a, b, mgr} {
		if !hasToolNamed(ag.Tools, recallMemoryToolName) {
			t.Errorf("%s missing recall_memory", ag.Role)
		}
		if !hasToolNamed(ag.Tools, rememberToolName) {
			t.Errorf("%s missing remember", ag.Role)
		}
	}
	n := len(a.Tools)
	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if len(a.Tools) != n {
		t.Fatalf("tools duplicated: %d -> %d", n, len(a.Tools))
	}
}

func TestCrew_EnableMemoryTools_DefaultOff(t *testing.T) {
	llm := &llmStub{responses: []string{"Final Answer: done"}}
	a := NewAgent("A", "g", "", llm)
	task := NewTask("say hi", "hi", a)
	crew := NewCrew([]*Agent{a}, []*Task{task})
	crew.Memory = true
	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if hasToolNamed(a.Tools, recallMemoryToolName) || hasToolNamed(a.Tools, rememberToolName) {
		t.Fatal("Memory=true must not auto-attach tools")
	}
}

func TestCrew_EnableMemoryTools_SkipsExisting(t *testing.T) {
	llm := &llmStub{responses: []string{"Final Answer: done"}}
	a := NewAgent("A", "g", "", llm)
	existing := NewTool(recallMemoryToolName, "custom", func(context.Context, string) (string, error) {
		return "custom", nil
	})
	a.WithTools(existing)
	task := NewTask("hi", "hi", a)
	crew := NewCrew([]*Agent{a}, []*Task{task})
	crew.Memory = true
	crew.EnableMemoryTools = true
	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if a.Tools[0] != existing {
		t.Fatal("existing recall_memory replaced")
	}
	if !hasToolNamed(a.Tools, rememberToolName) {
		t.Fatal("remember not attached alongside existing recall")
	}
	nRecall := 0
	for _, tl := range a.Tools {
		if tl.Name() == recallMemoryToolName {
			nRecall++
		}
	}
	if nRecall != 1 {
		t.Fatalf("recall copies = %d", nRecall)
	}
}

func TestKickoff_RememberThenRecallViaReAct(t *testing.T) {
	llm := &llmStub{responses: []string{
		"Thought: store it\nAction: remember\nAction Input: Acme Q1 revenue grew 12%\n",
		"Thought: look it up\nAction: recall_memory\nAction Input: revenue\n",
		"Thought: done\nFinal Answer: recalled 12%",
	}}
	a := NewAgent("Analyst", "g", "", llm)
	task := NewTask("remember then recall", "ok", a)
	crew := NewCrew([]*Agent{a}, []*Task{task})
	crew.Memory = true
	crew.EnableMemoryTools = true
	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Final, "12%") {
		t.Errorf("final = %q", out.Final)
	}
	hits, err := crew.MemorySnapshot().Query(context.Background(), MemoryQuery{Text: "Acme", Limit: 8})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("remember did not persist")
	}
}

func TestRemember_UsesTaskNameFromWarningSink(t *testing.T) {
	store := NewMemory()
	crew := NewCrew(nil, nil)
	crew.MemoryStore = store
	task := NewTask("desc", "out", nil)
	task.Name = "q1"
	ctx := ContextWithWarningSink(context.Background(), task)
	if _, err := NewRememberTool(crew).Call(ctx, "note"); err != nil {
		t.Fatal(err)
	}
	hits, _ := store.Query(context.Background(), MemoryQuery{Limit: 8})
	if len(hits) != 1 || hits[0].Task != "q1" {
		t.Fatalf("task name = %+v", hits)
	}
}

func TestRecallMemorySchema_ValidJSON(t *testing.T) {
	var obj map[string]any
	if err := json.Unmarshal(recallMemorySchema, &obj); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(rememberSchema, &obj); err != nil {
		t.Fatal(err)
	}
}

func TestAttachMemoryTools_NilCrew(t *testing.T) {
	var c *Crew
	c.attachMemoryTools()
}

func TestHasToolNamed(t *testing.T) {
	if hasToolNamed(nil, "x") {
		t.Fatal("nil")
	}
	if hasToolNamed([]Tool{nil}, "x") {
		t.Fatal("nil tool")
	}
}

func TestRemember_JSONContent(t *testing.T) {
	store := NewMemory()
	crew := NewCrew(nil, nil)
	crew.MemoryStore = store
	out, err := NewRememberTool(crew).Call(context.Background(), `{"content":"json note"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "Remembered id=") {
		t.Fatalf("out = %q", out)
	}
	hits, _ := store.Query(context.Background(), MemoryQuery{Text: "json", Limit: 4})
	if len(hits) != 1 {
		t.Fatalf("hits = %+v", hits)
	}
}

func TestMemoryTools_InputCap(t *testing.T) {
	store := NewMemory()
	crew := NewCrew(nil, nil)
	crew.MemoryStore = store
	big := strings.Repeat("x", MaxToolArgsBytes+1)
	out, err := NewRememberTool(crew).Call(context.Background(), big)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "size limit") {
		t.Errorf("remember cap = %q", out)
	}
	out, err = NewRecallMemoryTool(crew).Call(context.Background(), big)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "size limit") {
		t.Errorf("recall cap = %q", out)
	}
}

func TestRecall_QueryErrorIsRedacted(t *testing.T) {
	crew := NewCrew(nil, nil)
	crew.MemoryStore = errStore{msg: "query boom sk-abcdefghijklmnopqrstuvwxyz012345"}
	out, err := NewRecallMemoryTool(crew).Call(context.Background(), "q")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "query failed") {
		t.Errorf("query err = %q", out)
	}
	if strings.Contains(out, "sk-abcdefghijklmnopqrstuvwxyz012345") {
		t.Errorf("secret leaked in observation: %q", out)
	}
}

func TestRemember_PutErrorIsRedacted(t *testing.T) {
	crew := NewCrew(nil, nil)
	crew.MemoryStore = errStore{msg: "put boom sk-abcdefghijklmnopqrstuvwxyz012345"}
	out, err := NewRememberTool(crew).Call(context.Background(), "note")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "remember failed") {
		t.Errorf("put err = %q", out)
	}
	if strings.Contains(out, "sk-abcdefghijklmnopqrstuvwxyz012345") {
		t.Errorf("secret leaked: %q", out)
	}
}

func TestMemoryToolStorePrefersKickoffStore(t *testing.T) {
	external := NewMemory()
	internal := NewMemory()
	_, _ = internal.Put(context.Background(), MemoryEntry{Content: "from-kickoff-store"})
	crew := NewCrew(nil, nil)
	crew.MemoryStore = external
	crew.store = internal
	out, err := NewRecallMemoryTool(crew).Call(context.Background(), "kickoff")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "from-kickoff-store") {
		t.Errorf("should query resolved store: %q", out)
	}
}

func TestMemoryToolPolicyUsesResolved(t *testing.T) {
	p := NewMemoryPolicy()
	p.DefaultLimit = 1
	store := NewMemory()
	ctx := context.Background()
	_, _ = store.Put(ctx, MemoryEntry{Content: "one"})
	_, _ = store.Put(ctx, MemoryEntry{Content: "two"})
	crew := NewCrew(nil, nil)
	crew.MemoryStore = store
	crew.policy = p
	out, err := NewRecallMemoryTool(crew).Call(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	// Latest-first, limit 1 → only "two".
	if strings.Contains(out, "one") {
		t.Errorf("limit 1 should drop older: %q", out)
	}
	if !strings.Contains(out, "two") {
		t.Errorf("latest missing: %q", out)
	}
}

func TestMemoryToolLogFallback(t *testing.T) {
	crew := NewCrew(nil, nil)
	if crew.memoryToolLog() == nil {
		t.Fatal("nil logger")
	}
	crew.logger = slog.Default()
	if crew.memoryToolLog() != crew.logger {
		t.Fatal("injected logger unused")
	}
}

func TestAttachMemoryTools_NilAgentSkipped(t *testing.T) {
	a := NewAgent("A", "g", "", &llmStub{responses: []string{"Final Answer: done"}})
	crew := NewCrew([]*Agent{nil, a}, []*Task{NewTask("hi", "hi", a)})
	crew.Memory = true
	crew.EnableMemoryTools = true
	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if !hasToolNamed(a.Tools, recallMemoryToolName) {
		t.Fatal("A missing tools after nil peer")
	}
}

func TestRemember_EmptyIDFallback(t *testing.T) {
	crew := NewCrew(nil, nil)
	crew.MemoryStore = emptyIDStore{}
	out, err := NewRememberTool(crew).Call(context.Background(), "note")
	if err != nil {
		t.Fatal(err)
	}
	if out != "Remembered id=(assigned)" {
		t.Errorf("empty id = %q", out)
	}
}

func TestEmbedLockedSerializesMemoryTools(t *testing.T) {
	var inside atomic.Int32
	var peak atomic.Int32
	embed := func(_ context.Context, texts []string) ([][]float32, error) {
		cur := inside.Add(1)
		for {
			p := peak.Load()
			if cur <= p || peak.CompareAndSwap(p, cur) {
				break
			}
		}
		inside.Add(-1)
		out := make([][]float32, len(texts))
		for i := range texts {
			out[i] = []float32{1, 0}
		}
		return out, nil
	}
	store := NewMemory()
	crew := NewCrew(nil, nil)
	crew.MemoryStore = store
	crew.Embed = embed
	p := NewMemoryPolicy()
	p.AutoEmbed = true
	crew.MemoryPolicy = p

	done := make(chan struct{})
	go func() {
		_, _ = NewRememberTool(crew).Call(context.Background(), "alpha")
		close(done)
	}()
	_, _ = NewRecallMemoryTool(crew).Call(context.Background(), "alpha")
	<-done
	if peak.Load() > 1 {
		t.Errorf("embed peak = %d, want 1", peak.Load())
	}
}

func TestWithAsyncAll_NilCrew(t *testing.T) {
	var c *Crew
	if c.WithAsyncAll() != nil {
		t.Fatal("nil receiver")
	}
}

func TestRecall_EmptyQueryNoEmbed(t *testing.T) {
	store := NewMemory()
	_, _ = store.Put(context.Background(), MemoryEntry{Content: "kept"})
	var calls atomic.Int32
	crew := NewCrew(nil, nil)
	crew.MemoryStore = store
	crew.Embed = func(context.Context, []string) ([][]float32, error) {
		calls.Add(1)
		return [][]float32{{1}}, nil
	}
	out, err := NewRecallMemoryTool(crew).Call(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatal("empty query must not embed")
	}
	if !strings.Contains(out, "kept") {
		t.Errorf("latest = %q", out)
	}
}
