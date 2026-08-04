package crewai

import (
	"context"
	"errors"
	"testing"
)

func TestFunctionTool(t *testing.T) {
	tool := NewTool("eco", "repete a entrada", func(_ context.Context, in string) (string, error) {
		return "eco: " + in, nil
	})

	if tool.Name() != "eco" {
		t.Errorf("Name() = %q, quer %q", tool.Name(), "eco")
	}
	if tool.Description() != "repete a entrada" {
		t.Errorf("Description() = %q", tool.Description())
	}

	out, err := tool.Call(context.Background(), "olá")
	if err != nil {
		t.Fatalf("Call() erro: %v", err)
	}
	if out != "eco: olá" {
		t.Errorf("Call() = %q, quer %q", out, "eco: olá")
	}
}

func TestFunctionToolNilFn(t *testing.T) {
	tool := &FunctionTool{name: "x"}
	if _, err := tool.Call(context.Background(), "a"); err == nil {
		t.Error("esperava erro para função nula")
	}
}

func TestFunctionToolPropagatesError(t *testing.T) {
	sentinel := errors.New("falhou")
	tool := NewTool("f", "", func(_ context.Context, _ string) (string, error) {
		return "", sentinel
	})
	if _, err := tool.Call(context.Background(), ""); !errors.Is(err, sentinel) {
		t.Errorf("erro = %v, quer %v", err, sentinel)
	}
}

func TestFindTool(t *testing.T) {
	a := NewTool("a", "", nil)
	b := NewTool("b", "", nil)
	tools := []Tool{a, b}

	got, ok := findTool(tools, "b")
	if !ok || got != b {
		t.Errorf("findTool(b) = %v, %v", got, ok)
	}
	if _, ok := findTool(tools, "z"); ok {
		t.Error("findTool(z) deveria falhar")
	}
}
