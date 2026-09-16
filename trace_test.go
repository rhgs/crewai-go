package crewai

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type seqLLM struct {
	i         int
	responses []string
}

func (s *seqLLM) Model() string { return "seq" }
func (s *seqLLM) Call(_ context.Context, _ []Message) (string, error) {
	if s.i >= len(s.responses) {
		return s.responses[len(s.responses)-1], nil
	}
	r := s.responses[s.i]
	s.i++
	return r, nil
}

type traceLLM string

func (t traceLLM) Model() string { return "trace" }
func (t traceLLM) Call(_ context.Context, _ []Message) (string, error) {
	return string(t), nil
}

func TestTraceRecorder_MetadataDefault(t *testing.T) {
	rec := NewTraceRecorder()
	a := NewAgent("Writer", "g", "b", traceLLM("Final Answer: secret-token-abcdefghijklmnopqrstuvwxyz"))
	task := NewTask("draft the copy", "one line", a)
	task.Name = "draft"
	crew := NewCrew([]*Agent{a}, []*Task{task}).WithTracer(rec)
	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Final, "secret-token") {
		t.Fatal("kickoff output")
	}
	got := rec.Records()
	if len(got) != 1 {
		t.Fatalf("n=%d", len(got))
	}
	if got[0].Task != "draft" || got[0].Agent != "Writer" {
		t.Fatalf("%+v", got[0])
	}
	if got[0].Output != "" || got[0].Prompt != nil {
		t.Fatalf("bodies leaked: %+v", got[0])
	}
	if got[0].KickoffID == "" {
		t.Fatal("kickoff id")
	}
}

func TestTraceRecorder_BodiesOptIn(t *testing.T) {
	rec := NewTraceRecorder(WithTraceBodies(true))
	a := NewAgent("W", "", "", traceLLM("Final Answer: sk-abcdefghijklmnopqrstuvwxyz012345"))
	task := NewTask("do it", "", a)
	crew := NewCrew([]*Agent{a}, []*Task{task}).WithTracer(rec)
	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	got := rec.Records()
	if len(got) != 1 || got[0].Output == "" || got[0].Prompt == nil {
		t.Fatalf("%+v", got)
	}
	if strings.Contains(got[0].Output, "sk-abcdefghijklmnopqrstuvwxyz012345") {
		t.Fatalf("unredacted: %q", got[0].Output)
	}
	if !strings.Contains(got[0].Output, "REDACTED") {
		t.Fatalf("want redaction: %q", got[0].Output)
	}
}

func TestTraceRecorder_FilterAndSave(t *testing.T) {
	rec := NewTraceRecorder(WithTraceFilter(func(r TaskTraceRecord) bool {
		return r.Task == "keep"
	}))
	a := NewAgent("A", "", "", traceLLM("Final Answer: ok"))
	keep := NewTask("k", "", a)
	keep.Name = "keep"
	drop := NewTask("d", "", a)
	drop.Name = "drop"
	crew := NewCrew([]*Agent{a}, []*Task{keep, drop}).WithTracer(rec)
	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if n := len(rec.Records()); n != 1 || rec.Records()[0].Task != "keep" {
		t.Fatalf("%v", rec.Records())
	}
	path := filepath.Join(t.TempDir(), "t.jsonl")
	if err := rec.Save(path); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", st.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var line TaskTraceRecord
	if err := json.Unmarshal(bytesTrimLine(data), &line); err != nil {
		t.Fatal(err)
	}
	if line.Task != "keep" {
		t.Fatalf("%+v", line)
	}
}

func bytesTrimLine(b []byte) []byte {
	s := strings.TrimSpace(string(b))
	return []byte(s)
}

func TestTraceRecorder_AsyncFoldOrder(t *testing.T) {
	rec := NewTraceRecorder()
	a := NewAgent("A", "", "", traceLLM("Final Answer: x"))
	t0 := NewTask("first", "", a)
	t0.Name = "first"
	t0.Async = true
	t1 := NewTask("second", "", a)
	t1.Name = "second"
	t1.Async = true
	crew := NewCrew([]*Agent{a}, []*Task{t0, t1}).WithTracer(rec)
	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	got := rec.Records()
	if len(got) != 2 || got[0].Task != "first" || got[1].Task != "second" {
		t.Fatalf("%v", got)
	}
	if got[0].Wave == "" || got[1].Wave == "" {
		t.Fatalf("wave labels %q %q", got[0].Wave, got[1].Wave)
	}
}

func TestTraceRecorder_NilAndReset(t *testing.T) {
	var rec *TraceRecorder
	if rec.Records() != nil {
		t.Fatal("nil records")
	}
	rec.Reset()
	if err := rec.Save("x"); err == nil {
		t.Fatal("nil save")
	}
	r := NewTraceRecorder()
	r.records = []TaskTraceRecord{{Task: "old"}}
	r.Reset()
	if len(r.Records()) != 0 {
		t.Fatal("reset")
	}
}

func TestTraceRecorder_FactsAndToolsMetadata(t *testing.T) {
	rec := NewTraceRecorder()
	tool := NewFactSourceTool("lookup", "looks up", func(_ context.Context, in string) (string, error) {
		return "claim", nil
	}, func(_ context.Context, _ string) []Fact {
		return []Fact{NewFact("claim", "org", "https://example.com", []byte(`{"k":"v"}`))}
	})
	a := NewAgent("A", "", "", &seqLLM{responses: []string{
		"Thought: need lookup\nAction: lookup\nAction Input: q",
		"Final Answer: done",
	}})
	a.WithTools(tool)
	task := NewTask("use tool", "", a)
	crew := NewCrew([]*Agent{a}, []*Task{task}).WithTracer(rec)
	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	got := rec.Records()
	if len(got) != 1 {
		t.Fatalf("%v", got)
	}
	if len(got[0].Facts) == 0 {
		t.Fatal("want facts metadata")
	}
	if got[0].Facts[0].Claim != "" {
		t.Fatalf("claim leaked: %+v", got[0].Facts[0])
	}
	if got[0].Facts[0].SourceOrg != "org" {
		t.Fatalf("%+v", got[0].Facts[0])
	}
}

func TestTraceRecorder_SaveError(t *testing.T) {
	rec := NewTraceRecorder()
	if err := rec.Save(filepath.Join(t.TempDir(), "no", "such", "t.jsonl")); err == nil {
		t.Fatal("save")
	}
}

func TestMetadataToolsAndCaptureError(t *testing.T) {
	got := metadataTools([]ToolTrace{{Tool: "calc", Args: []byte(`{"x":1}`), Output: "2", Failed: false}})
	if len(got) != 1 || got[0].Tool != "calc" || len(got[0].Args) != 0 || got[0].Output != "" {
		t.Fatalf("%+v", got)
	}
	if metadataTools(nil) != nil {
		t.Fatal("nil")
	}
	if metadataFacts(nil) != nil {
		t.Fatal("facts nil")
	}

	rec := NewTraceRecorder()
	a := NewAgent("A", "", "", errLLM{})
	task := NewTask("fail", "", a)
	crew := NewCrew([]*Agent{a}, []*Task{task}).WithTracer(rec)
	if _, err := crew.Kickoff(context.Background(), nil); err == nil {
		t.Fatal("want fail")
	}
	// Failed tasks are captured but not published (fold only publishes success).
	if n := len(rec.Records()); n != 0 {
		t.Fatalf("published %d", n)
	}
}

type errLLM struct{}

func (errLLM) Model() string { return "err" }
func (errLLM) Call(context.Context, []Message) (string, error) {
	return "", errors.New("llm down")
}

func TestTraceRecorder_NoTracer(t *testing.T) {
	a := NewAgent("A", "", "", traceLLM("Final Answer: x"))
	crew := NewCrew([]*Agent{a}, []*Task{NewTask("d", "", a)})
	if _, err := crew.Kickoff(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
}
