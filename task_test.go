package crewai

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTaskInterpolate(t *testing.T) {
	task := NewTask("Pesquise sobre {tema} em {ano}", "relatório sobre {tema}", nil)
	task.interpolate(map[string]string{"tema": "IA", "ano": "2026"})

	if task.Description != "Pesquise sobre IA em 2026" {
		t.Errorf("Description = %q", task.Description)
	}
	if task.ExpectedOutput != "relatório sobre IA" {
		t.Errorf("ExpectedOutput = %q", task.ExpectedOutput)
	}
}

func TestTaskContextText(t *testing.T) {
	dep := NewTask("dep", "", nil)
	dep.Name = "Pesquisa"
	_ = dep.setOutput("dados coletados")

	task := NewTask("análise", "", nil).WithContext(dep)
	ctx := task.contextText()

	if !strings.Contains(ctx, "Pesquisa") || !strings.Contains(ctx, "dados coletados") {
		t.Errorf("contextText = %q", ctx)
	}
}

func TestTaskContextTextSkipsEmpty(t *testing.T) {
	dep := NewTask("dep vazia", "", nil) // sem saída
	task := NewTask("t", "", nil).WithContext(dep)
	if got := task.contextText(); got != "" {
		t.Errorf("contextText = %q, quer vazio", got)
	}
}

func TestTaskOutputFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "saida.txt")
	task := NewTask("t", "", nil)
	task.OutputFile = file

	if err := task.setOutput("conteúdo final"); err != nil {
		t.Fatalf("setOutput erro: %v", err)
	}
	if task.Output() != "conteúdo final" {
		t.Errorf("Output() = %q", task.Output())
	}

	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("lendo arquivo: %v", err)
	}
	if string(data) != "conteúdo final" {
		t.Errorf("arquivo = %q", string(data))
	}
}
