package crewai

import (
	"context"
	"strings"
	"testing"
)

func TestNewAgent(t *testing.T) {
	llm := &llmStub{responses: []string{"ok"}}
	a := NewAgent("Papel", "Objetivo", "História", llm)
	if a.Role != "Papel" || a.Goal != "Objetivo" || a.Backstory != "História" {
		t.Errorf("campos incorretos: %+v", a)
	}
	if a.LLM != llm {
		t.Error("LLM não atribuído")
	}
}

func TestAgentWithTools(t *testing.T) {
	a := NewAgent("A", "", "", nil)
	tool := NewTool("t", "", nil)
	a.WithTools(tool)
	if len(a.Tools) != 1 || a.Tools[0] != tool {
		t.Error("WithTools não adicionou a ferramenta")
	}
}

func TestAgentExecute(t *testing.T) {
	a := NewAgent("Escritor", "escrever", "", &llmStub{responses: []string{"texto gerado"}})
	task := NewTask("escreva", "", a)
	out, err := a.Execute(context.Background(), task)
	if err != nil {
		t.Fatalf("erro: %v", err)
	}
	if out != "texto gerado" {
		t.Errorf("out = %q", out)
	}
}

func TestBuildSystemPrompt(t *testing.T) {
	a := NewAgent("Pesquisador", "encontrar a verdade", "Você é curioso.", nil)
	s := buildSystemPrompt(a)
	if !strings.Contains(s, "Pesquisador") || !strings.Contains(s, "encontrar a verdade") || !strings.Contains(s, "curioso") {
		t.Errorf("prompt de sistema incompleto: %q", s)
	}
}

func TestBuildToolInstructions(t *testing.T) {
	tools := []Tool{NewTool("busca", "busca na web", nil)}
	s := buildToolInstructions(tools)
	if !strings.Contains(s, "busca") || !strings.Contains(s, "Action:") || !strings.Contains(s, "Final Answer:") {
		t.Errorf("instruções de ferramenta incompletas: %q", s)
	}
	if buildToolInstructions(nil) != "" {
		t.Error("sem ferramentas deveria devolver string vazia")
	}
}
