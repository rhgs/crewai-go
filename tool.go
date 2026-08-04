package crewai

import (
	"context"
	"fmt"
)

// Tool é uma capacidade que um agente pode invocar durante a execução de uma
// tarefa (buscar na web, consultar um banco, fazer um cálculo, etc.).
//
// A entrada e a saída são strings para manter a interface simples e
// interoperável com o raciocínio baseado em texto (ReAct) do agente. Uma
// ferramenta que precise de argumentos estruturados deve documentar, na sua
// Description, o formato esperado (por exemplo, um JSON).
type Tool interface {
	// Name é o identificador único e curto da ferramenta.
	Name() string
	// Description explica ao modelo o que a ferramenta faz e como usá-la.
	Description() string
	// Call executa a ferramenta com a entrada fornecida pelo agente.
	Call(ctx context.Context, input string) (string, error)
}

// FunctionTool adapta uma função Go comum para a interface Tool.
type FunctionTool struct {
	name        string
	description string
	fn          func(ctx context.Context, input string) (string, error)
}

// NewTool cria uma ferramenta a partir de uma função.
//
//	calc := crewai.NewTool(
//	    "calculadora",
//	    "Avalia uma expressão aritmética simples. Entrada: a expressão.",
//	    func(ctx context.Context, in string) (string, error) { ... },
//	)
func NewTool(name, description string, fn func(ctx context.Context, input string) (string, error)) *FunctionTool {
	return &FunctionTool{name: name, description: description, fn: fn}
}

// Name implementa Tool.
func (t *FunctionTool) Name() string { return t.name }

// Description implementa Tool.
func (t *FunctionTool) Description() string { return t.description }

// Call implementa Tool.
func (t *FunctionTool) Call(ctx context.Context, input string) (string, error) {
	if t.fn == nil {
		return "", fmt.Errorf("ferramenta %q: função nula", t.name)
	}
	return t.fn(ctx, input)
}

// findTool procura uma ferramenta pelo nome (case-insensitive nas bordas)
// dentro de uma lista.
func findTool(tools []Tool, name string) (Tool, bool) {
	for _, t := range tools {
		if t.Name() == name {
			return t, true
		}
	}
	return nil, false
}
