package crewai

import "context"

// SearchHit is a single web search result. It is returned by WebSearcher
// implementations and by the WebSearchTool. Content is a short text
// excerpt (snippet), not the full page content.
type SearchHit struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Content string `json:"content"`
}

// WebSearcher is an optional capability that an LLM may implement when the
// underlying provider has a native web search API. It allows agents to
// search the web directly from Go code (agent-driven), without going
// through the ReAct loop or tool calling.
//
// Providers that have a native search API (e.g. Ollama Cloud /api/web_search)
// implement this interface. Providers that do not (e.g. local Ollama, mock)
// do not implement it, and the executor returns ErrWebSearchUnsupported.
//
// This is the agent-driven pattern: the agent's Go code decides what to
// search, when to search, and how many results to fetch. The LLM is not
// involved in the search decision.
type WebSearcher interface {
	// WebSearch queries the provider's search API and returns results.
	// max <= 0 uses a provider default (typically 5); max is clamped to
	// a provider-specific maximum (typically 10).
	WebSearch(ctx context.Context, query string, max int) ([]SearchHit, error)
}

// SearchWeb is a convenience function that calls WebSearch on the given LLM
// if it implements WebSearcher. Returns ErrWebSearchUnsupported otherwise.
// This allows agents to call web search directly without a type assertion:
//
//	hits, err := crewai.SearchWeb(ctx, llm, "Go programming", 5)
func SearchWeb(ctx context.Context, llm LLM, query string, max int) ([]SearchHit, error) {
	ws, ok := llm.(WebSearcher)
	if !ok {
		return nil, ErrWebSearchUnsupported
	}
	return ws.WebSearch(ctx, query, max)
}