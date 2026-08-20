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

// Serpstack implements SearchProvider using the Serpstack SERP API.
// Free tier: 1000 searches/month. Requires a free API key from
// https://serpstack.com.
//
// NOTE: The free tier uses HTTP (not HTTPS). For HTTPS, a paid plan
// is required.
type Serpstack struct {
	apiKey     string
	httpClient *http.Client
	baseURL    string
}

// NewSerpstack creates a Serpstack provider.
// Reads SERPSTACK_API_KEY from the environment if not set.
func NewSerpstack(apiKey string) *Serpstack {
	if apiKey == "" {
		apiKey = os.Getenv("SERPSTACK_API_KEY")
	}
	return &Serpstack{
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    "http://api.serpstack.com/search",
	}
}

// WithSerpstackBaseURL sets a custom base URL for testing.
func WithSerpstackBaseURL(url string) func(*Serpstack) {
	return func(s *Serpstack) { s.baseURL = url }
}

func (s *Serpstack) Name() string { return "serpstack" }

func (s *Serpstack) Search(ctx context.Context, query string, maxResults int) ([]SearchResult, error) {
	if s.apiKey == "" {
		return nil, fmt.Errorf("serpstack: SERPSTACK_API_KEY not set (get a free key at https://serpstack.com)")
	}
	if maxResults <= 0 || maxResults > 100 {
		maxResults = 10
	}

	// Free tier uses HTTP. For HTTPS, a paid plan is required.
	// Put the API key in the query string only after QueryEscape, and never
	// include the raw URL in returned errors (the key would leak into logs).
	searchURL := fmt.Sprintf("%s?access_key=%s&query=%s&num=%d",
		s.baseURL, url.QueryEscape(s.apiKey), url.QueryEscape(query), maxResults)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("serpstack: building request: %w", err)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		// http.Client errors can embed the full URL (and thus the key).
		return nil, fmt.Errorf("serpstack: request failed")
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("serpstack: reading response: %w", err)
	}

	var result struct {
		Success bool `json:"success"`
		Error   *struct {
			Code int    `json:"code"`
			Type string `json:"type"`
			Info string `json:"info"`
		} `json:"error"`
		OrganicResults []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Snippet string `json:"snippet"`
		} `json:"organic_results"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("serpstack: decode: %w", err)
	}
	if !result.Success && result.Error != nil {
		// Do not echo result.Error.Info verbatim — some upstream errors
		// restate the request URL (and therefore the access_key).
		return nil, fmt.Errorf("serpstack: error %d (%s)",
			result.Error.Code, result.Error.Type)
	}

	var results []SearchResult
	for _, r := range result.OrganicResults {
		results = append(results, SearchResult{
			Title:     r.Title,
			URL:       r.URL,
			Snippet:   r.Snippet,
			SourceOrg: extractHost(r.URL),
		})
	}
	return results, nil
}

// Compile-time check.
var _ SearchProvider = (*Serpstack)(nil)
