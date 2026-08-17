// Package tools -- Wikipedia search provider.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"time"
)

// WikipediaSearch implements SearchProvider using the Wikipedia API.
// No API key required. Only searches Wikipedia articles, not the
// general web. Use Brave, Google, or LangSearch providers for general
// web search.
type WikipediaSearch struct {
	httpClient *http.Client
	language   string // e.g. "en", "pt"
	baseURL    string
}

// NewWikipediaSearch creates a Wikipedia search provider (English).
func NewWikipediaSearch() *WikipediaSearch {
	return &WikipediaSearch{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		language:   "en",
		baseURL:    "https://en.wikipedia.org",
	}
}

// WithWikipediaBaseURL sets a custom base URL for testing.
func WithWikipediaBaseURL(url string) func(*WikipediaSearch) {
	return func(w *WikipediaSearch) { w.baseURL = url }
}

// NewWikipediaSearchWithLanguage creates a Wikipedia search provider
// with a specific language edition.
func NewWikipediaSearchWithLanguage(lang string) *WikipediaSearch {
	return &WikipediaSearch{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		language:   lang,
		baseURL:    fmt.Sprintf("https://%s.wikipedia.org", lang),
	}
}

func (w *WikipediaSearch) Name() string { return "wikipedia" }

func (w *WikipediaSearch) Search(ctx context.Context, query string, maxResults int) ([]SearchResult, error) {
	if maxResults <= 0 {
		maxResults = 5
	}
	if maxResults > 50 {
		maxResults = 50
	}

	searchURL := fmt.Sprintf("%s/w/api.php?action=query&list=search&srsearch=%s&format=json&srlimit=%d",
		w.baseURL, url.QueryEscape(query), maxResults)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "crewai-go/1.0 (https://github.com/rhgs/crewai-go)")

	resp, err := w.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("wikipedia search: HTTP %d", resp.StatusCode)
	}

	var result struct {
		Query struct {
			Search []struct {
				Title   string `json:"title"`
				PageID  int    `json:"pageid"`
				Snippet string `json:"snippet"`
			} `json:"search"`
		} `json:"query"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("wikipedia search: decode: %w", err)
	}

	var results []SearchResult
	for _, item := range result.Query.Search {
		snippet := stripTags(item.Snippet)
		pageURL := fmt.Sprintf("https://%s.wikipedia.org/?curid=%d", w.language, item.PageID)
		results = append(results, SearchResult{
			Title:     item.Title,
			URL:       pageURL,
			Snippet:   snippet,
			SourceOrg: "wikipedia.org",
		})
	}
	return results, nil
}

// stripTags removes HTML tags from a string.
var tagRegex = regexp.MustCompile(`<[^>]*>`)

func stripTags(s string) string {
	return tagRegex.ReplaceAllString(s, "")
}

// Compile-time check.
var _ SearchProvider = (*WikipediaSearch)(nil)
