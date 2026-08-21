package crewai_test

import (
	"context"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rhgs/crewai-go"
	"github.com/rhgs/crewai-go/llm/mock"
)

func TestNewMemoryPolicyDefaults(t *testing.T) {
	p := crewai.NewMemoryPolicy()
	if !p.AutoSave || !p.InjectWhenEmptyContext {
		t.Errorf("defaults AutoSave=%v InjectWhenEmpty=%v", p.AutoSave, p.InjectWhenEmptyContext)
	}
	if p.DefaultLimit != crewai.DefaultMemoryQueryLimit {
		t.Errorf("DefaultLimit = %d, want %d", p.DefaultLimit, crewai.DefaultMemoryQueryLimit)
	}
	if p.DefaultMaxChars != crewai.DefaultMemoryMaxChars {
		t.Errorf("DefaultMaxChars = %d, want %d", p.DefaultMaxChars, crewai.DefaultMemoryMaxChars)
	}
}

func TestMemoryBoolStillWorksSerial(t *testing.T) {
	// G4 / D-M1: Memory=true still saves and MemorySnapshot works.
	llm := mock.New("first", "second")
	a := crewai.NewAgent("A", "", "", llm)
	t1 := crewai.NewTask("t1", "", a)
	t1.Name = "t1"
	t2 := crewai.NewTask("t2", "", a)

	crew := crewai.NewCrew([]*crewai.Agent{a}, []*crewai.Task{t1, t2})
	crew.Memory = true
	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatalf("error: %v", err)
	}
	mem := crew.MemorySnapshot()
	if mem == nil {
		t.Fatal("memory nil")
	}
	if len(mem.Records()) != 2 {
		t.Errorf("expected 2 records, got %d", len(mem.Records()))
	}
}

func TestMemoryCommitOrderMatchesDeclaration(t *testing.T) {
	// D-M7 / G9: slow-first / fast-second parallel tasks → committed Memory
	// sequence matches declaration index, not finish time.
	llm := &mock.LLM{Handler: func(_ context.Context, msgs []crewai.Message) (string, error) {
		for _, m := range msgs {
			if strings.Contains(m.Content, "SLOW") {
				time.Sleep(80 * time.Millisecond)
				return "slow-out", nil
			}
			if strings.Contains(m.Content, "FAST") {
				return "fast-out", nil
			}
		}
		return "?", nil
	}}
	a := crewai.NewAgent("A", "", "", llm)
	t1 := crewai.NewTask("SLOW", "", a)
	t1.Name = "slow"
	t2 := crewai.NewTask("FAST", "", a)
	t2.Name = "fast"

	crew := crewai.NewCrew([]*crewai.Agent{a}, nil)
	crew.Process = crewai.Staged
	crew.Stages = []crewai.Stage{
		{Name: "s1", Tasks: []*crewai.Task{t1, t2}},
	}
	crew.Memory = true

	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatalf("error: %v", err)
	}
	recs := crew.MemorySnapshot().Records()
	if len(recs) != 2 {
		t.Fatalf("records = %d, want 2", len(recs))
	}
	if recs[0].Content != "slow-out" || recs[1].Content != "fast-out" {
		t.Errorf("commit order = [%q %q], want [slow-out fast-out] (declaration, not finish)",
			recs[0].Content, recs[1].Content)
	}
}

func TestMemoryNextWaveCannotSeeInFlightSibling(t *testing.T) {
	// Wave 0: two Async siblings. Wave 1: a dependent that would inject
	// memory. The dependent must NOT see uncommitted mid-wave state; after
	// the barrier it sees BOTH siblings in declaration order.
	var thirdPrompt string
	llm := &mock.LLM{Handler: func(_ context.Context, msgs []crewai.Message) (string, error) {
		var all string
		for _, m := range msgs {
			all += m.Content
		}
		if strings.Contains(all, "SLOW") {
			time.Sleep(60 * time.Millisecond)
			return "slow-out", nil
		}
		if strings.Contains(all, "FAST") {
			return "fast-out", nil
		}
		// third task: no Context, so inject kicks in
		thirdPrompt = all
		return "third-out", nil
	}}
	a := crewai.NewAgent("A", "", "", llm)
	t1 := crewai.NewTask("SLOW", "", a).WithAsync()
	t1.Name = "slow"
	t2 := crewai.NewTask("FAST", "", a).WithAsync()
	t2.Name = "fast"
	t3 := crewai.NewTask("recall", "", a) // no Context → inject committed memory

	crew := crewai.NewCrew([]*crewai.Agent{a}, []*crewai.Task{t1, t2, t3})
	crew.Memory = true

	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatalf("error: %v", err)
	}
	// After wave 0 barrier both siblings are committed; t3 must see both,
	// and the block must list slow before fast (declaration commit order
	// rendered oldest→newest by formatMemoryHits).
	if !strings.Contains(thirdPrompt, "slow-out") || !strings.Contains(thirdPrompt, "fast-out") {
		t.Errorf("next-wave inject missing committed siblings; prompt = %q", thirdPrompt)
	}
	slowIdx := strings.Index(thirdPrompt, "slow-out")
	fastIdx := strings.Index(thirdPrompt, "fast-out")
	if slowIdx < 0 || fastIdx < 0 || slowIdx > fastIdx {
		t.Errorf("committed memory order in prompt should be slow then fast; prompt = %q", thirdPrompt)
	}
}

func TestMemoryInjectRespectsEmptyContextPolicy(t *testing.T) {
	// Default InjectWhenEmptyContext=true: a task WITH Context does not get
	// the memory dump (D-M3 default A).
	var secondPrompt string
	llm := &mock.LLM{Handler: func(_ context.Context, msgs []crewai.Message) (string, error) {
		var all string
		for _, m := range msgs {
			all += m.Content
		}
		if strings.Contains(all, "first task") {
			return "MEMORY_MARKER_SHOULD_NOT_LEAK_VIA_CONTEXT_PATH", nil
		}
		secondPrompt = all
		return "ok", nil
	}}
	a := crewai.NewAgent("A", "", "", llm)
	t1 := crewai.NewTask("first task", "", a)
	t1.Name = "t1"
	t2 := crewai.NewTask("second task", "", a).WithContext(t1)

	crew := crewai.NewCrew([]*crewai.Agent{a}, []*crewai.Task{t1, t2})
	crew.Memory = true
	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatalf("error: %v", err)
	}
	// t2 has Context, so default policy must NOT inject the memory block
	// (it already receives t1 via WithContext). The marker is t1's output,
	// which IS expected via Context — but the "## Memory (recalled)" header
	// must be absent.
	if strings.Contains(secondPrompt, "## Memory (recalled)") {
		t.Errorf("default policy injected memory into a task with Context: %q", secondPrompt)
	}
}

func TestMemoryInjectWhenPolicyAllowsWithContext(t *testing.T) {
	var secondPrompt string
	llm := &mock.LLM{Handler: func(_ context.Context, msgs []crewai.Message) (string, error) {
		var all string
		for _, m := range msgs {
			all += m.Content
		}
		if strings.Contains(all, "first task") {
			return "from-t1", nil
		}
		secondPrompt = all
		return "ok", nil
	}}
	a := crewai.NewAgent("A", "", "", llm)
	t1 := crewai.NewTask("first task", "", a)
	t1.Name = "t1"
	t2 := crewai.NewTask("second task", "", a).WithContext(t1)

	crew := crewai.NewCrew([]*crewai.Agent{a}, []*crewai.Task{t1, t2})
	crew.Memory = true
	p := crewai.NewMemoryPolicy()
	p.InjectWhenEmptyContext = false // always inject
	crew.MemoryPolicy = p

	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatalf("error: %v", err)
	}
	if !strings.Contains(secondPrompt, "## Memory (recalled)") {
		t.Errorf("policy InjectWhenEmptyContext=false should inject; prompt = %q", secondPrompt)
	}
	if !strings.Contains(secondPrompt, "from-t1") {
		t.Errorf("expected both Context and memory; prompt = %q", secondPrompt)
	}
}

func TestMemoryAutoSaveSkippedOnFailure(t *testing.T) {
	// G2: failed tasks do not land in memory.
	llm := &mock.LLM{Handler: func(_ context.Context, msgs []crewai.Message) (string, error) {
		for _, m := range msgs {
			if strings.Contains(m.Content, "FAIL") {
				return "", errBoom
			}
		}
		return "ok", nil
	}}
	a := crewai.NewAgent("A", "", "", llm)
	t1 := crewai.NewTask("ok task", "", a)
	t1.Name = "ok"
	t2 := crewai.NewTask("FAIL task", "", a)
	t2.Name = "fail"

	// Staged optional so Kickoff succeeds and we can inspect memory.
	crew := crewai.NewCrew([]*crewai.Agent{a}, nil)
	crew.Process = crewai.Staged
	crew.Stages = []crewai.Stage{
		{Name: "s1", Tasks: []*crewai.Task{t1, t2}, Optional: true},
	}
	crew.Memory = true
	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatalf("optional stage should not abort: %v", err)
	}
	recs := crew.MemorySnapshot().Records()
	if len(recs) != 1 || recs[0].Content != "ok" {
		t.Errorf("AutoSave must skip failures; records = %+v", recs)
	}
}

func TestStagedAsyncWarnLoggedOncePerKickoff(t *testing.T) {
	// G5: when Task.Async is set under Staged, Kickoff logs the Warn ONCE
	// (not once per stage). Multiple Async tasks across stages → one line.
	var warns atomic.Int32
	llm := mock.New("a", "b", "c", "d")
	a := crewai.NewAgent("A", "", "", llm)
	t1 := crewai.NewTask("1", "", a).WithAsync()
	t2 := crewai.NewTask("2", "", a).WithAsync()

	handler := &countingHandler{warnCount: &warns}
	crew := crewai.NewCrew([]*crewai.Agent{a}, nil)
	crew.Process = crewai.Staged
	crew.Stages = []crewai.Stage{
		{Name: "s1", Tasks: []*crewai.Task{t1}},
		{Name: "s2", Tasks: []*crewai.Task{t2}},
	}
	crew.WithLogger(slog.New(handler))
	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatalf("error: %v", err)
	}
	if got := warns.Load(); got != 1 {
		t.Errorf("Warn count = %d, want exactly 1 (once per Kickoff)", got)
	}
}

// countingHandler counts Warn-level records.
type countingHandler struct {
	warnCount *atomic.Int32
}

func (h *countingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *countingHandler) Handle(_ context.Context, r slog.Record) error {
	if r.Level == slog.LevelWarn {
		h.warnCount.Add(1)
	}
	return nil
}
func (h *countingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *countingHandler) WithGroup(string) slog.Handler      { return h }

func TestMemorySnapshotScopedWithExternalMemory(t *testing.T) {
	// External *Memory + Crew.Name + Memory=true: MemorySnapshot().Records()
	// must see this Kickoff's entries even though they are scoped to
	// Crew.Name (G3). Without a scope-tolerant Records(), the snapshot would
	// be empty while the store holds data — a silent observability lie.
	store := crewai.NewMemory()
	llm := mock.New("scoped")
	a := crewai.NewAgent("A", "", "", llm)
	task := crewai.NewTask("t", "", a)
	task.Name = "t"

	crew := crewai.NewCrew([]*crewai.Agent{a}, []*crewai.Task{task})
	crew.Name = "scoped-crew"
	crew.Memory = true // AND external store
	crew.MemoryStore = store

	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if got := len(crew.MemorySnapshot().Records()); got != 1 {
		t.Errorf("MemorySnapshot().Records() = %d, want 1 (scoped entries visible)", got)
	}
}

// errBoom is a package-level sentinel for failure tests.
var errBoom = errString("boom")

type errString string

func (e errString) Error() string { return string(e) }

func TestMemoryScopeDefaultsToCrewName(t *testing.T) {
	// G3: default Scope = Crew.Name when set.
	store := crewai.NewMemory()
	llm := mock.New("scoped-out")
	a := crewai.NewAgent("A", "", "", llm)
	task := crewai.NewTask("t", "", a)
	task.Name = "t1"

	crew := crewai.NewCrew([]*crewai.Agent{a}, []*crewai.Task{task})
	crew.Name = "crew-alpha"
	crew.MemoryStore = store
	// Memory bool false: store is explicit. Policy defaults still AutoSave.
	crew.MemoryPolicy = crewai.NewMemoryPolicy()

	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatalf("error: %v", err)
	}
	// Default scope query sees nothing; crew-alpha scope sees the entry.
	def, _ := store.Query(context.Background(), crewai.MemoryQuery{Limit: 8})
	if len(def) != 0 {
		t.Errorf("default scope should be empty, got %+v", def)
	}
	got, _ := store.Query(context.Background(), crewai.MemoryQuery{Scope: "crew-alpha", Limit: 8})
	if len(got) != 1 || got[0].Content != "scoped-out" {
		t.Errorf("crew-alpha scope = %+v, want 1 scoped-out", got)
	}
}

func TestAsyncMaxWorkersActuallyCapsConcurrency(t *testing.T) {
	// Regression: AsyncMaxWorkers must bound in-flight workers.
	const workers = 2
	const n = 6
	var inside atomic.Int32
	var peak atomic.Int32
	llm := &mock.LLM{Handler: func(ctx context.Context, _ []crewai.Message) (string, error) {
		cur := inside.Add(1)
		for {
			p := peak.Load()
			if cur <= p || peak.CompareAndSwap(p, cur) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		inside.Add(-1)
		return "ok", nil
	}}
	a := crewai.NewAgent("A", "", "", llm)
	tasks := make([]*crewai.Task, n)
	for i := range tasks {
		tasks[i] = crewai.NewTask("t", "", a).WithAsync()
	}
	crew := crewai.NewCrew([]*crewai.Agent{a}, tasks).WithAsyncMaxWorkers(workers)
	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatalf("error: %v", err)
	}
	if peak.Load() > int32(workers) {
		t.Errorf("peak concurrency = %d, want ≤ %d", peak.Load(), workers)
	}
}

func TestAsyncMixedWaveRunsNonAsyncAfterAsync(t *testing.T) {
	// Regression: a wave with both Async and non-Async ready tasks must
	// run the non-Async ones too (previously dropped).
	var ran atomic.Int32
	llm := &mock.LLM{Handler: func(_ context.Context, msgs []crewai.Message) (string, error) {
		ran.Add(1)
		return "ok", nil
	}}
	a := crewai.NewAgent("A", "", "", llm)
	tAsync := crewai.NewTask("async one", "", a).WithAsync()
	tSync := crewai.NewTask("sync one", "", a) // not Async, same ready wave

	crew := crewai.NewCrew([]*crewai.Agent{a}, []*crewai.Task{tAsync, tSync})
	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if ran.Load() != 2 {
		t.Errorf("ran = %d, want 2 (async + non-async in same ready set)", ran.Load())
	}
	if len(out.TasksOutput) != 2 {
		t.Errorf("outputs = %d, want 2", len(out.TasksOutput))
	}
}

func TestAsyncFailFastFalseSkipsDependentsOnly(t *testing.T) {
	// D-A3: FailFast=false skips dependents of the failed task; unrelated
	// branches continue.
	llm := &mock.LLM{Handler: func(_ context.Context, msgs []crewai.Message) (string, error) {
		var all string
		for _, m := range msgs {
			all += m.Content
		}
		if strings.Contains(all, "FAIL") {
			return "", errBoom
		}
		return "ok", nil
	}}
	a := crewai.NewAgent("A", "", "", llm)
	fail := crewai.NewTask("FAIL root", "", a).WithAsync()
	dep := crewai.NewTask("depends on fail", "", a).WithContext(fail) // should be skipped
	ok := crewai.NewTask("unrelated", "", a).WithAsync()              // should run

	crew := crewai.NewCrew([]*crewai.Agent{a}, []*crewai.Task{fail, dep, ok})
	crew.AsyncFailFast = false
	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("FailFast=false should not abort: %v", err)
	}
	// Only the unrelated success should be in TasksOutput (failed + skipped
	// dependents contribute nothing).
	if len(out.TasksOutput) != 1 || out.TasksOutput[0].Output != "ok" {
		t.Errorf("TasksOutput = %+v, want single ok from unrelated branch", out.TasksOutput)
	}
}
