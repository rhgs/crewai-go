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

// GoogleSearch implements SearchProvider using Google Custom Search JSON API.
type GoogleSearch struct {
	apiKey     string
	cxID       string
	httpClient *http.Client
	baseURL    string
}

// NewGoogleSearch creates a Google Custom Search provider.
// Reads GOOGLE_API_KEY and GOOGLE_CSE_ID from the environment if not set.
func NewGoogleSearch(apiKey, cxID string) *GoogleSearch {
	if apiKey == "" {
		apiKey = os.Getenv("GOOGLE_API_KEY")
	}
	if cxID == "" {
		cxID = os.Getenv("GOOGLE_CSE_ID")
	}
	return &GoogleSearch{
		apiKey:     apiKey,
		cxID:       cxID,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    "https://www.googleapis.com/customsearch/v1",
	}
}

// WithGoogleBaseURL sets a custom base URL for testing.
func WithGoogleBaseURL(url string) func(*GoogleSearch) {
	return func(g *GoogleSearch) { g.baseURL = url }
}

func (g *GoogleSearch) Name() string { return "google" }

func (g *GoogleSearch) Search(ctx context.Context, query string, maxResults int) ([]SearchResult, error) {
	if g.apiKey == "" {
		return nil, fmt.Errorf("google search: GOOGLE_API_KEY not set")
	}
	if g.cxID == "" {
		return nil, fmt.Errorf("google search: GOOGLE_CSE_ID not set")
	}
	if maxResults <= 0 || maxResults > 10 {
		maxResults = 10
	}

	// Escape every query parameter. Never include the raw URL (which holds
	// the API key) in returned errors — http.Client errors embed it.
	searchURL := fmt.Sprintf("%s?q=%s&key=%s&cx=%s&num=%d",
		g.baseURL, url.QueryEscape(query), url.QueryEscape(g.apiKey), url.QueryEscape(g.cxID), maxResults)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("google search: building request: %w", err)
	}

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("google search: request failed")
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("google search: reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		// Do not echo the body: Google error payloads can restate the key.
		return nil, fmt.Errorf("google search: HTTP %d", resp.StatusCode)
	}

	var result struct {
		Items []struct {
			Title   string `json:"title"`
			Link    string `json:"link"`
			Snippet string `json:"snippet"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("google search: decode: %w", err)
	}

	var results []SearchResult
	for _, item := range result.Items {
		results = append(results, SearchResult{
			Title:     item.Title,
			URL:       item.Link,
			Snippet:   item.Snippet,
			SourceOrg: extractHost(item.Link),
		})
	}
	return results, nil
}

// Compile-time check.
var _ SearchProvider = (*GoogleSearch)(nil)
