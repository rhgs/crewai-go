package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/rhgs/crewai-go"
)

// SearchResult represents a single web search result from a SearchProvider.
type SearchResult struct {
	Title     string `json:"title"`
	URL       string `json:"url"`
	Snippet   string `json:"snippet"`
	SourceOrg string `json:"source_org,omitempty"`
}

// SearchProvider is the interface for a web search backend.
// Implementations must be safe for concurrent use.
type SearchProvider interface {
	Search(ctx context.Context, query string, maxResults int) ([]SearchResult, error)
	Name() string
}

// WebSearchTool is a Tool that searches the web via a SearchProvider.
// It also implements FactSource so results are collected as Facts with
// provenance. Use this in the ReAct loop when you want the LLM to decide
// when to search. For agent-driven search (Go code controls queries), use
// crewai.WebSearcher directly.
type WebSearchTool struct {
	provider   SearchProvider
	maxResults int
	timeout    time.Duration

	mu        sync.Mutex
	lastFacts []crewai.Fact
}

// Option configures the WebSearchTool.
type Option func(*WebSearchTool)

// WithMaxResults sets the maximum number of results to return.
func WithMaxResults(n int) Option { return func(t *WebSearchTool) { t.maxResults = n } }

// WithSearchTimeout sets the HTTP timeout for search requests.
func WithSearchTimeout(d time.Duration) Option { return func(t *WebSearchTool) { t.timeout = d } }

// NewWebSearch creates a web search tool with the given provider.
// If provider is nil, the default (Wikipedia) is used.
// Wikipedia is free (no API key) but only searches Wikipedia articles.
// For general web search, use NewBraveSearch, NewGoogleSearch, or
// NewLangSearch.
func NewWebSearch(provider SearchProvider, opts ...Option) *WebSearchTool {
	if provider == nil {
		provider = NewWikipediaSearch()
	}
	t := &WebSearchTool{provider: provider, maxResults: 5, timeout: 30 * time.Second}
	for _, o := range opts {
		o(t)
	}
	return t
}

// Name implements crewai.Tool.
func (t *WebSearchTool) Name() string { return "web_search" }

// Description implements crewai.Tool.
func (t *WebSearchTool) Description() string {
	return "Searches the web for a term. Input: the search query string."
}

// Call implements crewai.Tool. It searches the web and returns a formatted
// string of results for the ReAct observation.
func (t *WebSearchTool) Call(ctx context.Context, input string) (string, error) {
	query := strings.TrimSpace(input)
	if query == "" {
		return "", fmt.Errorf("web_search: empty query")
	}

	searchCtx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	results, err := t.provider.Search(searchCtx, query, t.maxResults)
	if err != nil {
		return "", fmt.Errorf("web_search: %w", err)
	}

	// Filter SSRF.
	var filtered []SearchResult
	for _, r := range results {
		if !isBlockedURL(r.URL) {
			filtered = append(filtered, r)
		}
	}

	// Build observation string.
	var b strings.Builder
	fmt.Fprintf(&b, "Found %d results for '%s':\n", len(filtered), query)
	for i, r := range filtered {
		fmt.Fprintf(&b, "\n%d. %s -- %s\n", i+1, r.Title, r.URL)
		if r.Snippet != "" {
			fmt.Fprintf(&b, "   %s\n", r.Snippet)
		}
	}

	// Produce facts.
	facts := make([]crewai.Fact, 0, len(filtered))
	for _, r := range filtered {
		rawPayload, _ := json.Marshal(r)
		facts = append(facts, crewai.NewFact(
			r.Title+": "+r.Snippet,
			r.SourceOrg,
			r.URL,
			rawPayload,
		))
	}

	t.mu.Lock()
	t.lastFacts = facts
	t.mu.Unlock()

	return b.String(), nil
}

// Facts implements crewai.FactSource.
func (t *WebSearchTool) Facts() []crewai.Fact {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.lastFacts
}

// Compile-time check.
var _ crewai.Tool = (*WebSearchTool)(nil)
var _ crewai.FactSource = (*WebSearchTool)(nil)

// extractHost returns the hostname from a URL.
func extractHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// isBlockedURL checks whether a URL points to a private/loopback/unspecified
// address. This prevents SSRF attacks where a malicious search result could
// direct the agent to internal endpoints (e.g. cloud metadata at 169.254.169.254).
//
// Checks: localhost, 127.0.0.1, ::1, private IPs (10.x, 172.16-31.x,
// 192.168.x), link-local (169.254.x), and unspecified (0.0.0.0).
func isBlockedURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return true
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return true
	}
	host := u.Hostname()
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return true
	}
	ip := net.ParseIP(host)
	if ip != nil && (ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()) {
		return true
	}
	return false
}

// ensure net/http is used (for future provider implementations that need it).
var _ = http.StatusOK