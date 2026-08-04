// Package mock fornece uma implementação de crewai.LLM para testes, sem
// nenhuma chamada de rede.
package mock

import (
	"context"
	"sync"

	"github.com/rodolphosa/crewai-go"
)

// LLM é um modelo falso e determinístico. Ele devolve respostas pré-definidas
// (Responses) em sequência ou, se um Handler for fornecido, delega a ele.
type LLM struct {
	// Responses são devolvidas em ordem a cada chamada. Quando a lista se
	// esgota, a última resposta é repetida.
	Responses []string
	// Handler, se definido, tem prioridade sobre Responses e recebe as
	// mensagens da chamada atual.
	Handler func(ctx context.Context, messages []crewai.Message) (string, error)
	// ModelName é o identificador devolvido por Model().
	ModelName string

	mu    sync.Mutex
	calls int
	log   [][]crewai.Message
}

// New cria um mock que devolve as respostas informadas em sequência.
func New(responses ...string) *LLM {
	return &LLM{Responses: responses, ModelName: "mock"}
}

// Call implementa crewai.LLM.
func (m *LLM) Call(ctx context.Context, messages []crewai.Message) (string, error) {
	m.mu.Lock()
	m.calls++
	n := m.calls
	m.log = append(m.log, messages)
	m.mu.Unlock()

	if m.Handler != nil {
		return m.Handler(ctx, messages)
	}
	if len(m.Responses) == 0 {
		return "", nil
	}
	if n-1 < len(m.Responses) {
		return m.Responses[n-1], nil
	}
	return m.Responses[len(m.Responses)-1], nil
}

// Model implementa crewai.LLM.
func (m *LLM) Model() string {
	if m.ModelName == "" {
		return "mock"
	}
	return m.ModelName
}

// Calls devolve quantas vezes o modelo foi chamado.
func (m *LLM) Calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// LastMessages devolve as mensagens da última chamada (nil se nunca chamado).
func (m *LLM) LastMessages() []crewai.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.log) == 0 {
		return nil
	}
	return m.log[len(m.log)-1]
}
