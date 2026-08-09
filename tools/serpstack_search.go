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
	}
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
	searchURL := fmt.Sprintf("http://api.serpstack.com/search?access_key=%s&query=%s&num=%d",
		url.QueryEscape(s.apiKey), url.QueryEscape(query), maxResults)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
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
		return nil, fmt.Errorf("serpstack: error %d (%s): %s",
			result.Error.Code, result.Error.Type, result.Error.Info)
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