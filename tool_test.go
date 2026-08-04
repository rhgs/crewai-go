package crewai

import (
	"context"
	"errors"
	"testing"
)

func TestFunctionTool(t *testing.T) {
	tool := NewTool("echo", "repeats the input", func(_ context.Context, in string) (string, error) {
		return "echo: " + in, nil
	})

	if tool.Name() != "echo" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "echo")
	}
	if tool.Description() != "repeats the input" {
		t.Errorf("Description() = %q", tool.Description())
	}

	out, err := tool.Call(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Call() error: %v", err)
	}
	if out != "echo: hi" {
		t.Errorf("Call() = %q, want %q", out, "echo: hi")
	}
}

func TestFunctionToolNilFn(t *testing.T) {
	tool := &FunctionTool{name: "x"}
	if _, err := tool.Call(context.Background(), "a"); err == nil {
		t.Error("expected an error for a nil function")
	}
}

func TestFunctionToolPropagatesError(t *testing.T) {
	sentinel := errors.New("failed")
	tool := NewTool("f", "", func(_ context.Context, _ string) (string, error) {
		return "", sentinel
	})
	if _, err := tool.Call(context.Background(), ""); !errors.Is(err, sentinel) {
		t.Errorf("error = %v, want %v", err, sentinel)
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
		t.Error("findTool(z) should fail")
	}
}
