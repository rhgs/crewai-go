package tools_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rhgs/crewai-go/tools"
)

// --- Wikipedia provider with mock server ---

func TestWikipediaSearch_Search(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"query": {
				"search": [
					{"title": "Go (programming language)", "pageid": 12345, "snippet": "Go is a <span class=\"searchmatch\">programming</span> language."},
					{"title": "Rust (programming language)", "pageid": 67890, "snippet": "Rust is a <span class=\"searchmatch\">systems</span> language."}
				]
			}
		}`))
	}))
	defer srv.Close()

	ws := tools.NewWikipediaSearch()
	tools.WithWikipediaBaseURL(srv.URL)(ws)

	results, err := ws.Search(context.Background(), "Go programming", 5)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	if results[0].Title != "Go (programming language)" {
		t.Errorf("results[0].Title = %q", results[0].Title)
	}
	// Verify HTML tags are stripped from snippets.
	if strings.Contains(results[0].Snippet, "<span") {
		t.Errorf("snippet should have tags stripped: %q", results[0].Snippet)
	}
	if results[0].SourceOrg != "wikipedia.org" {
		t.Errorf("results[0].SourceOrg = %q", results[0].SourceOrg)
	}
}

func TestWikipediaSearchWithLanguage_Search(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"query":{"search":[{"title":"Go","pageid":1,"snippet":"Go lang."}]}}`))
	}))
	defer srv.Close()

	ws := tools.NewWikipediaSearchWithLanguage("pt")
	tools.WithWikipediaBaseURL(srv.URL)(ws)

	results, err := ws.Search(context.Background(), "Go", 5)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
}

func TestWikipediaSearch_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ws := tools.NewWikipediaSearch()
	tools.WithWikipediaBaseURL(srv.URL)(ws)

	_, err := ws.Search(context.Background(), "test", 5)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestWikipediaSearch_Name(t *testing.T) {
	ws := tools.NewWikipediaSearch()
	if ws.Name() != "wikipedia" {
		t.Errorf("Name() = %q, want wikipedia", ws.Name())
	}
}

// --- Brave Search provider ---

func TestBraveSearch_Search(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Subscription-Token") != "brave-key" {
			t.Errorf("X-Subscription-Token = %q", r.Header.Get("X-Subscription-Token"))
		}
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("Accept = %q", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"web": {
				"results": [
					{"title": "Go", "url": "https://go.dev", "description": "Go is an open source programming language."},
					{"title": "Wikipedia", "url": "https://en.wikipedia.org/wiki/Go", "description": "Go is a compiled language."}
				]
			}
		}`))
	}))
	defer srv.Close()

	bs := tools.NewBraveSearch("brave-key")
	tools.WithBraveBaseURL(srv.URL)(bs)

	results, err := bs.Search(context.Background(), "Go programming", 5)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	if results[0].Title != "Go" {
		t.Errorf("results[0].Title = %q", results[0].Title)
	}
	if results[0].SourceOrg != "go.dev" {
		t.Errorf("results[0].SourceOrg = %q", results[0].SourceOrg)
	}
}

func TestBraveSearch_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"forbidden"}`))
	}))
	defer srv.Close()

	bs := tools.NewBraveSearch("brave-key")
	tools.WithBraveBaseURL(srv.URL)(bs)

	_, err := bs.Search(context.Background(), "test", 5)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBraveSearch_Name(t *testing.T) {
	bs := tools.NewBraveSearch("key")
	if bs.Name() != "brave" {
		t.Errorf("Name() = %q, want brave", bs.Name())
	}
}

// --- Google Search provider ---

func TestGoogleSearch_Search(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != "google-key" {
			t.Errorf("key = %q", r.URL.Query().Get("key"))
		}
		if r.URL.Query().Get("cx") != "cx-id" {
			t.Errorf("cx = %q", r.URL.Query().Get("cx"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"items": [
				{"title": "Go", "link": "https://go.dev", "snippet": "Go is an open source programming language."},
				{"title": "Wikipedia", "link": "https://en.wikipedia.org/wiki/Go", "snippet": "Go is a compiled language."}
			]
		}`))
	}))
	defer srv.Close()

	gs := tools.NewGoogleSearch("google-key", "cx-id")
	tools.WithGoogleBaseURL(srv.URL)(gs)

	results, err := gs.Search(context.Background(), "Go programming", 5)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	if results[0].Title != "Go" {
		t.Errorf("results[0].Title = %q", results[0].Title)
	}
	if results[0].SourceOrg != "go.dev" {
		t.Errorf("results[0].SourceOrg = %q", results[0].SourceOrg)
	}
}

func TestGoogleSearch_MissingCXID(t *testing.T) {
	gs := tools.NewGoogleSearch("key-only", "")
	_, err := gs.Search(context.Background(), "test", 5)
	if err == nil || !strings.Contains(err.Error(), "GOOGLE_CSE_ID") {
		t.Errorf("expected missing CX ID error, got: %v", err)
	}
}

func TestGoogleSearch_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad request"}}`))
	}))
	defer srv.Close()

	gs := tools.NewGoogleSearch("key", "cx")
	tools.WithGoogleBaseURL(srv.URL)(gs)

	_, err := gs.Search(context.Background(), "test", 5)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGoogleSearch_Name(t *testing.T) {
	gs := tools.NewGoogleSearch("key", "cx")
	if gs.Name() != "google" {
		t.Errorf("Name() = %q, want google", gs.Name())
	}
}

// --- LangSearch provider ---

func TestLangSearch_Search(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer lang-key" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"summary":true`) {
			t.Error("request should include summary: true")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": 200,
			"msg": null,
			"data": {
				"webPages": {
					"value": [
						{"name": "Go", "url": "https://go.dev", "snippet": "Go is a programming language.", "summary": "Go is an open source programming language that makes it simple to build secure, scalable systems."},
						{"name": "Wikipedia", "url": "https://en.wikipedia.org/wiki/Go", "snippet": "Go is a compiled language.", "summary": ""}
					]
				}
			}
		}`))
	}))
	defer srv.Close()

	ls := tools.NewLangSearch("lang-key")
	tools.WithLangSearchBaseURL(srv.URL)(ls)

	results, err := ls.Search(context.Background(), "Go programming", 5)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	// First result should use summary (richer) when available.
	if results[0].Snippet != "Go is an open source programming language that makes it simple to build secure, scalable systems." {
		t.Errorf("results[0].Snippet = %q (should use summary)", results[0].Snippet)
	}
	// Second result should fall back to snippet when summary is empty.
	if results[1].Snippet != "Go is a compiled language." {
		t.Errorf("results[1].Snippet = %q (should use snippet)", results[1].Snippet)
	}
}

func TestLangSearch_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code": 401, "msg": "Invalid API KEY", "data": null}`))
	}))
	defer srv.Close()

	ls := tools.NewLangSearch("lang-key")
	tools.WithLangSearchBaseURL(srv.URL)(ls)

	_, err := ls.Search(context.Background(), "test", 5)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLangSearch_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ls := tools.NewLangSearch("lang-key")
	tools.WithLangSearchBaseURL(srv.URL)(ls)

	_, err := ls.Search(context.Background(), "test", 5)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLangSearch_Name(t *testing.T) {
	ls := tools.NewLangSearch("key")
	if ls.Name() != "langsearch" {
		t.Errorf("Name() = %q, want langsearch", ls.Name())
	}
}

// --- Serpstack provider ---

func TestSerpstack_Search(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("access_key") != "serp-key" {
			t.Errorf("access_key = %q", r.URL.Query().Get("access_key"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success": true,
			"organic_results": [
				{"title": "Go", "url": "https://go.dev", "snippet": "Go is an open source programming language."},
				{"title": "Wikipedia", "url": "https://en.wikipedia.org/wiki/Go", "snippet": "Go is a compiled language."}
			]
		}`))
	}))
	defer srv.Close()

	ss := tools.NewSerpstack("serp-key")
	tools.WithSerpstackBaseURL(srv.URL)(ss)

	results, err := ss.Search(context.Background(), "Go programming", 5)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	if results[0].Title != "Go" {
		t.Errorf("results[0].Title = %q", results[0].Title)
	}
}

func TestSerpstack_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success": false,
			"error": {"code": 101, "type": "invalid_access_key", "info": "Invalid key"}
		}`))
	}))
	defer srv.Close()

	ss := tools.NewSerpstack("serp-key")
	tools.WithSerpstackBaseURL(srv.URL)(ss)

	_, err := ss.Search(context.Background(), "test", 5)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSerpstack_Name(t *testing.T) {
	ss := tools.NewSerpstack("key")
	if ss.Name() != "serpstack" {
		t.Errorf("Name() = %q, want serpstack", ss.Name())
	}
}

// --- DuckDuckGo provider ---

func TestDuckDuckGo_Search(t *testing.T) {
	htmlBody := `<html><body>
		<a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgo.dev%2F&amp;rut=abc">The Go Programming Language</a>
		<a class="result__snippet" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgo.dev%2F&amp;rut=abc">Go is an open source programming language.</a>
		<a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fen.wikipedia.org%2Fwiki%2FGo&amp;rut=def">Go (programming language) - Wikipedia</a>
		<a class="result__snippet" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fen.wikipedia.org%2Fwiki%2FGo&amp;rut=def">Go is a compiled language.</a>
	</body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(htmlBody))
	}))
	defer srv.Close()

	ds := tools.NewDuckDuckGoSearch()
	tools.WithDuckDuckGoBaseURL(srv.URL + "/?q=")(ds)

	results, err := ds.Search(context.Background(), "Go programming", 5)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	if results[0].Title != "The Go Programming Language" {
		t.Errorf("results[0].Title = %q", results[0].Title)
	}
	if results[0].URL != "https://go.dev/" {
		t.Errorf("results[0].URL = %q, want https://go.dev/", results[0].URL)
	}
	if results[0].Snippet != "Go is an open source programming language." {
		t.Errorf("results[0].Snippet = %q", results[0].Snippet)
	}
}

func TestDuckDuckGo_AnomalyDetection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted) // 202
		_, _ = w.Write([]byte(`<html><body>Unfortunately, bots use DuckDuckGo too. anomaly page.</body></html>`))
	}))
	defer srv.Close()

	ds := tools.NewDuckDuckGoSearch()
	tools.WithDuckDuckGoBaseURL(srv.URL + "/?q=")(ds)

	_, err := ds.Search(context.Background(), "test", 5)
	if err == nil {
		t.Fatal("expected anomaly error")
	}
	if !strings.Contains(err.Error(), "anomaly") {
		t.Errorf("error should mention anomaly: %v", err)
	}
}

func TestDuckDuckGo_Name(t *testing.T) {
	ds := tools.NewDuckDuckGoSearch()
	if ds.Name() != "duckduckgo" {
		t.Errorf("Name() = %q, want duckduckgo", ds.Name())
	}
}

// --- WebSearchTool Name/Description ---

func TestWebSearchTool_Name(t *testing.T) {
	tool := tools.NewWebSearch(&mockProvider{})
	if tool.Name() != "web_search" {
		t.Errorf("Name() = %q, want web_search", tool.Name())
	}
}

func TestWebSearchTool_Description(t *testing.T) {
	tool := tools.NewWebSearch(&mockProvider{})
	if tool.Description() == "" {
		t.Error("Description() should not be empty")
	}
}

func TestNewWebSearch_NilProvider(t *testing.T) {
	tool := tools.NewWebSearch(nil)
	if tool == nil {
		t.Fatal("NewWebSearch(nil) returned nil")
	}
	if tool.Name() != "web_search" {
		t.Errorf("Name() = %q", tool.Name())
	}
}

// --- Context cancellation ---

func TestWebSearchTool_ContextCancellation(t *testing.T) {
	provider := &mockProvider{
		delay: 500 * time.Millisecond,
	}
	tool := tools.NewWebSearch(provider, tools.WithSearchTimeout(10*time.Second))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := tool.Call(ctx, "test")
	if err == nil {
		t.Error("expected context cancellation error")
	}
}

// --- Edge case tests for coverage ---

func TestBraveSearch_JSONDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{invalid json`))
	}))
	defer srv.Close()

	bs := tools.NewBraveSearch("brave-key")
	tools.WithBraveBaseURL(srv.URL)(bs)

	_, err := bs.Search(context.Background(), "test", 5)
	if err == nil {
		t.Fatal("expected JSON decode error")
	}
}

func TestBraveSearch_EmptyResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"web":{"results":[]}}`))
	}))
	defer srv.Close()

	bs := tools.NewBraveSearch("brave-key")
	tools.WithBraveBaseURL(srv.URL)(bs)

	results, err := bs.Search(context.Background(), "test", 5)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("results = %d, want 0", len(results))
	}
}

func TestGoogleSearch_JSONDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{broken`))
	}))
	defer srv.Close()

	gs := tools.NewGoogleSearch("key", "cx")
	tools.WithGoogleBaseURL(srv.URL)(gs)

	_, err := gs.Search(context.Background(), "test", 5)
	if err == nil {
		t.Fatal("expected JSON decode error")
	}
}

func TestGoogleSearch_EmptyResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	gs := tools.NewGoogleSearch("key", "cx")
	tools.WithGoogleBaseURL(srv.URL)(gs)

	results, err := gs.Search(context.Background(), "test", 5)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("results = %d, want 0", len(results))
	}
}

func TestLangSearch_JSONDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{bad json`))
	}))
	defer srv.Close()

	ls := tools.NewLangSearch("lang-key")
	tools.WithLangSearchBaseURL(srv.URL)(ls)

	_, err := ls.Search(context.Background(), "test", 5)
	if err == nil {
		t.Fatal("expected JSON decode error")
	}
}

func TestLangSearch_EmptyResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code": 200, "data": {"webPages": {"value": []}}}`))
	}))
	defer srv.Close()

	ls := tools.NewLangSearch("lang-key")
	tools.WithLangSearchBaseURL(srv.URL)(ls)

	results, err := ls.Search(context.Background(), "test", 5)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("results = %d, want 0", len(results))
	}
}

func TestSerpstack_JSONDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{bad json`))
	}))
	defer srv.Close()

	ss := tools.NewSerpstack("serp-key")
	tools.WithSerpstackBaseURL(srv.URL)(ss)

	_, err := ss.Search(context.Background(), "test", 5)
	if err == nil {
		t.Fatal("expected JSON decode error")
	}
}

func TestSerpstack_EmptyResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success": true}`))
	}))
	defer srv.Close()

	ss := tools.NewSerpstack("serp-key")
	tools.WithSerpstackBaseURL(srv.URL)(ss)

	results, err := ss.Search(context.Background(), "test", 5)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("results = %d, want 0", len(results))
	}
}

func TestWikipediaSearch_JSONDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{bad json`))
	}))
	defer srv.Close()

	ws := tools.NewWikipediaSearch()
	tools.WithWikipediaBaseURL(srv.URL)(ws)

	_, err := ws.Search(context.Background(), "test", 5)
	if err == nil {
		t.Fatal("expected JSON decode error")
	}
}

func TestWikipediaSearch_EmptyResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"query":{"search":[]}}`))
	}))
	defer srv.Close()

	ws := tools.NewWikipediaSearch()
	tools.WithWikipediaBaseURL(srv.URL)(ws)

	results, err := ws.Search(context.Background(), "test", 5)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("results = %d, want 0", len(results))
	}
}

func TestDuckDuckGo_EmptyResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body>no results</body></html>`))
	}))
	defer srv.Close()

	ds := tools.NewDuckDuckGoSearch()
	tools.WithDuckDuckGoBaseURL(srv.URL + "/?q=")(ds)

	results, err := ds.Search(context.Background(), "test", 5)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("results = %d, want 0", len(results))
	}
}

func TestDuckDuckGo_PlainURL(t *testing.T) {
	// Test that a URL without uddg= parameter is returned as-is.
	htmlBody := `<a rel="nofollow" class="result__a" href="https://example.com">Example</a>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(htmlBody))
	}))
	defer srv.Close()

	ds := tools.NewDuckDuckGoSearch()
	tools.WithDuckDuckGoBaseURL(srv.URL + "/?q=")(ds)

	results, err := ds.Search(context.Background(), "test", 5)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	if results[0].URL != "https://example.com" {
		t.Errorf("URL = %q, want https://example.com", results[0].URL)
	}
}

func TestWebSearchTool_ProviderError(t *testing.T) {
	provider := &mockProvider{
		err: context.DeadlineExceeded,
	}
	tool := tools.NewWebSearch(provider)
	_, err := tool.Call(context.Background(), "test")
	if err == nil {
		t.Error("expected provider error to propagate")
	}
}
