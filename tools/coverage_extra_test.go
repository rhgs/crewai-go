package tools_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rhgs/crewai-go/tools"
)

func TestProviderConstructorsDefaultsAndErrors(t *testing.T) {
	if tools.NewBraveSearch("") == nil {
		t.Fatal("NewBraveSearch nil")
	}
	if tools.NewLangSearch("") == nil {
		t.Fatal("NewLangSearch nil")
	}
	if tools.NewSerpstack("") == nil {
		t.Fatal("NewSerpstack nil")
	}
	if tools.NewGoogleSearch("", "") == nil {
		t.Fatal("NewGoogleSearch nil")
	}

	if _, err := tools.NewBraveSearch("").Search(context.Background(), "q", 3); err == nil {
		t.Fatal("expected brave key error")
	}
	if _, err := tools.NewLangSearch("").Search(context.Background(), "q", 3); err == nil {
		t.Fatal("expected langsearch key error")
	}
	if _, err := tools.NewSerpstack("").Search(context.Background(), "q", 3); err == nil {
		t.Fatal("expected serpstack key error")
	}
	if _, err := tools.NewGoogleSearch("", "").Search(context.Background(), "q", 3); err == nil {
		t.Fatal("expected google key error")
	}
}

func TestBraveLangSerpGoogle_HTTPErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("down"))
	}))
	defer srv.Close()

	b := tools.NewBraveSearch("k")
	tools.WithBraveBaseURL(srv.URL)(b)
	if _, err := b.Search(context.Background(), "q", 0); err == nil {
		t.Fatal("expected brave http error")
	}
	l := tools.NewLangSearch("k")
	tools.WithLangSearchBaseURL(srv.URL)(l)
	if _, err := l.Search(context.Background(), "q", 99); err == nil {
		t.Fatal("expected langsearch http error")
	}
	s := tools.NewSerpstack("k")
	tools.WithSerpstackBaseURL(srv.URL)(s)
	if _, err := s.Search(context.Background(), "q", 3); err == nil {
		t.Fatal("expected serpstack http error")
	}
	g := tools.NewGoogleSearch("k", "cx")
	tools.WithGoogleBaseURL(srv.URL)(g)
	if _, err := g.Search(context.Background(), "q", 3); err == nil {
		t.Fatal("expected google http error")
	}
}

func TestDuckDuckGoAndWikipedia_MorePaths(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not-html-results`))
	}))
	defer srv.Close()
	ddg := tools.NewDuckDuckGoSearch()
	tools.WithDuckDuckGoBaseURL(srv.URL)(ddg)
	// No structured results is fine; ensure no panic.
	if _, err := ddg.Search(context.Background(), "q", 0); err != nil {
		// some implementations may error on empty parse; both ok
		t.Logf("ddg search err (ok): %v", err)
	}

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted) // anomaly path
		_, _ = w.Write([]byte(`anomaly captcha`))
	}))
	defer srv2.Close()
	ddg2 := tools.NewDuckDuckGoSearch()
	tools.WithDuckDuckGoBaseURL(srv2.URL)(ddg2)
	if _, err := ddg2.Search(context.Background(), "q", 3); err == nil {
		t.Fatal("expected ddg anomaly error")
	}

	wsrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer wsrv.Close()
	ws := tools.NewWikipediaSearch()
	tools.WithWikipediaBaseURL(wsrv.URL)(ws)
	if _, err := ws.Search(context.Background(), "q", 3); err == nil {
		t.Fatal("expected wikipedia decode error")
	}
}

func TestMiscAndCalculatorEdges(t *testing.T) {
	ct := tools.CurrentTime("")
	out, err := ct.Call(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatal("empty current time")
	}
	if _, err := ct.Call(context.Background(), "ignored"); err != nil {
		t.Fatal(err)
	}
	ct2 := tools.CurrentTime("2006-01-02")
	if _, err := ct2.Call(context.Background(), ""); err != nil {
		t.Fatal(err)
	}

	wc := tools.WordCount()
	out, err = wc.Call(context.Background(), "one two three")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "words=3") {
		t.Fatalf("word count = %q", out)
	}

	calc := tools.Calculator()
	cases := []struct {
		in      string
		wantErr bool
	}{
		{"(1+2)*3", false},
		{"-5+2", false},
		{"2^3", true},
		{"(1+2", true},
		{"", true},
		{"1/0", true},
	}
	for _, tc := range cases {
		_, err := calc.Call(context.Background(), tc.in)
		if tc.wantErr && err == nil {
			t.Errorf("%q: expected error", tc.in)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("%q: unexpected err %v", tc.in, err)
		}
	}
}

func TestWebSearchTool_SSRFAndClampExtra(t *testing.T) {
	p := &stubProvider{results: []tools.SearchResult{
		{Title: "bad", URL: "http://127.0.0.1/secret", Snippet: "nope"},
		{Title: "ok", URL: "https://example.com", Snippet: "yes"},
		{Title: "file", URL: "file:///etc/passwd", Snippet: "nope"},
	}}
	tool := tools.NewWebSearch(p, tools.WithMaxResults(10))
	out, err := tool.Call(context.Background(), "query")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "127.0.0.1") || strings.Contains(out, "file://") {
		t.Fatalf("blocked urls leaked: %s", out)
	}
	if !strings.Contains(out, "example.com") {
		t.Fatalf("missing ok url: %s", out)
	}
	if _, err := tool.Call(context.Background(), "   "); err == nil {
		t.Fatal("expected empty query error")
	}
}

type stubProvider struct {
	results []tools.SearchResult
	err     error
}

func (s *stubProvider) Name() string { return "stub" }
func (s *stubProvider) Search(context.Context, string, int) ([]tools.SearchResult, error) {
	return s.results, s.err
}
