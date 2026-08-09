package crewai_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/rhgs/crewai-go"
	"github.com/rhgs/crewai-go/llm/mock"
)

func TestSearchWeb_NativeProvider(t *testing.T) {
	m := &mock.LLM{
		WebSearchResults: []crewai.SearchHit{
			{Title: "Go", URL: "https://go.dev", Content: "Go is an open source programming language."},
			{Title: "Wikipedia", URL: "https://en.wikipedia.org/wiki/Go", Content: "Go is a compiled language."},
			{Title: "Tutorial", URL: "https://go.dev/learn", Content: "Learn Go."},
		},
	}

	hits, err := crewai.SearchWeb(context.Background(), m, "Go programming", 10)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(hits) != 3 {
		t.Fatalf("hits = %d, want 3", len(hits))
	}
	if hits[0].Title != "Go" {
		t.Errorf("hit[0].Title = %q", hits[0].Title)
	}
	if hits[1].URL != "https://en.wikipedia.org/wiki/Go" {
		t.Errorf("hit[1].URL = %q", hits[1].URL)
	}
}

func TestSearchWeb_Unsupported(t *testing.T) {
	// A plain mock without WebSearchResults/Handler does NOT implement
	// WebSearcher properly -- but it does have the method. Use a
	// bare LLM that doesn't implement WebSearcher at all.
	m := &bareLLM{}
	_, err := crewai.SearchWeb(context.Background(), m, "test", 5)
	if err != crewai.ErrWebSearchUnsupported {
		t.Errorf("error = %v, want ErrWebSearchUnsupported", err)
	}
}

// bareLLM implements only crewai.LLM, NOT crewai.WebSearcher.
type bareLLM struct{}

func (b *bareLLM) Call(_ context.Context, _ []crewai.Message) (string, error) { return "", nil }
func (b *bareLLM) Model() string                                                { return "bare" }

func TestSearchWeb_MockUnsupported(t *testing.T) {
	// Mock without WebSearchResults or WebSearchHandler returns
	// ErrWebSearchUnsupported.
	m := mock.New("test")
	_, err := crewai.SearchWeb(context.Background(), m, "test", 5)
	if err != crewai.ErrWebSearchUnsupported {
		t.Errorf("error = %v, want ErrWebSearchUnsupported", err)
	}
}

func TestSearchHitSerialization(t *testing.T) {
	hit := crewai.SearchHit{
		Title:   "Go Programming",
		URL:     "https://go.dev",
		Content: "Go is an open source programming language.",
	}

	data, err := json.Marshal(hit)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var parsed crewai.SearchHit
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if parsed.Title != hit.Title {
		t.Errorf("Title = %q, want %q", parsed.Title, hit.Title)
	}
	if parsed.URL != hit.URL {
		t.Errorf("URL = %q, want %q", parsed.URL, hit.URL)
	}
	if parsed.Content != hit.Content {
		t.Errorf("Content = %q, want %q", parsed.Content, hit.Content)
	}
}

func TestSearchWeb_WithHandler(t *testing.T) {
	m := &mock.LLM{
		WebSearchHandler: func(_ context.Context, query string, max int) ([]crewai.SearchHit, error) {
			return []crewai.SearchHit{
				{Title: query, URL: "https://example.com", Content: "result for " + query},
			}, nil
		},
	}

	hits, err := crewai.SearchWeb(context.Background(), m, "test query", 5)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %d, want 1", len(hits))
	}
	if hits[0].Title != "test query" {
		t.Errorf("hit[0].Title = %q", hits[0].Title)
	}
}