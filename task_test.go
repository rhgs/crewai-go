package crewai

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTaskInterpolate(t *testing.T) {
	task := NewTask("Research {topic} in {year}", "report on {topic}", nil)
	task.interpolate(map[string]string{"topic": "AI", "year": "2026"})

	if task.Description != "Research AI in 2026" {
		t.Errorf("Description = %q", task.Description)
	}
	if task.ExpectedOutput != "report on AI" {
		t.Errorf("ExpectedOutput = %q", task.ExpectedOutput)
	}
}

func TestTaskContextText(t *testing.T) {
	dep := NewTask("dep", "", nil)
	dep.Name = "Research"
	_ = dep.setOutput("collected data")

	task := NewTask("analysis", "", nil).WithContext(dep)
	ctx := task.contextText()

	if !strings.Contains(ctx, "Research") || !strings.Contains(ctx, "collected data") {
		t.Errorf("contextText = %q", ctx)
	}
}

func TestTaskContextTextSkipsEmpty(t *testing.T) {
	dep := NewTask("empty dep", "", nil) // no output
	task := NewTask("t", "", nil).WithContext(dep)
	if got := task.contextText(); got != "" {
		t.Errorf("contextText = %q, want empty", got)
	}
}

func TestTaskOutputFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "output.txt")
	task := NewTask("t", "", nil)
	task.OutputFile = file

	if err := task.setOutput("final content"); err != nil {
		t.Fatalf("setOutput error: %v", err)
	}
	if task.Output() != "final content" {
		t.Errorf("Output() = %q", task.Output())
	}

	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("reading file: %v", err)
	}
	if string(data) != "final content" {
		t.Errorf("file = %q", string(data))
	}
}
