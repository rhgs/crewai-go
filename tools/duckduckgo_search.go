package tools

import (
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// DuckDuckGoSearch implements SearchProvider using the DuckDuckGo HTML
// endpoint. No API key required.
//
// WARNING: DuckDuckGo blocks automated access with a captcha/anomaly
// page (HTTP 202). This provider may fail at any time and should NOT
// be used as the default. Use WikipediaSearch (free, Wikipedia only)
// or BraveSearch/GoogleSearch/LangSearch (require API keys) instead.
type DuckDuckGoSearch struct {
	httpClient *http.Client
	baseURL    string
}

// NewDuckDuckGoSearch creates a DuckDuckGo search provider.
// Deprecated: Use NewWikipediaSearch (free) or NewBraveSearch (general
// web search with API key) instead.
func NewDuckDuckGoSearch() *DuckDuckGoSearch {
	return &DuckDuckGoSearch{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    "https://duckduckgo.com/html/",
	}
}

// WithDuckDuckGoBaseURL sets a custom base URL for testing.
func WithDuckDuckGoBaseURL(url string) func(*DuckDuckGoSearch) {
	return func(d *DuckDuckGoSearch) { d.baseURL = url }
}

func (d *DuckDuckGoSearch) Name() string { return "duckduckgo" }

func (d *DuckDuckGoSearch) Search(ctx context.Context, query string, maxResults int) ([]SearchResult, error) {
	if maxResults <= 0 {
		maxResults = 5
	}

	searchURL := d.baseURL + "?q=" + url.QueryEscape(query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; crewai-go/1.0)")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}

	// Detect anomaly/captcha page.
	if resp.StatusCode == http.StatusAccepted || strings.Contains(string(body), "anomaly") {
		return nil, fmt.Errorf("duckduckgo: blocked by anomaly/captcha page (HTTP %d)", resp.StatusCode)
	}

	return parseDuckDuckGoHTML(body, maxResults), nil
}

var (
	ddgResultLink    = regexp.MustCompile(`<a[^>]*class="[^"]*result__a[^"]*"[^>]*href="([^"]*)"[^>]*>(.*?)</a>`)
	ddgResultSnippet = regexp.MustCompile(`<a[^>]*class="[^"]*result__snippet[^"]*"[^>]*>(.*?)</a>`)
	ddgStripTags     = regexp.MustCompile(`<[^>]*>`)
)

func parseDuckDuckGoHTML(body []byte, max int) []SearchResult {
	links := ddgResultLink.FindAllSubmatch(body, -1)
	snippets := ddgResultSnippet.FindAllSubmatch(body, -1)

	var results []SearchResult
	for i, link := range links {
		if i >= max {
			break
		}
		rawURL := string(link[1])
		title := html.UnescapeString(string(ddgStripTags.ReplaceAll(link[2], nil)))

		actualURL := decodeDuckDuckGoURL(rawURL)

		var snippet string
		if i < len(snippets) {
			snippet = html.UnescapeString(string(ddgStripTags.ReplaceAll(snippets[i][1], nil)))
		}

		results = append(results, SearchResult{
			Title:     title,
			URL:       actualURL,
			Snippet:   snippet,
			SourceOrg: extractHost(actualURL),
		})
	}
	return results
}

func decodeDuckDuckGoURL(raw string) string {
	if strings.Contains(raw, "uddg=") {
		u, err := url.Parse(raw)
		if err == nil {
			if encoded := u.Query().Get("uddg"); encoded != "" {
				if decoded, err := url.QueryUnescape(encoded); err == nil {
					return decoded
				}
			}
		}
	}
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	return raw
}

// Compile-time check.
var _ SearchProvider = (*DuckDuckGoSearch)(nil)