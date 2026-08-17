# Plan — WebSearch Tool: Built-in Web Search via HTTP

> **Languages:** **English** (current) · [Português](PLAN.websearch.pt-BR.md)

> **Status:** Draft for implementation
> **Scope:** Add a built-in `WebSearch` tool to the `tools` package that lets
> agents search the web during the ReAct loop. The tool implements `FactSource`
> so search results are collected as `Fact`s with provenance. Zero new
> dependencies (stdlib `net/http`, `encoding/json`, `net/url` only).

---

## 1. Goals and Constraints

### 1.1 Goals

1. Provide a `WebSearch` tool in the `tools` package that agents can use to
   search the web for real-time information during the ReAct loop.
2. Support pluggable search backends via a `SearchProvider` interface, so users
   can connect any search API (Google Custom Search, Bing, Brave Search, SearXNG,
   DuckDuckGo, etc.) without modifying the framework.
3. Ship a default provider that works with **no API key** — using the DuckDuckGo
   HTML endpoint (or a JSON API if available) — so the tool works out of the box
   for quick experimentation.
4. Implement `FactSource` so search results are automatically collected as
   `Fact`s with full provenance (source org, URL, timestamp, payload hash).
5. Support result limiting (`MaxResults`), snippet extraction, and URL
   normalization.
6. Be safe by default: timeout, max response size, and no SSRF (no internal IPs
   or localhost in results).

### 1.2 Constraints (must be preserved)

| Constraint | Enforcement |
|---|---|
| Zero external dependencies | `go.mod` unchanged; uses only `net/http`, `encoding/json`, `net/url`, `strings`, `time`, `fmt`, `crypto/sha256`. |
| LLM-agnostic | The tool is a `Tool` + `FactSource`; it runs during the ReAct loop. No LLM dependency. |
| English comments, no accents | All godoc, error messages, identifiers. |
| Hermetic tests | Use `httptest.NewServer` to mock the search API; no real network. |
| Sentinel errors, context on all I/O, functional options | New sentinels in `errors.go` (or `tools`-local); `ctx` passed to all HTTP calls; functional options for `WebSearch`. |
| Concurrency safety | `SearchProvider` implementations must be safe for concurrent use (stateless or mutex-protected). |
| Backward compatibility | The tool is opt-in; no changes to existing code paths. `FactSource` integration is already in the executor. |
| SSRF protection | Block requests to private/loopback IPs and non-HTTP(S) schemes. |

---

## 2. New API Surface

### 2.1 `SearchProvider` interface (tools package)

```go
// SearchProvider is the interface for a web search backend. Implementations
// must be safe for concurrent use.
type SearchProvider interface {
    // Search queries the backend and returns search results.
    Search(ctx context.Context, query string, maxResults int) ([]SearchResult, error)
    // Name identifies the provider (e.g. "duckduckgo", "google", "brave").
    Name() string
}
```

### 2.2 `SearchResult` struct

```go
// SearchResult represents a single search result from a SearchProvider.
type SearchResult struct {
    // Title is the title of the result page.
    Title string `json:"title"`
    // URL is the link to the result page.
    URL string `json:"url"`
    // Snippet is a short text excerpt from the result.
    Snippet string `json:"snippet"`
    // SourceOrg is the organization/site name, used for Fact provenance.
    SourceOrg string `json:"source_org,omitempty"`
}
```

### 2.3 `WebSearch` tool

```go
// WebSearchTool is a Tool that searches the web via a SearchProvider. It
// also implements FactSource so results are collected as Facts with
// provenance.
type WebSearchTool struct {
    provider SearchProvider
    maxResults int
    timeout time.Duration

    mu sync.Mutex
    lastFacts []crewai.Fact
}
```

### 2.4 Functional options

```go
// WithMaxResults sets the maximum number of results to return.
func WithMaxResults(n int) func(*WebSearchTool)

// WithSearchTimeout sets the HTTP timeout for search requests.
func WithSearchTimeout(d time.Duration) func(*WebSearchTool)
```

### 2.5 Constructor

```go
// NewWebSearch creates a web search tool with the given provider and options.
// If provider is nil, the default (DuckDuckGo) is used.
func NewWebSearch(provider SearchProvider, opts ...func(*WebSearchTool)) *WebSearchTool

// NewWebSearchWithDefault creates a web search tool using the default
// DuckDuckGo provider (no API key required).
func NewWebSearchWithDefault(opts ...func(*WebSearchTool)) *WebSearchTool
```

### 2.6 Built-in providers

| Provider | Package | Auth | Status |
|----------|--------|------|--------|
| DuckDuckGo (HTML) | `tools` (`defaultSearchProvider`) | none | Default, no key needed |
| Google Custom Search | `tools` | `GOOGLE_API_KEY` + `GOOGLE_CSE_ID` | Optional, via `NewGoogleSearch` |
| Brave Search | `tools` | `BRAVE_API_KEY` | Optional, via `NewBraveSearch` |

### 2.7 Fact integration

`WebSearchTool` implements `FactSource`. After a successful `Call`, it converts
each `SearchResult` into a `Fact`:

```
Fact{
    Claim:       result.Title + ": " + result.Snippet,
    SourceOrg:   result.SourceOrg (or hostname of result.URL),
    SourceURL:   result.URL,
    CollectedAt: time.Now(),
    PayloadHash: sha256(rawJSONPayload),
}
```

The raw payload for hashing is the JSON encoding of the `SearchResult`.

---

## 3. DuckDuckGo Default Provider

### 3.1 Why DuckDuckGo?

- No API key required — works out of the box.
- HTML endpoint (`https://duckduckgo.com/html/`) is stable and parseable.
- No rate limit for low-volume usage (acceptable for agent demos).

### 3.2 Implementation

```
GET https://duckduckgo.com/html/?q={query}

- Parse HTML response using regex (no external HTML parser).
- Extract result blocks: title, URL, snippet.
- DuckDuckGo HTML uses:
  <a class="result__a" href="...">Title</a>
  <a class="result__snippet" ...>Snippet</a>
- Limit to MaxResults.
- URL normalization: DuckDuckGo redirects through //duckduckgo.com/l/?uddg=;
  decode the actual URL.
```

### 3.3 Regex parsing (no external dependency)

```go
var (
    // resultLink matches result anchor tags.
    resultLink = regexp.MustCompile(`<a[^>]*class="[^"]*result__a[^"]*"[^>]*href="([^"]*)"[^>]*>(.*?)</a>`)
    // resultSnippet matches snippet elements.
    resultSnippet = regexp.MustCompile(`<a[^>]*class="[^"]*result__snippet[^"]*"[^>]*>(.*?)</a>`)
    // stripTags removes HTML tags from a string.
    stripTags = regexp.MustCompile(`<[^>]*>`)
    // unescapeEntities decodes basic HTML entities.
    unescapeEntities = html.UnescapeString
)
```

This uses only `regexp` and `html` (both stdlib). The parsing is intentionally
simple and tolerant — DuckDuckGo's HTML structure may change, and the tool
degrades gracefully (returns fewer results, not an error).

### 3.4 SSRF protection

```go
// isBlockedURL checks whether a URL points to a private/loopback address.
func isBlockedURL(rawURL string) bool {
    u, err := url.Parse(rawURL)
    if err != nil {
        return true
    }
    if u.Scheme != "http" && u.Scheme != "https" {
        return true
    }
    host := u.Hostname()
    if host == "localhost" || host == "127.0.0.1" || host == "::1" {
        return true
    }
    // Block private ranges (10.x, 172.16-31.x, 192.168.x, 169.254.x).
    ip := net.ParseIP(host)
    if ip != nil && (ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast()) {
        return true
    }
    return false
}
```

---

## 4. Google Custom Search Provider (optional)

```go
// GoogleSearchProvider implements SearchProvider using the Google Custom
// Search JSON API.
type GoogleSearchProvider struct {
    apiKey string
    cxID   string // Custom Search Engine ID
}

// NewGoogleSearch creates a Google Custom Search provider.
// Reads GOOGLE_API_KEY and GOOGLE_CSE_ID from the environment if not set.
func NewGoogleSearch(apiKey, cxID string) *GoogleSearchProvider
```

API call:

```
GET https://www.googleapis.com/customsearch/v1?q={query}&key={key}&cx={cx}&num={maxResults}
```

Response is JSON; parse `items[].{title, link, snippet}`.

---

## 5. Brave Search Provider (optional)

```go
// BraveSearchProvider implements SearchProvider using the Brave Search API.
type BraveSearchProvider struct {
    apiKey string
}

// NewBraveSearch creates a Brave Search provider.
// Reads BRAVE_API_KEY from the environment if not set.
func NewBraveSearch(apiKey string) *BraveSearchProvider
```

API call:

```
GET https://api.search.brave.com/res/v1/web/search?q={query}&count={maxResults}
Header: X-Subscription-Token: {apiKey}
```

Response is JSON; parse `web.results[].{title, url, description}`.

---

## 6. Execution Flow

### 6.1 `WebSearchTool.Call`

```
1. Parse input as search query (trim whitespace).
2. Call provider.Search(ctx, query, maxResults).
3. Format results as a human-readable string for the ReAct observation:
   "Found N results for '{query}':
   1. {title} — {url}
      {snippet}
   2. ..."
4. Convert results to Facts and store for FactSource.Facts().
5. Return formatted string.
```

### 6.2 Result formatting (for the agent)

```
Found 3 results for 'Go programming language':

1. The Go Programming Language — https://go.dev
   Go is an open source programming language that makes it easy to build
   simple, reliable, and efficient software.

2. Tutorial: Get started with Go — https://go.dev/doc/tutorial/getting-started
   ...

3. Go (programming language) - Wikipedia — https://en.wikipedia.org/wiki/Go_(programming_language)
   ...
```

### 6.3 Fact production

After `Call` returns, `Facts()` returns the `[]Fact` slice. The executor
already calls `Facts()` on tools implementing `FactSource` after a successful
call (see `executor.go`). No changes to the executor are needed.

---

## 7. File Changes

| File | Change | Description |
|------|--------|-------------|
| `tools/websearch.go` | **New** | `SearchProvider` interface, `SearchResult`, `WebSearchTool`, `NewWebSearch`, `NewWebSearchWithDefault`, DuckDuckGo provider, SSRF protection, fact production. |
| `tools/websearch_test.go` | **New** | Hermetic tests using `httptest.NewServer`: basic search, max results, timeout, empty results, SSRF blocked, fact production, FactSource interface, DuckDuckGo HTML parsing, Google provider (httptest), Brave provider (httptest). |
| `tools/google_search.go` | **New** | `GoogleSearchProvider` + `NewGoogleSearch`. |
| `tools/google_search_test.go` | **New** | Tests using `httptest.NewServer`. |
| `tools/brave_search.go` | **New** | `BraveSearchProvider` + `NewBraveSearch`. |
| `tools/brave_search_test.go` | **New** | Tests using `httptest.NewServer`. |
| `tools/doc.go` | **New** or **Modified** | Package doc listing all built-in tools including WebSearch. |
| `errors.go` | **Modified** | Add `ErrSearchTimeout`, `ErrSearchProvider` sentinels (or keep in `tools` package). |
| `examples/websearch/main.go` | **New** | Runnable example (uses DuckDuckGo default; offline mode with mock for CI). |
| `docs/tools.md` | **Modified** | Add WebSearch section. |
| `docs/pt-BR/tools.md` | **Modified** | Portuguese mirror. |
| `PLAN.md` | **Modified** | Update roadmap: mark web search as in-progress/done. |
| `PLAN.pt-BR.md` | **Modified** | Portuguese mirror. |
| `README.md` | **Modified** | Add WebSearch to built-in tools list + "What's new". |
| `README.pt-BR.md` | **Modified** | Portuguese mirror. |
| `CHANGELOG.md` | **Modified** | Add entry under `[Unreleased]`. |
| `CHANGELOG.pt-BR.md` | **Modified** | Portuguese mirror. |

---

## 8. Test Plan

All tests use `httptest.NewServer` — no real network.

### 8.1 DuckDuckGo provider tests

| Test | Description |
|------|-------------|
| `TestDuckDuckGo_BasicSearch` | Mock server returns HTML with 3 results → parse → 3 `SearchResult` with title, URL, snippet. |
| `TestDuckDuckGo_EmptyResults` | Mock returns HTML with no results → empty slice, no error. |
| `TestDuckDuckGo_MaxResults` | Mock returns 10 results, MaxResults=3 → only 3 returned. |
| `TestDuckDuckGo_URLDecoding` | DuckDuckGo redirect URLs (`//duckduckgo.com/l/?uddg=...`) are decoded to the actual destination. |
| `TestDuckDuckGo_MalformedHTML` | Mock returns broken HTML → no panic, graceful degradation. |
| `TestDuckDuckGo_SSFRBlocked` | Results containing `http://localhost:8080` or `http://10.0.0.1` are filtered out. |

### 8.2 Google provider tests

| Test | Description |
|------|-------------|
| `TestGoogleSearch_BasicSearch` | Mock returns JSON with items → parsed correctly. |
| `TestGoogleSearch_APIError` | Mock returns 403 → error with status code. |
| `TestGoogleSearch_EmptyResults` | Mock returns `{"items": []}` → empty slice, no error. |
| `TestGoogleSearch_MissingKey` | Empty API key → error. |

### 8.3 Brave provider tests

| Test | Description |
|------|-------------|
| `TestBraveSearch_BasicSearch` | Mock returns JSON with web.results → parsed correctly. |
| `TestBraveSearch_APIError` | Mock returns 401 → error. |
| `TestBraveSearch_EmptyResults` | Mock returns empty results array → empty slice. |
| `TestBraveSearch_MissingKey` | Empty API key → error. |

### 8.4 WebSearchTool tests

| Test | Description |
|------|-------------|
| `TestWebSearchTool_Call` | Mock provider returns results → `Call` returns formatted string. |
| `TestWebSearchTool_FactSource` | After `Call`, `Facts()` returns Facts with correct Claim, SourceOrg, SourceURL, PayloadHash. |
| `TestWebSearchTool_Timeout` | Mock provider sleeps > timeout → `ErrSearchTimeout`. |
| `TestWebSearchTool_EmptyQuery` | Empty input → error or empty results. |
| `TestWebSearchTool_FactDedup` | Two searches with same payload → Facts deduplicated by PayloadHash (tested via executor). |
| `TestWebSearchTool_NoSSRF` | Provider returns results with internal IPs → filtered from output and facts. |

### 8.5 Example

`examples/websearch/main.go`:

- Uses the DuckDuckGo default provider (no API key).
- For CI: detects `CI` env var and uses a mock server instead.
- Shows a research agent that uses WebSearch + Calculator tools.

---

## 9. Implementation Order

1. **`tools/websearch.go`** — `SearchProvider` interface, `SearchResult`,
   `WebSearchTool`, `NewWebSearch`, `NewWebSearchWithDefault`, DuckDuckGo
   provider, SSRF protection, fact production, formatting.
2. **`tools/websearch_test.go`** — all DuckDuckGo + WebSearchTool tests.
3. **`tools/google_search.go`** — Google provider.
4. **`tools/google_search_test.go`** — Google tests.
5. **`tools/brave_search.go`** — Brave provider.
6. **`tools/brave_search_test.go`** — Brave tests.
7. **`examples/websearch/main.go`** — runnable example.
8. **`docs/tools.md`** + **`docs/pt-BR/tools.md`** — documentation.
9. **`PLAN.md`** + **`PLAN.pt-BR.md`** — update roadmap.
10. **`README.md`** + **`README.pt-BR.md`** — feature list.
11. **`CHANGELOG.md`** + **`CHANGELOG.pt-BR.md`** — add entry.

---

## 10. Open Decisions

| Decision | Options | Recommendation |
|----------|---------|----------------|
| DuckDuckGo HTML vs Lite vs JSON API? | `/html/` (HTML parse) vs `lite.duckduckgo.com` vs API endpoint | `/html/` — most stable, no key, parseable with regex. |
| Should results include the full page content (fetch + extract)? | Snippets only vs fetch full page | Snippets only in v1 — full-page fetch is a separate `FetchURL` tool (future). Keeps the tool fast and simple. |
| Should `WebSearchTool` cache results? | No cache vs in-memory TTL cache | No cache in v1 — the agent loop naturally handles re-searching. Caching adds complexity and stale-data risk. |
| Should SSRF protection block all non-public IPs or just loopback? | Loopback only vs all private ranges | All private ranges (10.x, 172.16-31.x, 192.168.x, 169.254.x, loopback, link-local) — defense in depth. |
| Should the tool support `site:` / `filetype:` operators? | Pass-through to provider vs filter | Pass-through — the query string is forwarded as-is to the provider, which handles operators. |
| Where should search-specific errors live? | Root `errors.go` vs `tools` package | `tools` package — the errors are tool-specific, not core. Exported as `tools.ErrSearchTimeout`, etc. |