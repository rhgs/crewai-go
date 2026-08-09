package anthropic_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rhgs/crewai-go/llm/anthropic"
)

func TestAnthropic_WebSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/messages" {
			t.Errorf("path = %q, want /messages", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("x-api-key = %q", r.Header.Get("x-api-key"))
		}

		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)

		// Verify tools field contains web_search_20250305.
		tools, ok := req["tools"].([]any)
		if !ok || len(tools) != 1 {
			t.Fatalf("tools field missing or wrong length")
		}
		tool, ok := tools[0].(map[string]any)
		if !ok {
			t.Fatalf("tool[0] is not a map")
		}
		if tool["type"] != "web_search_20250305" {
			t.Errorf("tool type = %v, want web_search_20250305", tool["type"])
		}

		// Return a response with web_search_tool_result block.
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"content": [
				{
					"type": "web_search_tool_result",
					"tool_use_id": "ws_123",
					"content": [
						{
							"type": "web_search_result",
							"title": "The Go Programming Language",
							"url": "https://go.dev",
							"encrypted_content": "encrypted_data_1"
						},
						{
							"type": "web_search_result",
							"title": "Go (programming language) - Wikipedia",
							"url": "https://en.wikipedia.org/wiki/Go",
							"encrypted_content": "encrypted_data_2"
						}
					]
				},
				{
					"type": "text",
					"text": "Here are the results."
				}
			]
		}`))
	}))
	defer srv.Close()

	c := anthropic.New("claude-sonnet-5",
		anthropic.WithAPIKey("test-key"),
		anthropic.WithBaseURL(srv.URL),
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
	if hits[0].Content != "encrypted_data_1" {
		t.Errorf("hit[0].Content = %q", hits[0].Content)
	}
	if hits[1].Title != "Go (programming language) - Wikipedia" {
		t.Errorf("hit[1].Title = %q", hits[1].Title)
	}
}

func TestAnthropic_WebSearch_NoResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Response with only text blocks, no web_search_tool_result.
		_, _ = w.Write([]byte(`{
			"content": [
				{
					"type": "text",
					"text": "No results found."
				}
			]
		}`))
	}))
	defer srv.Close()

	c := anthropic.New("claude-sonnet-5",
		anthropic.WithAPIKey("test-key"),
		anthropic.WithBaseURL(srv.URL),
	)
	hits, err := c.WebSearch(context.Background(), "obscure query", 5)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("hits = %d, want 0", len(hits))
	}
}

func TestAnthropic_WebSearch_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"type":"server_error","message":"internal error"}}`))
	}))
	defer srv.Close()

	c := anthropic.New("claude-sonnet-5",
		anthropic.WithAPIKey("test-key"),
		anthropic.WithBaseURL(srv.URL),
	)
	_, err := c.WebSearch(context.Background(), "test", 5)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAnthropic_WebSearch_NoKey(t *testing.T) {
	c := anthropic.New("claude-sonnet-5", anthropic.WithAPIKey(""))
	_, err := c.WebSearch(context.Background(), "test", 5)
	if err == nil {
		t.Fatal("expected error for missing API key")
	}
}