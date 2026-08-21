package crewai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestRedactHandler_GroupAttr(t *testing.T) {
	var buf bytes.Buffer
	h := RedactHandler(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	log := slog.New(h)
	log.Info("msg",
		slog.Group("g",
			slog.String("token", "sk-proj-AbCdEfGhIjKlMnOpQrStUvWxYz0123456789"),
			slog.Int("n", 1),
		),
		slog.Group("empty"),
	)
	got := buf.String()
	if strings.Contains(got, "AbCdEfGhIjKlMn") {
		t.Fatalf("group secret leaked: %s", got)
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("expected redaction: %s", got)
	}
}

func TestRedactError_EmptyAfterRedact(t *testing.T) {
	err := errors.New("\n\n")
	got := redactError(err)
	if got == nil || got.Error() == "" {
		t.Fatalf("got %#v", got)
	}
}

func TestAsFloat_AllBranches(t *testing.T) {
	cases := []struct {
		in   any
		want float64
		ok   bool
	}{
		{float64(1.5), 1.5, true},
		{float32(2), 2, true},
		{int(3), 3, true},
		{int64(4), 4, true},
		{json.Number("5.5"), 5.5, true},
		{json.Number("x"), 0, false},
		{"6.25", 6.25, true},
		{"nope", 0, false},
		{true, 0, false},
	}
	for _, tc := range cases {
		got, ok := asFloat(tc.in)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Fatalf("asFloat(%T %v)=(%v,%v) want (%v,%v)", tc.in, tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestWalkSchemaKeywords_NestedUnsupported(t *testing.T) {
	schema := mustRaw(t, map[string]any{
		"type": "object",
		"properties": map[string]any{
			"a": map[string]any{"$ref": "#/x"},
		},
		"$defs": map[string]any{
			"T": map[string]any{"format": "email"},
		},
		"definitions": map[string]any{
			"U": map[string]any{"if": map[string]any{"type": "string"}},
		},
		"items": map[string]any{"not": map[string]any{"type": "null"}},
		"oneOf": []any{
			map[string]any{"type": "string"},
			map[string]any{"unevaluatedProperties": false},
		},
		"anyOf":                []any{map[string]any{"type": "integer"}},
		"allOf":                []any{map[string]any{"type": "object"}},
		"additionalProperties": map[string]any{"pattern": "^x$"},
	})
	if err := checkSchemaSupported(schema); err == nil {
		t.Fatal("expected unsupported keywords")
	}
	if err := checkSchemaSupported(json.RawMessage(`not-json`)); err == nil {
		t.Fatal("invalid schema JSON")
	}
	arr := mustRaw(t, []any{map[string]any{"unevaluatedItems": false}})
	if err := checkSchemaSupported(arr); err == nil {
		t.Fatal("array with unevaluatedItems should fail")
	}
	// $ref is supported even nested in arrays
	if err := checkSchemaSupported(mustRaw(t, []any{map[string]any{"$ref": "#/y"}})); err != nil {
		t.Fatalf("$ref in array should be supported: %v", err)
	}
}

func TestValidateNumber_ExclusiveBoolForm(t *testing.T) {
	schema := mustRaw(t, map[string]any{
		"type": "number", "minimum": 1, "exclusiveMinimum": true,
		"maximum": 10, "exclusiveMaximum": true,
	})
	if err := validateSchema(mustRaw(t, float64(1)), schema); err == nil {
		t.Fatal("exclusive min bool")
	}
	if err := validateSchema(mustRaw(t, float64(10)), schema); err == nil {
		t.Fatal("exclusive max bool")
	}
	if err := validateSchema(mustRaw(t, float64(5)), schema); err != nil {
		t.Fatal(err)
	}
}

func TestValidateString_PatternTooLong(t *testing.T) {
	longPat := strings.Repeat("a", MaxSchemaPatternLen+1)
	schema := mustRaw(t, map[string]any{"type": "string", "pattern": longPat})
	if err := validateSchema(mustRaw(t, "a"), schema); err == nil {
		t.Fatal("expected pattern length error")
	}
}

func TestCheckType_MultiTypeArray(t *testing.T) {
	schema := mustRaw(t, map[string]any{"type": []any{"string", "integer"}})
	if err := validateSchema(mustRaw(t, "hi"), schema); err != nil {
		t.Fatal(err)
	}
	if err := validateSchema(mustRaw(t, float64(3)), schema); err != nil {
		t.Fatal(err)
	}
	if err := validateSchema(mustRaw(t, true), schema); err == nil {
		t.Fatal("bool should fail multi-type")
	}
}

type gatherNativeStub struct {
	responses []*ToolCallResponse
	errs      []error
	idx       int
}

func (s *gatherNativeStub) Call(context.Context, []Message) (string, error) {
	return "", errors.New("Call should not be used in native gather")
}
func (s *gatherNativeStub) Model() string { return "gn-stub" }
func (s *gatherNativeStub) CallWithTools(ctx context.Context, _ []Message, _ []ToolSpec) (*ToolCallResponse, error) {
	i := s.idx
	if s.idx < len(s.responses)-1 {
		s.idx++
	}
	if i < len(s.errs) && s.errs[i] != nil {
		return nil, s.errs[i]
	}
	if i >= len(s.responses) {
		return &ToolCallResponse{Content: "done"}, nil
	}
	return s.responses[i], nil
}

type nativeGatherHybrid struct {
	toolResponses []*ToolCallResponse
	callResponses []string
	ti, ci        int
}

func (h *nativeGatherHybrid) Model() string { return "hybrid" }
func (h *nativeGatherHybrid) Call(_ context.Context, _ []Message) (string, error) {
	if h.ci >= len(h.callResponses) {
		return h.callResponses[len(h.callResponses)-1], nil
	}
	s := h.callResponses[h.ci]
	h.ci++
	return s, nil
}
func (h *nativeGatherHybrid) CallWithTools(_ context.Context, _ []Message, _ []ToolSpec) (*ToolCallResponse, error) {
	if h.ti >= len(h.toolResponses) {
		return &ToolCallResponse{Content: "done"}, nil
	}
	r := h.toolResponses[h.ti]
	h.ti++
	return r, nil
}

func TestExecuteStructured_AllowToolsNativeGather(t *testing.T) {
	ft := NewFactSourceTool(
		"lookup", "d",
		func(context.Context, string) (string, error) { return "value=7", nil },
		func(_ context.Context, out string) []Fact {
			return []Fact{NewFact("seven", "org", "http://example.test", []byte(out))}
		},
	)
	hybrid := &nativeGatherHybrid{
		toolResponses: []*ToolCallResponse{
			{ToolCalls: []ToolCall{{
				ID:       "1",
				Function: ToolCallFunction{Name: "lookup", Arguments: json.RawMessage(`{"q":"x"}`)},
			}}},
			{Content: "found seven"},
		},
		callResponses: []string{`{"name":"Ann","age":22}`},
	}
	agent := NewAgent("E", "g", "b", hybrid)
	agent.ToolMode = ToolModeNative
	agent.Tools = []Tool{ft}
	task := NewTask("extract", "JSON", agent)
	task.Structured = &StructuredOutput{Schema: personSchema(t), AllowTools: true}

	out, facts, err := executeStructured(context.Background(), agent, task, "ctx", testLogger())
	if err != nil {
		t.Fatalf("%v", err)
	}
	if !strings.Contains(out, "Ann") {
		t.Fatalf("out=%s", out)
	}
	if len(facts) != 1 {
		t.Fatalf("facts=%d", len(facts))
	}
}

func TestGatherNative_UnsupportedAndMissingToolAndError(t *testing.T) {
	agent := NewAgent("E", "g", "b", &fixedLLM{out: "x"})
	agent.ToolMode = ToolModeNative
	agent.Tools = []Tool{NewTool("t", "d", func(context.Context, string) (string, error) { return "o", nil })}
	task := NewTask("d", "e", agent)
	_, _, _, err := gatherNative(context.Background(), agent, task, "", agent.Tools, testLogger())
	if !errors.Is(err, ErrNativeToolsUnsupported) {
		t.Fatalf("err=%v", err)
	}

	stub := &gatherNativeStub{responses: []*ToolCallResponse{
		{ToolCalls: []ToolCall{{ID: "1", Function: ToolCallFunction{Name: "missing", Arguments: json.RawMessage(`{}`)}}}},
		{ToolCalls: []ToolCall{{ID: "2", Function: ToolCallFunction{Name: "boom", Arguments: json.RawMessage(`{}`)}}}},
		{ToolCalls: []ToolCall{{ID: "3", Function: ToolCallFunction{Name: "boom", Arguments: json.RawMessage(`{}`)}}}},
	}}
	boom := NewTool("boom", "d", func(context.Context, string) (string, error) {
		return "", errors.New("nope")
	})
	agent2 := NewAgent("E", "g", "b", stub)
	agent2.ToolMode = ToolModeNative
	agent2.MaxIterations = 2
	agent2.Tools = []Tool{boom}
	task2 := NewTask("d", "e", agent2)
	tr, _, exhausted, err := gatherNative(context.Background(), agent2, task2, "", agent2.Tools, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if !exhausted {
		t.Fatal("expected exhaust")
	}
	if tr == "" {
		t.Fatal("empty transcript")
	}

	stubErr := &gatherNativeStub{
		responses: []*ToolCallResponse{{Content: "x"}},
		errs:      []error{errors.New("llm down")},
	}
	agent3 := NewAgent("E", "g", "b", stubErr)
	agent3.Tools = []Tool{boom}
	_, _, _, err = gatherNative(context.Background(), agent3, NewTask("d", "e", agent3), "", agent3.Tools, testLogger())
	if err == nil || !strings.Contains(err.Error(), "llm down") {
		t.Fatalf("err=%v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, _, err = gatherNative(ctx, agent2, task2, "", agent2.Tools, testLogger())
	if err == nil {
		t.Fatal("expected ctx err")
	}
}

func TestGatherForStructured_NoTools(t *testing.T) {
	agent := NewAgent("E", "g", "b", &fixedLLM{out: "x"})
	task := NewTask("d", "e", agent)
	tr, facts, ex, err := gatherForStructured(context.Background(), agent, task, "", testLogger())
	if err != nil || tr != "" || facts != nil || ex {
		t.Fatalf("tr=%q facts=%v ex=%v err=%v", tr, facts, ex, err)
	}
}

func TestTruncateForGather_Long(t *testing.T) {
	s := strings.Repeat("x", 600)
	got := truncateForGather(s)
	if len(got) <= 512 || !strings.HasSuffix(got, "…") {
		t.Fatalf("len=%d", len(got))
	}
	if truncateForGather("short") != "short" {
		t.Fatal("short")
	}
}

func TestDelegationHelpers_NilAndEmpty(t *testing.T) {
	ctx := ContextWithAgentRole(context.Background(), "")
	if agentRoleFromCtx(ctx) != "" {
		t.Fatal("empty role")
	}
	if agentRoleFromCtx(nil) != "" {
		t.Fatal("nil ctx role")
	}
	if agentRoleFromCtx(context.Background()) != "" {
		t.Fatal("missing role")
	}
	if delegationDepth(nil) != 0 || delegationDepth(context.Background()) != 0 {
		t.Fatal("depth default")
	}
	if delegationStack(nil) != nil || delegationStack(context.Background()) != nil {
		t.Fatal("stack default")
	}

	a := NewAgent("X", "g", "b", &fixedLLM{out: "Final Answer: x"})
	if findRosterAgent([]*Agent{nil, a}, "x") != a {
		t.Fatal("case fold find")
	}
	if findRosterAgent([]*Agent{nil}, "X") != nil {
		t.Fatal("nil only")
	}
	roles := rosterRoles([]*Agent{nil, a, NewAgent("", "g", "b", nil)})
	if len(roles) != 1 || roles[0] != "X" {
		t.Fatalf("%v", roles)
	}

	emptyLLM := &fixedLLM{out: "Final Answer:"}
	target := NewAgent("T", "g", "b", emptyLLM)
	target.AllowDelegation = true
	tool := NewDelegationTool(sliceRoster{target})
	ctx = ContextWithAgentRole(context.Background(), "Caller")
	out, err := tool.Call(ctx, `{"coworker":"T","request":"q"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "empty answer") {
		t.Fatalf("out=%q", out)
	}

	var c *Crew
	if c.PeerAgents() != nil {
		t.Fatal("nil crew PeerAgents")
	}
	c = &Crew{}
	c.attachDelegationTools()
	c.ManagerAgent = NewAgent("M", "g", "b", &fixedLLM{out: "Final Answer: m"})
	c.Agents = []*Agent{nil, NewAgent("A", "g", "b", &fixedLLM{out: "Final Answer: a"})}
	c.Agents[1].Tools = []Tool{NewDelegationTool(c)}
	c.attachDelegationTools()
	if !hasDelegationTool(c.ManagerAgent.Tools) {
		t.Fatal("manager should get tool")
	}
	n := 0
	for _, tl := range c.Agents[1].Tools {
		if tl.Name() == delegationToolName {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("dup tools %d", n)
	}
	if len(c.PeerAgents()) != 2 {
		t.Fatal("PeerAgents len")
	}
}

func TestDelegationTool_EmptyFields(t *testing.T) {
	tool := NewDelegationTool(sliceRoster{})
	out, _ := tool.Call(context.Background(), `{"coworker":"","request":""}`)
	if !strings.Contains(out, "non-empty") {
		t.Fatalf("%q", out)
	}
	out, _ = tool.Call(context.Background(), "")
	if !strings.Contains(out, "empty delegation") {
		t.Fatalf("%q", out)
	}
}

func TestEvalSymlinksExisting_RootWalk(t *testing.T) {
	dir := t.TempDir()
	p := dir + "/no/such/file.txt"
	got, err := evalSymlinksExisting(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "file.txt") {
		t.Fatalf("got=%q", got)
	}
}

func TestValidateAndCanonicalize_Invalid(t *testing.T) {
	_, err := validateAndCanonicalize("not-json", mustRaw(t, map[string]any{"type": "string"}))
	if err == nil {
		t.Fatal("expected invalid json")
	}
}

func TestGatherReact_MissingToolAndCtx(t *testing.T) {
	llm := &allowToolsSeq{responses: []string{
		"Action: nope\nAction Input: x",
		"Final Answer: done",
	}}
	agent := NewAgent("E", "g", "b", llm)
	agent.Tools = []Tool{NewTool("other", "d", func(context.Context, string) (string, error) { return "y", nil })}
	task := NewTask("d", "e", agent)
	tr, _, ex, err := gatherReact(context.Background(), agent, task, "", agent.Tools, testLogger())
	if err != nil || ex {
		t.Fatalf("err=%v ex=%v", err, ex)
	}
	if !strings.Contains(tr, "Gather summary") {
		t.Fatalf("tr=%q", tr)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, _, err = gatherReact(ctx, agent, task, "", agent.Tools, testLogger())
	if err == nil {
		t.Fatal("ctx")
	}
}
