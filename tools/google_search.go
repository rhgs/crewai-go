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
	}
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

	searchURL := fmt.Sprintf("https://www.googleapis.com/customsearch/v1?q=%s&key=%s&cx=%s&num=%d",
		url.QueryEscape(query), g.apiKey, g.cxID, maxResults)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("google search: HTTP %d: %s", resp.StatusCode, string(body))
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