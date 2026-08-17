package xai_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rhgs/crewai-go"
	"github.com/rhgs/crewai-go/llm/xai"
)

func TestXAI_WebSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %q, want /chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer xai-key" {
			t.Errorf("Authorization = %q", got)
		}

		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)

		// Verify web_search_options is present (xAI delegates to OpenAI format).
		if _, ok := req["web_search_options"]; !ok {
			t.Error("request should include web_search_options field")
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"choices": [{
				"message": {
					"role": "assistant",
					"content": "Grok found these results.",
					"annotations": [
						{
							"type": "url_citation",
							"url_citation": {
								"url": "https://go.dev",
								"title": "The Go Programming Language",
								"start_index": 0,
								"end_index": 50
							}
						}
					]
				}
			}]
		}`))
	}))
	defer srv.Close()

	c := xai.New("grok-search",
		xai.WithAPIKey("xai-key"),
		xai.WithBaseURL(srv.URL),
	)
	hits, err := c.WebSearch(context.Background(), "Go programming", 5)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %d, want 1", len(hits))
	}
	if hits[0].Title != "The Go Programming Language" {
		t.Errorf("hit[0].Title = %q", hits[0].Title)
	}
}

// Compile-time check: verify xai.Client implements crewai.WebSearcher.
var _ crewai.WebSearcher = (*xai.Client)(nil)
