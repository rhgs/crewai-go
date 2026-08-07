// Package mock provides a crewai.LLM implementation for tests, with no
// network calls.
package mock

import (
	"context"
	"sync"

	"github.com/rhgs/crewai-go"
)

// LLM is a deterministic fake model. It returns predefined responses (Responses)
// in sequence, or, if a Handler is provided, delegates to it.
type LLM struct {
	// Responses are returned in order on each call. When the list runs out,
	// the last response is repeated.
	Responses []string
	// Handler, when set, takes precedence over Responses and receives the
	// messages from the current call.
	Handler func(ctx context.Context, messages []crewai.Message) (string, error)
	// ModelName is the identifier returned by Model().
	ModelName string
	// ToolCallResponses queues tool call responses to return in order on
	// each CallWithTools invocation. When the list runs out, returns an
	// empty ToolCallResponse (model is done).
	ToolCallResponses []*crewai.ToolCallResponse

	mu            sync.Mutex
	calls         int
	toolCallIndex int
	log           [][]crewai.Message
}

// New creates a mock that returns the given responses in sequence.
func New(responses ...string) *LLM {
	return &LLM{Responses: responses, ModelName: "mock"}
}

// Call implements crewai.LLM.
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

// Model implements crewai.LLM.
func (m *LLM) Model() string {
	if m.ModelName == "" {
		return "mock"
	}
	return m.ModelName
}

// Calls returns how many times the model was called.
func (m *LLM) Calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// LastMessages returns the messages from the last call (nil if never called).
func (m *LLM) LastMessages() []crewai.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.log) == 0 {
		return nil
	}
	return m.log[len(m.log)-1]
}

// CallWithTools implements crewai.ToolCallingLLM.
func (m *LLM) CallWithTools(ctx context.Context, messages []crewai.Message, tools []crewai.ToolSpec) (*crewai.ToolCallResponse, error) {
	m.mu.Lock()
	m.calls++
	m.log = append(m.log, messages)
	idx := m.toolCallIndex
	m.toolCallIndex++
	m.mu.Unlock()

	if idx >= len(m.ToolCallResponses) {
		// Default: no tool calls, return empty content (model is done).
		return &crewai.ToolCallResponse{}, nil
	}
	resp := m.ToolCallResponses[idx]
	return resp, nil
}

// Compile-time check.
var _ crewai.ToolCallingLLM = (*LLM)(nil)
