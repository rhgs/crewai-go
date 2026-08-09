package tools_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rhgs/crewai-go"
	"github.com/rhgs/crewai-go/tools"
)

// --- Mock provider for WebSearchTool tests ---

type mockProvider struct {
	results []tools.SearchResult
	err     error
	delay   time.Duration
}

func (m *mockProvider) Search(ctx context.Context, _ string, _ int) ([]tools.SearchResult, error) {
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return m.results, m.err
}
func (m *mockProvider) Name() string { return "mock" }

// --- WebSearchTool tests ---

func TestWebSearchTool_Call(t *testing.T) {
	provider := &mockProvider{
		results: []tools.SearchResult{
			{Title: "Go", URL: "https://go.dev", Snippet: "Go is an open source programming language.", SourceOrg: "go.dev"},
			{Title: "Wikipedia", URL: "https://en.wikipedia.org/wiki/Go", Snippet: "Go is a compiled language.", SourceOrg: "en.wikipedia.org"},
			{Title: "Tutorial", URL: "https://go.dev/learn", Snippet: "Learn Go.", SourceOrg: "go.dev"},
		},
	}
	tool := tools.NewWebSearch(provider, tools.WithMaxResults(5))
	out, err := tool.Call(context.Background(), "Go programming")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !strings.Contains(out, "Found 3 results") {
		t.Errorf("output should contain result count: %s", out)
	}
	if !strings.Contains(out, "Go") {
		t.Errorf("output should contain title: %s", out)
	}
	if !strings.Contains(out, "https://go.dev") {
		t.Errorf("output should contain URL: %s", out)
	}
}

func TestWebSearchTool_FactSource(t *testing.T) {
	provider := &mockProvider{
		results: []tools.SearchResult{
			{Title: "Go", URL: "https://go.dev", Snippet: "Go is an open source programming language.", SourceOrg: "go.dev"},
		},
	}
	tool := tools.NewWebSearch(provider)
	_, _ = tool.Call(context.Background(), "Go")
	facts := tool.Facts()
	if len(facts) != 1 {
		t.Fatalf("facts = %d, want 1", len(facts))
	}
	if facts[0].SourceURL != "https://go.dev" {
		t.Errorf("SourceURL = %q", facts[0].SourceURL)
	}
	if facts[0].PayloadHash == "" {
		t.Error("PayloadHash should not be empty")
	}
	if facts[0].SourceOrg != "go.dev" {
		t.Errorf("SourceOrg = %q", facts[0].SourceOrg)
	}
}

func TestWebSearchTool_SSRFBlocked(t *testing.T) {
	provider := &mockProvider{
		results: []tools.SearchResult{
			{Title: "Local", URL: "http://localhost:8080/secret", Snippet: "secret", SourceOrg: "localhost"},
			{Title: "Public", URL: "https://go.dev", Snippet: "Go", SourceOrg: "go.dev"},
		},
	}
	tool := tools.NewWebSearch(provider)
	out, _ := tool.Call(context.Background(), "test")
	if strings.Contains(out, "localhost") {
		t.Errorf("output should not contain localhost URL: %s", out)
	}
	facts := tool.Facts()
	if len(facts) != 1 {
		t.Fatalf("facts = %d, want 1 (localhost filtered)", len(facts))
	}
	if facts[0].SourceURL != "https://go.dev" {
		t.Errorf("SourceURL = %q, want https://go.dev", facts[0].SourceURL)
	}
}

func TestWebSearchTool_SSRFBlocked_0000(t *testing.T) {
	provider := &mockProvider{
		results: []tools.SearchResult{
			{Title: "Unspecified", URL: "http://0.0.0.0:8080", Snippet: "internal", SourceOrg: ""},
			{Title: "Public", URL: "https://go.dev", Snippet: "Go", SourceOrg: "go.dev"},
		},
	}
	tool := tools.NewWebSearch(provider)
	out, _ := tool.Call(context.Background(), "test")
	if strings.Contains(out, "0.0.0.0") {
		t.Errorf("output should not contain 0.0.0.0 URL: %s", out)
	}
}

func TestWebSearchTool_EmptyQuery(t *testing.T) {
	tool := tools.NewWebSearch(&mockProvider{})
	_, err := tool.Call(context.Background(), "  ")
	if err == nil {
		t.Error("expected error for empty query")
	}
}

func TestWebSearchTool_Timeout(t *testing.T) {
	provider := &mockProvider{
		delay: 200 * time.Millisecond,
	}
	tool := tools.NewWebSearch(provider, tools.WithSearchTimeout(50*time.Millisecond))
	_, err := tool.Call(context.Background(), "test")
	if err == nil {
		t.Error("expected timeout error")
	}
}

func TestIsBlockedURL(t *testing.T) {
	provider := &mockProvider{
		results: []tools.SearchResult{
			{Title: "localhost", URL: "http://localhost/admin", Snippet: "", SourceOrg: ""},
			{Title: "127.0.0.1", URL: "http://127.0.0.1/secret", Snippet: "", SourceOrg: ""},
			{Title: "10.x", URL: "http://10.0.0.1/internal", Snippet: "", SourceOrg: ""},
			{Title: "192.168", URL: "http://192.168.1.1/router", Snippet: "", SourceOrg: ""},
			{Title: "0.0.0.0", URL: "http://0.0.0.0:8080", Snippet: "", SourceOrg: ""},
			{Title: "169.254", URL: "http://169.254.169.254/metadata", Snippet: "", SourceOrg: ""},
			{Title: "Public", URL: "https://go.dev", Snippet: "Go", SourceOrg: "go.dev"},
			{Title: "Public2", URL: "https://github.com", Snippet: "GitHub", SourceOrg: "github.com"},
		},
	}
	tool := tools.NewWebSearch(provider)
	out, _ := tool.Call(context.Background(), "test")
	if strings.Contains(out, "localhost") {
		t.Error("should block localhost")
	}
	if strings.Contains(out, "127.0.0.1") {
		t.Error("should block 127.0.0.1")
	}
	if strings.Contains(out, "10.0.0.1") {
		t.Error("should block 10.0.0.1")
	}
	if strings.Contains(out, "192.168.1.1") {
		t.Error("should block 192.168.1.1")
	}
	if strings.Contains(out, "0.0.0.0") {
		t.Error("should block 0.0.0.0")
	}
	if strings.Contains(out, "169.254.169.254") {
		t.Error("should block 169.254.169.254")
	}
	facts := tool.Facts()
	if len(facts) != 2 {
		t.Errorf("facts = %d, want 2 (only public URLs)", len(facts))
	}
}

// Compile-time checks.
var _ crewai.Tool = (*tools.WebSearchTool)(nil)
var _ crewai.FactSource = (*tools.WebSearchTool)(nil)