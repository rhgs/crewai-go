package crewai

import (
	"context"
	"strings"
	"testing"
)

func TestParseFinalAnswer(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"Thought: pronto\nFinal Answer: 42", "42", true},
		{"FINAL ANSWER: maiúsculas", "maiúsculas", true},
		{"Final Answer:  espaços  ", "espaços", true},
		{"sem marcador", "", false},
		{"Action: x", "", false},
	}
	for _, c := range cases {
		got, ok := parseFinalAnswer(c.in)
		if ok != c.ok {
			t.Errorf("parseFinalAnswer(%q) ok = %v, quer %v", c.in, ok, c.ok)
			continue
		}
		if ok && strings.TrimSpace(got) != c.want {
			t.Errorf("parseFinalAnswer(%q) = %q, quer %q", c.in, strings.TrimSpace(got), c.want)
		}
	}
}

func TestParseAction(t *testing.T) {
	in := "Thought: preciso calcular\nAction: calculadora\nAction Input: 2 + 2"
	action, input, ok := parseAction(in)
	if !ok {
		t.Fatal("esperava ok=true")
	}
	if action != "calculadora" {
		t.Errorf("action = %q, quer %q", action, "calculadora")
	}
	if input != "2 + 2" {
		t.Errorf("input = %q, quer %q", input, "2 + 2")
	}
}

func TestParseActionStopsAtObservation(t *testing.T) {
	in := "Action: busca\nAction Input: golang\nObservation: algo antigo"
	_, input, ok := parseAction(in)
	if !ok {
		t.Fatal("esperava ok")
	}
	if input != "golang" {
		t.Errorf("input = %q, quer %q", input, "golang")
	}
}

func TestParseActionNoAction(t *testing.T) {
	if _, _, ok := parseAction("apenas texto"); ok {
		t.Error("não deveria encontrar ação")
	}
}

// llmStub é um LLM mínimo local para evitar dependência do pacote mock aqui.
type llmStub struct {
	responses []string
	i         int
}

func (s *llmStub) Call(_ context.Context, _ []Message) (string, error) {
	r := s.responses[s.i]
	if s.i < len(s.responses)-1 {
		s.i++
	}
	return r, nil
}
func (s *llmStub) Model() string { return "stub" }

func TestExecuteTaskNoTools(t *testing.T) {
	agent := NewAgent("Escritor", "escrever", "", &llmStub{responses: []string{"Um poema."}})
	task := NewTask("Escreva um poema", "um poema curto", agent)

	out, err := executeTask(context.Background(), agent, task, "", nopLogger{})
	if err != nil {
		t.Fatalf("erro: %v", err)
	}
	if out != "Um poema." {
		t.Errorf("out = %q", out)
	}
}

func TestExecuteTaskWithTool(t *testing.T) {
	llm := &llmStub{responses: []string{
		"Thought: vou somar\nAction: calc\nAction Input: 2+2",
		"Thought: pronto\nFinal Answer: O resultado é 4.",
	}}
	agent := NewAgent("Matemático", "calcular", "", llm)
	agent.WithTools(NewTool("calc", "soma", func(_ context.Context, in string) (string, error) {
		return "4", nil
	}))
	task := NewTask("Quanto é 2+2?", "o número", agent)

	out, err := executeTask(context.Background(), agent, task, "", nopLogger{})
	if err != nil {
		t.Fatalf("erro: %v", err)
	}
	if !strings.Contains(out, "4") {
		t.Errorf("out = %q, esperava conter 4", out)
	}
}

func TestExecuteTaskUnknownToolRecovers(t *testing.T) {
	llm := &llmStub{responses: []string{
		"Action: inexistente\nAction Input: x",
		"Final Answer: recuperado",
	}}
	agent := NewAgent("A", "", "", llm)
	agent.WithTools(NewTool("real", "", func(_ context.Context, _ string) (string, error) { return "", nil }))
	task := NewTask("faça algo", "", agent)

	out, err := executeTask(context.Background(), agent, task, "", nopLogger{})
	if err != nil {
		t.Fatalf("erro: %v", err)
	}
	if out != "recuperado" {
		t.Errorf("out = %q", out)
	}
}

func TestExecuteTaskNoLLM(t *testing.T) {
	agent := &Agent{Role: "X"}
	task := NewTask("t", "", agent)
	if _, err := executeTask(context.Background(), agent, task, "", nopLogger{}); err != ErrNoLLM {
		t.Errorf("erro = %v, quer %v", err, ErrNoLLM)
	}
}

func TestExecuteTaskMaxIterations(t *testing.T) {
	// Sempre pede uma ferramenta, nunca dá Final Answer.
	llm := &llmStub{responses: []string{"Action: loop\nAction Input: x"}}
	agent := &Agent{Role: "L", LLM: llm, MaxIterations: 3}
	agent.WithTools(NewTool("loop", "", func(_ context.Context, _ string) (string, error) { return "ok", nil }))
	task := NewTask("loop", "", agent)

	_, err := executeTask(context.Background(), agent, task, "", nopLogger{})
	if err == nil || !strings.Contains(err.Error(), "iterações") {
		t.Errorf("esperava erro de máximo de iterações, obteve %v", err)
	}
}

func TestExecuteTaskContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	llm := &llmStub{responses: []string{"Action: t\nAction Input: x"}}
	agent := &Agent{Role: "C", LLM: llm}
	agent.WithTools(NewTool("t", "", func(_ context.Context, _ string) (string, error) { return "", nil }))
	task := NewTask("t", "", agent)

	if _, err := executeTask(ctx, agent, task, "", nopLogger{}); err != context.Canceled {
		t.Errorf("erro = %v, quer context.Canceled", err)
	}
}
