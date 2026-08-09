package ollama_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rhgs/crewai-go/llm/ollama"
)

func TestOllama_WebSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/web_search" {
			t.Errorf("path = %q, want /api/web_search", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer cloud-key" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"title":"Go","url":"https://go.dev","content":"Go is an open source programming language."},{"title":"Wikipedia","url":"https://en.wikipedia.org/wiki/Go","content":"Go is a compiled language."}]}`))
	}))
	defer srv.Close()

	c := ollama.NewCloud("test", ollama.WithBaseURL(srv.URL), ollama.WithAPIKey("cloud-key"))
	hits, err := c.WebSearch(context.Background(), "Go programming", 5)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("hits = %d, want 2", len(hits))
	}
	if hits[0].Title != "Go" {
		t.Errorf("hit[0].Title = %q", hits[0].Title)
	}
	if hits[0].URL != "https://go.dev" {
		t.Errorf("hit[0].URL = %q", hits[0].URL)
	}
	if hits[1].Content != "Go is a compiled language." {
		t.Errorf("hit[1].Content = %q", hits[1].Content)
	}
}

func TestOllama_WebSearch_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer srv.Close()

	c := ollama.New("test", ollama.WithBaseURL(srv.URL))
	hits, err := c.WebSearch(context.Background(), "test", 5)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("hits = %d, want 0", len(hits))
	}
}

func TestOllama_WebSearch_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`internal error`))
	}))
	defer srv.Close()

	c := ollama.New("test", ollama.WithBaseURL(srv.URL))
	_, err := c.WebSearch(context.Background(), "test", 5)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestOllama_WebSearch_NoKey(t *testing.T) {
	c := ollama.NewCloud("test", ollama.WithAPIKey(""))
	_, err := c.WebSearch(context.Background(), "test", 5)
	if err == nil {
		t.Fatal("expected error for missing cloud token")
	}
}

func TestOllama_WebSearch_MaxClamped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		// max=0 should clamp to 5, max=100 should clamp to 5
		if req["max_results"].(float64) != 5 {
			t.Errorf("max_results = %v, want 5", req["max_results"])
		}
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer srv.Close()

	c := ollama.New("test", ollama.WithBaseURL(srv.URL))
	// max=0
	_, _ = c.WebSearch(context.Background(), "test", 0)
}

func TestOllama_WebSearch_MaxClamped_High(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		if req["max_results"].(float64) != 5 {
			t.Errorf("max_results = %v, want 5 (clamped from 100)", req["max_results"])
		}
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer srv.Close()

	c := ollama.New("test", ollama.WithBaseURL(srv.URL))
	_, _ = c.WebSearch(context.Background(), "test", 100)
}