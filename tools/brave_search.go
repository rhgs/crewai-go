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
	}
}

func (b *BraveSearch) Name() string { return "brave" }

func (b *BraveSearch) Search(ctx context.Context, query string, maxResults int) ([]SearchResult, error) {
	if b.apiKey == "" {
		return nil, fmt.Errorf("brave search: BRAVE_API_KEY not set")
	}
	if maxResults <= 0 || maxResults > 20 {
		maxResults = 10
	}

	searchURL := fmt.Sprintf("https://api.search.brave.com/res/v1/web/search?q=%s&count=%d",
		url.QueryEscape(query), maxResults)

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
		return nil, fmt.Errorf("brave search: HTTP %d: %s", resp.StatusCode, string(body))
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