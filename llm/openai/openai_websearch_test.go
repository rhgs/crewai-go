package openai_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rhgs/crewai-go/llm/openai"
)

func TestOpenAI_WebSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)

		// Verify web_search_options is present, tools is absent.
		if _, ok := req["web_search_options"]; !ok {
			t.Error("request should include web_search_options field")
		}
		if _, ok := req["tools"]; ok {
			t.Error("request should NOT include tools field")
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"choices": [{
				"message": {
					"role": "assistant",
					"content": "Go is an open source programming language.",
					"annotations": [
						{
							"type": "url_citation",
							"url_citation": {
								"url": "https://go.dev",
								"title": "The Go Programming Language",
								"start_index": 0,
								"end_index": 100
							}
						},
						{
							"type": "url_citation",
							"url_citation": {
								"url": "https://en.wikipedia.org/wiki/Go",
								"title": "Go (programming language) - Wikipedia",
								"start_index": 101,
								"end_index": 200
							}
						}
					]
				}
			}]
		}`))
	}))
	defer srv.Close()

	c := openai.New("gpt-4o-search-preview",
		openai.WithAPIKey("test-key"),
		openai.WithBaseURL(srv.URL),
	)
	hits, err := c.WebSearch(context.Background(), "Go programming", 5)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("hits = %d, want 2", len(hits))
	}
	if hits[0].Title != "The Go Programming Language" {
		t.Errorf("hit[0].Title = %q", hits[0].Title)
	}
	if hits[0].URL != "https://go.dev" {
		t.Errorf("hit[0].URL = %q", hits[0].URL)
	}
	if hits[1].Title != "Go (programming language) - Wikipedia" {
		t.Errorf("hit[1].Title = %q", hits[1].Title)
	}
}

func TestOpenAI_WebSearch_NoAnnotations(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"choices": [{
				"message": {
					"role": "assistant",
					"content": "Search results: Go is a programming language."
				}
			}]
		}`))
	}))
	defer srv.Close()

	c := openai.New("gpt-4o-search-preview",
		openai.WithAPIKey("test-key"),
		openai.WithBaseURL(srv.URL),
	)
	hits, err := c.WebSearch(context.Background(), "Go programming", 5)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %d, want 1", len(hits))
	}
	if hits[0].Content != "Search results: Go is a programming language." {
		t.Errorf("hit[0].Content = %q", hits[0].Content)
	}
}

func TestOpenAI_WebSearch_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"internal error","type":"server_error"}}`))
	}))
	defer srv.Close()

	c := openai.New("gpt-4o-search-preview",
		openai.WithAPIKey("test-key"),
		openai.WithBaseURL(srv.URL),
	)
	_, err := c.WebSearch(context.Background(), "test", 5)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestOpenAI_WebSearch_MaxClamped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Return 8 annotations, max should clamp to 5.
		annotations := make([]string, 8)
		for i := range annotations {
			annotations[i] = `{"type":"url_citation","url_citation":{"url":"https://example.com/` + string(rune('a'+i)) + `","title":"Result ` + string(rune('a'+i)) + `","start_index":0,"end_index":0}}`
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"results","annotations":[` + joinJSON(annotations) + `]}}]}`))
	}))
	defer srv.Close()

	c := openai.New("gpt-4o-search-preview",
		openai.WithAPIKey("test-key"),
		openai.WithBaseURL(srv.URL),
	)
	hits, err := c.WebSearch(context.Background(), "test", 0) // max=0 should clamp to 5
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(hits) != 5 {
		t.Errorf("hits = %d, want 5 (clamped)", len(hits))
	}
}

// joinJSON is a helper to join JSON objects with commas.
func joinJSON(items []string) string {
	result := ""
	for i, item := range items {
		if i > 0 {
			result += ","
		}
		result += item
	}
	return result
}
