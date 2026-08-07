package crewai

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// --- Unit tests ---

func TestNewFact(t *testing.T) {
	f := NewFact("Company X is active", "Receita Federal",
		"https://api.receita.gov.br/v1/cnpj/00000000000100",
		[]byte(`{"cnpj":"00000000000100"}`))
	if f.Claim != "Company X is active" {
		t.Errorf("Claim = %q", f.Claim)
	}
	if f.SourceOrg != "Receita Federal" {
		t.Errorf("SourceOrg = %q", f.SourceOrg)
	}
	if f.SourceURL == "" {
		t.Error("SourceURL should not be empty")
	}
	if f.PayloadHash == "" {
		t.Error("PayloadHash should not be empty")
	}
	if f.CollectedAt.IsZero() {
		t.Error("CollectedAt should not be zero")
	}
}

func TestNewFact_DifferentPayloads(t *testing.T) {
	f1 := NewFact("same claim", "org", "url", []byte("payload1"))
	f2 := NewFact("same claim", "org", "url", []byte("payload2"))
	if f1.PayloadHash == f2.PayloadHash {
		t.Error("different payloads should have different hashes")
	}
}

func TestAllFactsProvenanced_AllFilled(t *testing.T) {
	facts := []Fact{
		NewFact("claim1", "org1", "url1", []byte("p1")),
		NewFact("claim2", "org2", "url2", []byte("p2")),
	}
	if err := AllFactsProvenanced(facts); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestAllFactsProvenanced_MissingURL(t *testing.T) {
	facts := []Fact{
		{Claim: "fact without url", SourceOrg: "org", PayloadHash: "abc123"},
	}
	err := AllFactsProvenanced(facts)
	if err == nil {
		t.Fatal("expected error for missing source_url")
	}
	if !strings.Contains(err.Error(), "missing source_url") {
		t.Errorf("error should mention source_url: %v", err)
	}
	if !strings.Contains(err.Error(), "fact without url") {
		t.Errorf("error should mention the claim: %v", err)
	}
}

func TestAllFactsProvenanced_MissingHash(t *testing.T) {
	facts := []Fact{
		{Claim: "fact without hash", SourceOrg: "org", SourceURL: "url"},
	}
	err := AllFactsProvenanced(facts)
	if err == nil {
		t.Fatal("expected error for missing payload_hash")
	}
	if !strings.Contains(err.Error(), "missing payload_hash") {
		t.Errorf("error should mention payload_hash: %v", err)
	}
}

func TestAllFactsProvenanced_Empty(t *testing.T) {
	if err := AllFactsProvenanced(nil); err != nil {
		t.Errorf("expected nil for nil slice, got %v", err)
	}
	if err := AllFactsProvenanced([]Fact{}); err != nil {
		t.Errorf("expected nil for empty slice, got %v", err)
	}
}

func TestDedupFacts_NoDuplicates(t *testing.T) {
	f1 := NewFact("claim1", "org", "url", []byte("p1"))
	f2 := NewFact("claim2", "org", "url", []byte("p2"))
	result := dedupFacts(nil, []Fact{f1, f2})
	if len(result) != 2 {
		t.Errorf("expected 2 facts, got %d", len(result))
	}
}

func TestDedupFacts_Duplicates(t *testing.T) {
	f1 := NewFact("claim1", "org", "url", []byte("same-payload"))
	f2 := NewFact("claim2 (dup)", "org", "url", []byte("same-payload"))
	result := dedupFacts(nil, []Fact{f1, f2})
	if len(result) != 1 {
		t.Errorf("expected 1 fact (dedup), got %d", len(result))
	}
	if result[0].Claim != "claim1" {
		t.Errorf("should keep first occurrence, got %q", result[0].Claim)
	}
}

func TestDedupFacts_EmptyHash(t *testing.T) {
	f1 := NewFact("claim1", "org", "url", []byte("p1"))
	f2 := Fact{Claim: "no hash", SourceURL: "url", PayloadHash: ""}
	result := dedupFacts(nil, []Fact{f1, f2})
	if len(result) != 1 {
		t.Errorf("expected 1 fact (empty hash skipped), got %d", len(result))
	}
}

func TestDedupFacts_AppendToExisting(t *testing.T) {
	f1 := NewFact("claim1", "org", "url", []byte("p1"))
	f2 := NewFact("claim2", "org", "url", []byte("p2"))
	f3 := NewFact("claim3 (dup of p1)", "org", "url", []byte("p1"))
	result := dedupFacts([]Fact{f1}, []Fact{f2, f3})
	if len(result) != 2 {
		t.Errorf("expected 2 facts, got %d", len(result))
	}
}

// --- FactSourceTool tests ---

func TestFactSourceTool_Call(t *testing.T) {
	tool := NewFactSourceTool(
		"connector",
		"A connector tool",
		func(_ context.Context, _ string) (string, error) {
			return "result", nil
		},
		func(_ context.Context, output string) []Fact {
			return []Fact{NewFact(output, "org", "url", []byte(output))}
		},
	)
	out, err := tool.Call(context.Background(), "input")
	if err != nil {
		t.Fatalf("Call error: %v", err)
	}
	if out != "result" {
		t.Errorf("output = %q", out)
	}
	facts := tool.Facts()
	if len(facts) != 1 {
		t.Fatalf("expected 1 fact, got %d", len(facts))
	}
	if facts[0].Claim != "result" {
		t.Errorf("fact claim = %q", facts[0].Claim)
	}
}

func TestFactSourceTool_CallError(t *testing.T) {
	tool := NewFactSourceTool(
		"connector",
		"A connector tool",
		func(_ context.Context, _ string) (string, error) {
			return "", errors.New("connection failed")
		},
		func(_ context.Context, _ string) []Fact {
			return []Fact{{Claim: "should not be called"}}
		},
	)
	_, err := tool.Call(context.Background(), "input")
	if err == nil {
		t.Fatal("expected error")
	}
	if facts := tool.Facts(); facts != nil {
		t.Errorf("expected nil facts on error, got %v", facts)
	}
}

func TestFactSourceTool_NilFactsFn(t *testing.T) {
	tool := NewFactSourceTool(
		"connector",
		"A connector tool",
		func(_ context.Context, _ string) (string, error) { return "ok", nil },
		nil,
	)
	if _, err := tool.Call(context.Background(), "input"); err != nil {
		t.Fatalf("Call error: %v", err)
	}
	if facts := tool.Facts(); facts != nil {
		t.Errorf("expected nil facts with nil factsFn, got %v", facts)
	}
}

func TestFactSourceTool_ImplementsInterfaces(t *testing.T) {
	tool := NewFactSourceTool("t", "d", nil, nil)
	var _ Tool = tool
	var _ FactSource = tool
}

// --- Integration tests ---

func TestFactSource_ToolAttachesFacts(t *testing.T) {
	// Mock LLM that triggers a tool call then a final answer.
	llm := &llmStub{responses: []string{
		"Thought: I need to look up the CNPJ.\nAction: cnpj_lookup\nAction Input: 00000000000100\n",
		"Final Answer: The company is active.",
	}}
	agent := NewAgent("Auditor", "", "", llm)

	tool := NewFactSourceTool(
		"cnpj_lookup",
		"Looks up CNPJ status.",
		func(_ context.Context, _ string) (string, error) {
			return "Empresa X is ATIVA", nil
		},
		func(_ context.Context, output string) []Fact {
			return []Fact{NewFact(output, "Receita Federal",
				"https://api.receita.gov.br/v1/cnpj/00000000000100",
				[]byte("ativa"))}
		},
	)
	agent.WithTools(tool)

	task := NewTask("Verify CNPJ.", "Status.", agent)
	crew := &Crew{Agents: []*Agent{agent}, Tasks: []*Task{task}}

	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("Kickoff error: %v", err)
	}
	if out.Final != "The company is active." {
		t.Errorf("Final = %q", out.Final)
	}
	if len(out.TasksOutput[0].Facts) != 1 {
		t.Fatalf("expected 1 fact in TaskOutput, got %d", len(out.TasksOutput[0].Facts))
	}
	if len(out.Facts) != 1 {
		t.Fatalf("expected 1 fact in CrewOutput, got %d", len(out.Facts))
	}
	fact := out.Facts[0]
	if fact.SourceOrg != "Receita Federal" {
		t.Errorf("SourceOrg = %q", fact.SourceOrg)
	}
	if fact.PayloadHash == "" {
		t.Error("PayloadHash should not be empty")
	}
}

func TestFactSource_PlainToolNoFacts(t *testing.T) {
	llm := &llmStub{responses: []string{
		"Thought: I need to calculate.\nAction: calc\nAction Input: 2+2\n",
		"Final Answer: The result is 4.",
	}}
	agent := NewAgent("Agent", "", "", llm)
	plainTool := NewTool("calc", "Calculator",
		func(_ context.Context, _ string) (string, error) { return "4", nil },
	)
	agent.WithTools(plainTool)

	task := NewTask("Calculate.", "Number.", agent)
	crew := &Crew{Agents: []*Agent{agent}, Tasks: []*Task{task}}

	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("Kickoff error: %v", err)
	}
	if len(out.Facts) != 0 {
		t.Errorf("expected 0 facts from plain tool, got %d", len(out.Facts))
	}
	if len(out.TasksOutput[0].Facts) != 0 {
		t.Errorf("expected 0 facts in TaskOutput, got %d", len(out.TasksOutput[0].Facts))
	}
}

func TestFactSource_DedupAcrossCalls(t *testing.T) {
	llm := &llmStub{responses: []string{
		"Action: lookup\nAction Input: X\n",
		"Action: lookup\nAction Input: X\n",
		"Final Answer: Done.",
	}}
	agent := NewAgent("Agent", "", "", llm)

	payload := []byte("same-payload")
	tool := NewFactSourceTool(
		"lookup",
		"Lookup tool",
		func(_ context.Context, _ string) (string, error) { return "result", nil },
		func(_ context.Context, _ string) []Fact {
			return []Fact{NewFact("same fact", "org", "url", payload)}
		},
	)
	agent.WithTools(tool)

	task := NewTask("Lookup.", "Result.", agent)
	crew := &Crew{Agents: []*Agent{agent}, Tasks: []*Task{task}}

	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("Kickoff error: %v", err)
	}
	if len(out.Facts) != 1 {
		t.Errorf("expected 1 fact (deduped), got %d", len(out.Facts))
	}
}

func TestFactSource_LLMOutputNotFact(t *testing.T) {
	// No FactSource tool; LLM produces text containing "fact".
	llm := &llmStub{responses: []string{
		"Final Answer: This is a fact I remember.",
	}}
	agent := NewAgent("Agent", "", "", llm)
	task := NewTask("Say something.", "Text.", agent)
	crew := &Crew{Agents: []*Agent{agent}, Tasks: []*Task{task}}

	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("Kickoff error: %v", err)
	}
	if out.Final != "This is a fact I remember." {
		t.Errorf("Final = %q", out.Final)
	}
	if len(out.Facts) != 0 {
		t.Errorf("LLM output should NOT produce facts, got %d", len(out.Facts))
	}
}

func TestFactSource_ProvenanceGuardrail(t *testing.T) {
	llm := &llmStub{responses: []string{
		"Action: lookup\nAction Input: X\n",
		"Final Answer: Done.",
	}}
	agent := NewAgent("Agent", "", "", llm)

	tool := NewFactSourceTool(
		"lookup",
		"Lookup tool",
		func(_ context.Context, _ string) (string, error) { return "result", nil },
		func(_ context.Context, _ string) []Fact {
			// Fact missing SourceURL.
			return []Fact{{Claim: "unprovenanced fact", SourceOrg: "org", PayloadHash: "abc"}}
		},
	)
	agent.WithTools(tool)

	task := NewTask("Lookup.", "Result.", agent)
	crew := &Crew{Agents: []*Agent{agent}, Tasks: []*Task{task}}
	crew.Guardrails = []Guardrail{
		func(_ context.Context, out *CrewOutput) error {
			return AllFactsProvenanced(out.Facts)
		},
	}

	_, err := crew.Kickoff(context.Background(), nil)
	if !errors.Is(err, ErrBlockedByGuardrail) {
		t.Fatalf("expected ErrBlockedByGuardrail, got %v", err)
	}
	if !strings.Contains(err.Error(), "missing source_url") {
		t.Errorf("error should mention missing source_url: %v", err)
	}
}

func TestFactSource_ProvenanceGuardrailPass(t *testing.T) {
	llm := &llmStub{responses: []string{
		"Action: lookup\nAction Input: X\n",
		"Final Answer: Done.",
	}}
	agent := NewAgent("Agent", "", "", llm)

	tool := NewFactSourceTool(
		"lookup",
		"Lookup tool",
		func(_ context.Context, _ string) (string, error) { return "result", nil },
		func(_ context.Context, _ string) []Fact {
			return []Fact{NewFact("provenanced fact", "org", "https://source.gov", []byte("payload"))}
		},
	)
	agent.WithTools(tool)

	task := NewTask("Lookup.", "Result.", agent)
	crew := &Crew{Agents: []*Agent{agent}, Tasks: []*Task{task}}
	crew.Guardrails = []Guardrail{
		func(_ context.Context, out *CrewOutput) error {
			return AllFactsProvenanced(out.Facts)
		},
	}

	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("Kickoff error: %v", err)
	}
	if len(out.Facts) != 1 {
		t.Errorf("expected 1 fact, got %d", len(out.Facts))
	}
}

func TestFactSource_Regression(t *testing.T) {
	// Existing tools (Calculator, WordCount) without FactSource should
	// produce zero facts and behave as before.
	llm := &llmStub{responses: []string{
		"Action: calc\nAction Input: 2+2\n",
		"Final Answer: 4.",
	}}
	agent := NewAgent("Agent", "", "", llm)
	calc := NewTool("calc", "Calculator",
		func(_ context.Context, _ string) (string, error) { return "4", nil },
	)
	agent.WithTools(calc)

	task := NewTask("Calculate.", "Number.", agent)
	crew := &Crew{Agents: []*Agent{agent}, Tasks: []*Task{task}}

	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("Kickoff error: %v", err)
	}
	if out.Final != "4." {
		t.Errorf("Final = %q", out.Final)
	}
	if len(out.Facts) != 0 {
		t.Errorf("expected 0 facts, got %d", len(out.Facts))
	}
}

func TestFactSource_StructuredTask(t *testing.T) {
	schema := mustRaw(t, map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
		"required": []any{"name"},
	})
	llm := &structuredStub{responses: []string{`{"name":"Alice"}`}}
	agent := NewAgent("Agent", "", "", llm)
	task := NewTask("Extract name.", "JSON", agent)
	task.Structured = &StructuredOutput{Schema: schema}

	out, _, err := executeTask(context.Background(), agent, task, "", nopLogger{})
	if err != nil {
		t.Fatalf("executeTask error: %v", err)
	}
	if !strings.Contains(out, "Alice") {
		t.Errorf("output = %q", out)
	}
	// Structured tasks have no tools, so facts should be nil.
}

func TestFactSource_MultipleTasks(t *testing.T) {
	// Two tasks with FactSource tools; facts from both should appear in
	// CrewOutput.Facts, deduplicated.
	payload1 := []byte("payload1")
	payload2 := []byte("payload2")

	llm1 := &llmStub{responses: []string{
		"Action: lookup\nAction Input: A\n",
		"Final Answer: Done A.",
	}}
	llm2 := &llmStub{responses: []string{
		"Action: lookup\nAction Input: B\n",
		"Final Answer: Done B.",
	}}
	a1 := NewAgent("Agent1", "", "", llm1)
	a2 := NewAgent("Agent2", "", "", llm2)

	tool1 := NewFactSourceTool("lookup", "Lookup",
		func(_ context.Context, _ string) (string, error) { return "A", nil },
		func(_ context.Context, _ string) []Fact {
			return []Fact{NewFact("fact A", "org", "url1", payload1)}
		},
	)
	tool2 := NewFactSourceTool("lookup", "Lookup",
		func(_ context.Context, _ string) (string, error) { return "B", nil },
		func(_ context.Context, _ string) []Fact {
			return []Fact{NewFact("fact B", "org", "url2", payload2)}
		},
	)
	a1.WithTools(tool1)
	a2.WithTools(tool2)

	t1 := NewTask("Task A.", "Result.", a1)
	t2 := NewTask("Task B.", "Result.", a2).WithContext(t1)
	crew := &Crew{Agents: []*Agent{a1, a2}, Tasks: []*Task{t1, t2}}

	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("Kickoff error: %v", err)
	}
	if len(out.Facts) != 2 {
		t.Errorf("expected 2 facts across tasks, got %d", len(out.Facts))
	}
}

func TestFactSource_NoTools(t *testing.T) {
	// No tools at all: facts should be nil/empty.
	llm := &llmStub{responses: []string{"Final Answer: Hello."}}
	agent := NewAgent("Agent", "", "", llm)
	task := NewTask("Say hello.", "Greeting.", agent)
	crew := &Crew{Agents: []*Agent{agent}, Tasks: []*Task{task}}

	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("Kickoff error: %v", err)
	}
	if len(out.Facts) != 0 {
		t.Errorf("expected 0 facts, got %d", len(out.Facts))
	}
}

func TestFactSource_ToolError(t *testing.T) {
	// Tool returns error: no facts collected, observation shows error.
	llm := &llmStub{responses: []string{
		"Action: lookup\nAction Input: X\n",
		"Final Answer: Could not look up.",
	}}
	agent := NewAgent("Agent", "", "", llm)

	tool := NewFactSourceTool(
		"lookup",
		"Lookup tool",
		func(_ context.Context, _ string) (string, error) {
			return "", errors.New("service unavailable")
		},
		func(_ context.Context, _ string) []Fact {
			return []Fact{{Claim: "should not be collected on error"}}
		},
	)
	agent.WithTools(tool)

	task := NewTask("Lookup.", "Result.", agent)
	crew := &Crew{Agents: []*Agent{agent}, Tasks: []*Task{task}}

	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("Kickoff error: %v", err)
	}
	if len(out.Facts) != 0 {
		t.Errorf("expected 0 facts on tool error, got %d", len(out.Facts))
	}
}

func TestFactSource_FactsAreNotMutatedByGuardrail(t *testing.T) {
	llm := &llmStub{responses: []string{
		"Action: lookup\nAction Input: X\n",
		"Final Answer: Done.",
	}}
	agent := NewAgent("Agent", "", "", llm)

	tool := NewFactSourceTool(
		"lookup",
		"Lookup tool",
		func(_ context.Context, _ string) (string, error) { return "result", nil },
		func(_ context.Context, _ string) []Fact {
			return []Fact{NewFact("fact", "org", "url", []byte("payload"))}
		},
	)
	agent.WithTools(tool)

	task := NewTask("Lookup.", "Result.", agent)
	crew := &Crew{Agents: []*Agent{agent}, Tasks: []*Task{task}}
	crew.Guardrails = []Guardrail{
		func(_ context.Context, out *CrewOutput) error {
			// Guardrail inspects facts without mutating.
			if len(out.Facts) == 0 {
				return errors.New("no facts")
			}
			return AllFactsProvenanced(out.Facts)
		},
	}

	out, err := crew.Kickoff(context.Background(), nil)
	if err != nil {
		t.Fatalf("Kickoff error: %v", err)
	}
	if len(out.Facts) != 1 {
		t.Errorf("expected 1 fact, got %d", len(out.Facts))
	}
	// Verify the fact is unchanged.
	if out.Facts[0].Claim != "fact" {
		t.Errorf("fact was mutated: %q", out.Facts[0].Claim)
	}
}

func TestFactSource_CollectedAtRecent(t *testing.T) {
	before := time.Now()
	f := NewFact("claim", "org", "url", []byte("payload"))
	after := time.Now()
	if f.CollectedAt.Before(before) || f.CollectedAt.After(after) {
		t.Errorf("CollectedAt %v should be between %v and %v",
			f.CollectedAt, before, after)
	}
}
