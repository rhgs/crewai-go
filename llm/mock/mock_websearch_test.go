package mock_test

import (
	"context"
	"testing"

	"github.com/rhgs/crewai-go"
	"github.com/rhgs/crewai-go/llm/mock"
)

func TestMock_WebSearch_Results(t *testing.T) {
	m := &mock.LLM{
		WebSearchResults: []crewai.SearchHit{
			{Title: "Go", URL: "https://go.dev", Content: "Go is an open source programming language."},
		},
	}
	hits, err := m.WebSearch(context.Background(), "Go", 5)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %d, want 1", len(hits))
	}
	if hits[0].Title != "Go" {
		t.Errorf("hit[0].Title = %q", hits[0].Title)
	}
}

func TestMock_WebSearch_Handler(t *testing.T) {
	m := &mock.LLM{
		WebSearchHandler: func(_ context.Context, query string, max int) ([]crewai.SearchHit, error) {
			return []crewai.SearchHit{
				{Title: "result for " + query, URL: "https://example.com", Content: "snippet"},
			}, nil
		},
	}
	hits, err := m.WebSearch(context.Background(), "test", 5)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %d, want 1", len(hits))
	}
	if hits[0].Title != "result for test" {
		t.Errorf("hit[0].Title = %q", hits[0].Title)
	}
}

func TestMock_WebSearch_Unsupported(t *testing.T) {
	m := mock.New("test")
	_, err := m.WebSearch(context.Background(), "test", 5)
	if err != crewai.ErrWebSearchUnsupported {
		t.Errorf("error = %v, want ErrWebSearchUnsupported", err)
	}
}

func TestMock_WebSearch_Calls(t *testing.T) {
	m := &mock.LLM{
		WebSearchResults: []crewai.SearchHit{
			{Title: "Go", URL: "https://go.dev", Content: "Go is an open source programming language."},
		},
	}
	// Call WebSearch 3 times.
	_, _ = m.WebSearch(context.Background(), "a", 1)
	_, _ = m.WebSearch(context.Background(), "b", 1)
	_, _ = m.WebSearch(context.Background(), "c", 1)

	// WebSearchCalls should be 3.
	if m.WebSearchCalls() != 3 {
		t.Errorf("WebSearchCalls() = %d, want 3", m.WebSearchCalls())
	}
	// Calls() should NOT be incremented by WebSearch.
	if m.Calls() != 0 {
		t.Errorf("Calls() = %d, want 0 (WebSearch should not increment Calls)", m.Calls())
	}
}
