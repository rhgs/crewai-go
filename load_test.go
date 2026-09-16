package crewai

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type yamlEchoLLM string

func (f yamlEchoLLM) Model() string { return "yaml-echo" }
func (f yamlEchoLLM) Call(_ context.Context, _ []Message) (string, error) {
	return string(f), nil
}

func TestLoadCrew_HappyBuild(t *testing.T) {
	doc := `{
  "agents": [
    {"name": "writer", "role": "Writer", "goal": "write", "llm": "echo"}
  ],
  "tasks": [
    {"name": "draft", "description": "draft it", "agent": "writer"},
    {"name": "edit", "description": "edit it", "agent": "Writer", "context": ["draft"]}
  ],
  "crew": {"name": "desk", "process": "sequential"}
}`
	cfg, err := LoadCrew(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	crew, err := cfg.Build(WithLLMMap(map[string]LLM{"echo": yamlEchoLLM("Final Answer: ok")}))
	if err != nil {
		t.Fatal(err)
	}
	if crew.Name != "desk" || len(crew.Agents) != 1 || len(crew.Tasks) != 2 {
		t.Fatalf("%s %d %d", crew.Name, len(crew.Agents), len(crew.Tasks))
	}
	if crew.Tasks[1].Context[0] != crew.Tasks[0] {
		t.Fatal("context name")
	}
	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Final, "ok") {
		t.Fatalf("%q", out.Final)
	}
}

func TestLoadCrewFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "crew.json")
	if err := os.WriteFile(p, []byte(`{"agents":[{"role":"A","llm":"echo"}],"tasks":[{"description":"d"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadCrewFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.Build(WithLLMMap(map[string]LLM{"echo": yamlEchoLLM("Final Answer: x")})); err != nil {
		t.Fatal(err)
	}
}

func TestLoadCrew_SizeCap(t *testing.T) {
	big := bytes.Repeat([]byte("a"), MaxCrewConfigBytes+1)
	if _, err := LoadCrew(bytes.NewReader(big)); !errors.Is(err, ErrCrewConfigTooLarge) {
		t.Fatalf("%v", err)
	}
}

func TestLoadCrew_SchemaAndUnknownField(t *testing.T) {
	if _, err := LoadCrew(strings.NewReader(`{}`)); err == nil {
		t.Fatal("required")
	}
	if _, err := LoadCrew(strings.NewReader(`{"agents":[],"tasks":[],"nope":1}`)); err == nil {
		t.Fatal("additional")
	}
	if _, err := LoadCrew(strings.NewReader(`not-json`)); err == nil {
		t.Fatal("json")
	}
	if _, err := LoadCrew(nil); err == nil {
		t.Fatal("nil")
	}
}

func TestBuild_UnknownRefs(t *testing.T) {
	must := func(s string) *CrewConfig {
		c, err := LoadCrew(strings.NewReader(s))
		if err != nil {
			t.Helper()
			t.Fatal(err)
		}
		return c
	}
	if _, err := must(`{"agents":[{"role":"A","llm":"missing"}],"tasks":[{"description":"d"}]}`).Build(); !errors.Is(err, ErrUnknownRef) {
		t.Fatalf("llm %v", err)
	}
	if _, err := must(`{"agents":[{"role":"A","tools":["nope"]}],"tasks":[{"description":"d"}]}`).Build(); !errors.Is(err, ErrUnknownRef) {
		t.Fatalf("tool %v", err)
	}
	if _, err := must(`{"agents":[{"role":"A"}],"tasks":[{"description":"d","agent":"ghost"}]}`).Build(); !errors.Is(err, ErrUnknownRef) {
		t.Fatalf("agent %v", err)
	}
	if _, err := must(`{"agents":[{"role":"A"}],"tasks":[{"description":"d","guardrail":"g"}]}`).Build(); !errors.Is(err, ErrUnknownRef) {
		t.Fatalf("task gr %v", err)
	}
	if _, err := must(`{"agents":[{"role":"A"}],"tasks":[{"description":"d"}],"crew":{"guardrails":["g"]}}`).Build(); !errors.Is(err, ErrUnknownRef) {
		t.Fatalf("crew gr %v", err)
	}
	if _, err := must(`{"agents":[{"role":"A"}],"tasks":[{"description":"d"}],"crew":{"manager_llm":"x"}}`).Build(); !errors.Is(err, ErrUnknownRef) {
		t.Fatalf("mgr llm %v", err)
	}
	if _, err := must(`{"agents":[{"role":"A"}],"tasks":[{"description":"d"}],"crew":{"manager_agent":"x"}}`).Build(); !errors.Is(err, ErrUnknownRef) {
		t.Fatalf("mgr agent %v", err)
	}
}

func TestBuild_ContextIndexAndCycle(t *testing.T) {
	cfg, err := LoadCrew(strings.NewReader(`{
  "agents":[{"role":"A","llm":"echo"}],
  "tasks":[
    {"name":"t0","description":"a"},
    {"name":"t1","description":"b","context":[0]}
  ]
}`))
	if err != nil {
		t.Fatal(err)
	}
	crew, err := cfg.Build(WithLLMMap(map[string]LLM{"echo": yamlEchoLLM("Final Answer: z")}))
	if err != nil {
		t.Fatal(err)
	}
	if crew.Tasks[1].Context[0] != crew.Tasks[0] {
		t.Fatal("index")
	}

	cyc, err := LoadCrew(strings.NewReader(`{
  "agents":[{"role":"A"}],
  "tasks":[
    {"name":"a","description":"a","context":["b"]},
    {"name":"b","description":"b","context":["a"]}
  ]
}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cyc.Build(); !errors.Is(err, ErrTaskDependencyCycle) {
		t.Fatalf("cycle %v", err)
	}
}

func TestBuild_ContextUnknownAndOOB(t *testing.T) {
	cfg, err := LoadCrew(strings.NewReader(`{"agents":[{"role":"A"}],"tasks":[{"description":"d","context":["nope"]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.Build(); !errors.Is(err, ErrUnknownRef) {
		t.Fatalf("%v", err)
	}
	cfg2, err := LoadCrew(strings.NewReader(`{"agents":[{"role":"A"}],"tasks":[{"description":"d","context":[9]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfg2.Build(); !errors.Is(err, ErrUnknownRef) {
		t.Fatalf("oob %v", err)
	}
}

func TestBuild_DuplicatesAndStaged(t *testing.T) {
	dupA, err := LoadCrew(strings.NewReader(`{"agents":[{"role":"A"},{"role":"A"}],"tasks":[{"description":"d"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dupA.Build(); err == nil {
		t.Fatal("dup role")
	}
	dupT, err := LoadCrew(strings.NewReader(`{"agents":[{"role":"A"}],"tasks":[{"name":"t","description":"a"},{"name":"t","description":"b"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dupT.Build(); err == nil {
		t.Fatal("dup task")
	}
	st, err := LoadCrew(strings.NewReader(`{"agents":[{"role":"A"}],"tasks":[{"description":"d"}],"crew":{"process":"staged"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Build(); err == nil {
		t.Fatal("staged")
	}
	bad, err := LoadCrew(strings.NewReader(`{"agents":[{"role":"A"}],"tasks":[{"description":"d"}],"crew":{"process":"nope"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bad.Build(); err == nil {
		t.Fatal("process")
	}
}

func TestBuild_ToolsGuardrailsAndMeta(t *testing.T) {
	tool := NewTool("calc", "c", func(context.Context, string) (string, error) { return "1", nil })
	gr := func(context.Context, *CrewOutput) error { return nil }
	doc := `{
  "agents":[{"name":"a","role":"A","llm":"echo","tools":["calc"],"allow_delegation":true,"max_iterations":3,"tool_mode":"react"}],
  "tasks":[{"description":"d","agent":"a","tools":["calc"],"guardrail":"ok","async":true,"output_file":"out.txt"}],
  "crew":{"verbose":true,"memory":true,"enable_delegation_tool":true,"async_max_workers":2,"async_fail_fast":false,"guardrails":["ok"],"output_dir":"/tmp","manager_agent":"a"}
}`
	cfg, err := LoadCrew(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	crew, err := cfg.Build(
		WithLLMMap(map[string]LLM{"echo": yamlEchoLLM("Final Answer: 1")}),
		WithToolMap(map[string]Tool{"calc": tool}),
		WithGuardrailMap(map[string]Guardrail{"ok": gr}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !crew.Verbose || !crew.Memory || crew.AsyncMaxWorkers != 2 || crew.AsyncFailFast {
		t.Fatalf("%+v", crew)
	}
	if len(crew.Agents[0].Tools) != 1 || !crew.Tasks[0].Async {
		t.Fatal("tools/async")
	}
	if crew.ManagerAgent != crew.Agents[0] {
		t.Fatal("manager")
	}
}

func TestBuild_NilConfigAndContextStringIndex(t *testing.T) {
	if _, err := (*CrewConfig)(nil).Build(); err == nil {
		t.Fatal("nil")
	}
	cfg, err := LoadCrew(strings.NewReader(`{"agents":[{"role":"A"}],"tasks":[{"description":"a"},{"description":"b","context":["0"]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	crew, err := cfg.Build()
	if err != nil {
		t.Fatal(err)
	}
	if crew.Tasks[1].Context[0] != crew.Tasks[0] {
		t.Fatal("string index")
	}
}

func TestContextRef_BadJSON(t *testing.T) {
	var c ContextRef
	if err := c.UnmarshalJSON([]byte(`true`)); err == nil {
		t.Fatal("bool")
	}
	if err := c.UnmarshalJSON([]byte(``)); err == nil {
		t.Fatal("empty")
	}
	if err := c.UnmarshalJSON([]byte(`null`)); err == nil {
		t.Fatal("null")
	}
	if err := c.UnmarshalJSON([]byte(`"ok"`)); err != nil {
		t.Fatal(err)
	}
	if c.Name != "ok" || c.Index != nil {
		t.Fatalf("%+v", c)
	}
}

func TestLoadCrewFile_Missing(t *testing.T) {
	if _, err := LoadCrewFile(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Fatal("missing")
	}
}

func TestBuild_DuplicateAgentNameAndManagerLLM(t *testing.T) {
	dup, err := LoadCrew(strings.NewReader(`{"agents":[{"name":"x","role":"A"},{"name":"x","role":"B"}],"tasks":[{"description":"d"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dup.Build(); err == nil {
		t.Fatal("dup name")
	}
	cfg, err := LoadCrew(strings.NewReader(`{"agents":[{"role":"A"}],"tasks":[{"description":"d"}],"crew":{"process":"hierarchical","manager_llm":"echo"}}`))
	if err != nil {
		t.Fatal(err)
	}
	crew, err := cfg.Build(WithLLMMap(map[string]LLM{"echo": yamlEchoLLM("Final Answer: m")}))
	if err != nil {
		t.Fatal(err)
	}
	if crew.Process != Hierarchical || crew.ManagerLLM == nil {
		t.Fatal("hierarchical")
	}
}

func TestBuild_NegativeContextIndex(t *testing.T) {
	// Schema minimum:0 should reject negative index at load.
	if _, err := LoadCrew(strings.NewReader(`{"agents":[{"role":"A"}],"tasks":[{"description":"d","context":[-1]}]}`)); err == nil {
		t.Fatal("neg")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("read fail") }

func TestLoadCrew_ReadError(t *testing.T) {
	if _, err := LoadCrew(errReader{}); err == nil {
		t.Fatal("read")
	}
}
