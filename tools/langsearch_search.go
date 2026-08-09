package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// LangSearch implements SearchProvider using the LangSearch API.
// LangSearch is 100% free (no credit card, no subscription) but requires
// a free API key from https://langsearch.com.
//
// Results include snippets and semantic summaries, making it richer than
// Wikipedia-only search. Use this as a free general web search provider.
type LangSearch struct {
	apiKey     string
	httpClient *http.Client
	baseURL    string
}

// NewLangSearch creates a LangSearch provider.
// Reads LANGSEARCH_API_KEY from the environment if not set.
func NewLangSearch(apiKey string) *LangSearch {
	if apiKey == "" {
		apiKey = os.Getenv("LANGSEARCH_API_KEY")
	}
	return &LangSearch{
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    "https://api.langsearch.com/v1/web-search",
	}
}

// WithLangSearchBaseURL sets a custom base URL for testing.
func WithLangSearchBaseURL(url string) func(*LangSearch) {
	return func(l *LangSearch) { l.baseURL = url }
}

func (l *LangSearch) Name() string { return "langsearch" }

type langSearchRequest struct {
	Query     string `json:"query"`
	Freshness string `json:"freshness,omitempty"`
	Summary   bool   `json:"summary"`
	Count     int    `json:"count"`
}

func (l *LangSearch) Search(ctx context.Context, query string, maxResults int) ([]SearchResult, error) {
	if l.apiKey == "" {
		return nil, fmt.Errorf("langsearch: LANGSEARCH_API_KEY not set (get a free key at https://langsearch.com)")
	}
	if maxResults <= 0 || maxResults > 20 {
		maxResults = 10
	}

	reqBody := langSearchRequest{
		Query:     query,
		Freshness: "noLimit",
		Summary:   true,
		Count:     maxResults,
	}
	buf, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("langsearch: encoding request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		l.baseURL, bytes.NewReader(buf))
	if err != nil {
		return nil, fmt.Errorf("langsearch: creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+l.apiKey)

	resp, err := l.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("langsearch: sending request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("langsearch: reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("langsearch: HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			WebPages struct {
				Value []struct {
					Name    string `json:"name"`
					URL     string `json:"url"`
					Snippet string `json:"snippet"`
					Summary string `json:"summary"`
				} `json:"value"`
			} `json:"webPages"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("langsearch: decoding response: %w", err)
	}
	if result.Code != 200 {
		return nil, fmt.Errorf("langsearch: API error code %d: %s", result.Code, result.Msg)
	}

	var results []SearchResult
	for _, r := range result.Data.WebPages.Value {
		// Use summary if available (richer), otherwise snippet.
		snippet := r.Snippet
		if r.Summary != "" {
			snippet = r.Summary
		}
		results = append(results, SearchResult{
			Title:     r.Name,
			URL:       r.URL,
			Snippet:   snippet,
			SourceOrg: extractHost(r.URL),
		})
	}
	return results, nil
}

// Compile-time check.
var _ SearchProvider = (*LangSearch)(nil)
