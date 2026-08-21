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
	// WebSearchResults is returned by WebSearch when set. If nil and
	// WebSearchHandler is not set, returns ErrWebSearchUnsupported.
	WebSearchResults []crewai.SearchHit
	// WebSearchHandler, when set, takes precedence over WebSearchResults.
	WebSearchHandler func(ctx context.Context, query string, max int) ([]crewai.SearchHit, error)
	// StreamChunks optionally supplies per-CallStream sequences. When the
	// outer slice runs out, CallStream synthesizes chunks by splitting the
	// Call result into fixed-size rune groups (streamChunkRunes).
	StreamChunks [][]crewai.StreamChunk
	// StreamChunkRunes is the rune group size used when StreamChunks is
	// empty (default 4).
	StreamChunkRunes int

	mu             sync.Mutex
	calls          int
	toolCallIndex  int
	webSearchCalls int
	streamIndex    int
	log            [][]crewai.Message
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

// CallStream implements crewai.StreamingLLM.
func (m *LLM) CallStream(ctx context.Context, messages []crewai.Message) <-chan crewai.StreamChunk {
	ch := make(chan crewai.StreamChunk, crewai.DefaultStreamChanBuffer)

	m.mu.Lock()
	idx := m.streamIndex
	m.streamIndex++
	var scripted []crewai.StreamChunk
	if idx < len(m.StreamChunks) {
		scripted = m.StreamChunks[idx]
	}
	m.mu.Unlock()

	go func() {
		defer close(ch)
		if len(scripted) > 0 {
			for _, c := range scripted {
				select {
				case <-ctx.Done():
					return
				case ch <- c:
				}
			}
			return
		}
		// Synthesize from Call (records the call in the log).
		text, err := m.Call(ctx, messages)
		if err != nil {
			select {
			case <-ctx.Done():
			case ch <- crewai.StreamChunk{Err: err}:
			}
			return
		}
		n := m.StreamChunkRunes
		if n <= 0 {
			n = 4
		}
		for _, part := range splitRunes(text, n) {
			select {
			case <-ctx.Done():
				return
			case ch <- crewai.StreamChunk{Delta: part}:
			}
		}
		select {
		case <-ctx.Done():
		case ch <- crewai.StreamChunk{Done: true}:
		}
	}()
	return ch
}

func splitRunes(s string, n int) []string {
	if s == "" || n <= 0 {
		if s == "" {
			return nil
		}
		return []string{s}
	}
	var parts []string
	var b []rune
	for _, r := range s {
		b = append(b, r)
		if len(b) >= n {
			parts = append(parts, string(b))
			b = b[:0]
		}
	}
	if len(b) > 0 {
		parts = append(parts, string(b))
	}
	return parts
}

// WebSearch implements crewai.WebSearcher.
func (m *LLM) WebSearch(ctx context.Context, query string, max int) ([]crewai.SearchHit, error) {
	m.mu.Lock()
	m.webSearchCalls++
	m.mu.Unlock()

	if m.WebSearchHandler != nil {
		return m.WebSearchHandler(ctx, query, max)
	}
	if m.WebSearchResults != nil {
		return m.WebSearchResults, nil
	}
	return nil, crewai.ErrWebSearchUnsupported
}

// WebSearchCalls returns how many times WebSearch was called.
func (m *LLM) WebSearchCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.webSearchCalls
}

// Compile-time checks.
var (
	_ crewai.WebSearcher    = (*LLM)(nil)
	_ crewai.StreamingLLM   = (*LLM)(nil)
	_ crewai.ToolCallingLLM = (*LLM)(nil)
)
