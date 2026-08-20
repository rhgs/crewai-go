package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

// BraveSearch implements SearchProvider using the Brave Search API.
type BraveSearch struct {
	apiKey     string
	httpClient *http.Client
	baseURL    string
}

// NewBraveSearch creates a Brave Search provider.
// Reads BRAVE_API_KEY from the environment if not set.
func NewBraveSearch(apiKey string) *BraveSearch {
	if apiKey == "" {
		apiKey = os.Getenv("BRAVE_API_KEY")
	}
	return &BraveSearch{
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    "https://api.search.brave.com/res/v1/web/search",
	}
}

// WithBraveBaseURL sets a custom base URL for testing.
func WithBraveBaseURL(url string) func(*BraveSearch) {
	return func(b *BraveSearch) { b.baseURL = url }
}

func (b *BraveSearch) Name() string { return "brave" }

func (b *BraveSearch) Search(ctx context.Context, query string, maxResults int) ([]SearchResult, error) {
	if b.apiKey == "" {
		return nil, fmt.Errorf("brave search: BRAVE_API_KEY not set")
	}
	if maxResults <= 0 || maxResults > 20 {
		maxResults = 10
	}

	searchURL := fmt.Sprintf("%s?q=%s&count=%d",
		b.baseURL, url.QueryEscape(query), maxResults)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", b.apiKey)

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		// Do not echo the body: upstream error payloads can restate tokens.
		return nil, fmt.Errorf("brave search: HTTP %d", resp.StatusCode)
	}

	var result struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("brave search: decode: %w", err)
	}

	var results []SearchResult
	for _, r := range result.Web.Results {
		results = append(results, SearchResult{
			Title:     r.Title,
			URL:       r.URL,
			Snippet:   r.Description,
			SourceOrg: extractHost(r.URL),
		})
	}
	return results, nil
}

// Compile-time check.
var _ SearchProvider = (*BraveSearch)(nil)
