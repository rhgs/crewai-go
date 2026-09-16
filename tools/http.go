package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/rhgs/crewai-go"
)

const defaultHTTPTimeout = 30 * time.Second
const maxRedirects = 3

// HTTPFetch is an SSRF-safe HTTP GET tool (D-T1, D-T8). Hosts must be
// allowlisted (D-T2: empty allowlist denies every request). Redirects are
// capped at 3 hops and re-validated (D-T4). Bodies are capped at
// crewai.MaxToolOutputBytes (D-T6).
type HTTPFetch struct {
	allow      []string
	methods    map[string]bool
	timeout    time.Duration
	maxBytes   int
	headers    map[string]string
	exactAllow map[string]bool
	client     *http.Client
}

// HTTPOption configures HTTPFetch.
type HTTPOption func(*HTTPFetch)

// WithHTTPAllowlist appends hostname patterns. Each pattern is matched with
// path.Match against the request host (no port). Examples: "api.example.com",
// "docs.*.internal". Matching is case-insensitive.
func WithHTTPAllowlist(hosts ...string) HTTPOption {
	return func(h *HTTPFetch) {
		h.allow = append(h.allow, hosts...)
	}
}

// WithHTTPMethods replaces the allowed methods. Default is GET only (D-T8).
func WithHTTPMethods(methods ...string) HTTPOption {
	return func(h *HTTPFetch) {
		h.methods = map[string]bool{}
		for _, m := range methods {
			m = strings.ToUpper(strings.TrimSpace(m))
			if m != "" {
				h.methods[m] = true
			}
		}
	}
}

// WithHTTPTimeout sets the client timeout. Zero keeps the 30s default.
func WithHTTPTimeout(d time.Duration) HTTPOption {
	return func(h *HTTPFetch) {
		if d > 0 {
			h.timeout = d
		}
	}
}

// WithHTTPMaxResponse caps the response body. Zero or negative keeps
// crewai.MaxToolOutputBytes.
func WithHTTPMaxResponse(n int) HTTPOption {
	return func(h *HTTPFetch) {
		if n > 0 {
			h.maxBytes = n
		}
	}
}

// WithHTTPHeader sets a caller-owned request header. No Authorization or
// Cookie header is ever injected implicitly.
func WithHTTPHeader(key, value string) HTTPOption {
	return func(h *HTTPFetch) {
		if h.headers == nil {
			h.headers = map[string]string{}
		}
		h.headers[key] = value
	}
}

// WithHTTPClient replaces the default client (tests). Redirect policy of
// HTTPFetch still wraps CheckRedirect.
func WithHTTPClient(c *http.Client) HTTPOption {
	return func(h *HTTPFetch) {
		h.client = c
	}
}

// NewHTTPFetch builds an HTTP fetch tool. With no allowlist every Call
// is rejected (deny-by-default).
func NewHTTPFetch(opts ...HTTPOption) *HTTPFetch {
	h := &HTTPFetch{
		methods:  map[string]bool{http.MethodGet: true},
		timeout:  defaultHTTPTimeout,
		maxBytes: crewai.MaxToolOutputBytes,
	}
	for _, o := range opts {
		o(h)
	}
	if h.maxBytes <= 0 {
		h.maxBytes = crewai.MaxToolOutputBytes
	}
	if h.timeout <= 0 {
		h.timeout = defaultHTTPTimeout
	}
	h.exactAllow = exactAllowFrom(h.allow)
	h.client = h.buildClient()
	return h
}

func exactAllowFrom(patterns []string) map[string]bool {
	out := map[string]bool{}
	for _, p := range patterns {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" || strings.ContainsAny(p, "*?[]") {
			continue
		}
		out[p] = true
	}
	return out
}

func (h *HTTPFetch) buildClient() *http.Client {
	base := h.client
	timeout := h.timeout
	if timeout <= 0 {
		timeout = defaultHTTPTimeout
	}
	var inner http.RoundTripper
	if base != nil {
		inner = base.Transport
		if base.Timeout > 0 {
			timeout = base.Timeout
		}
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: inner,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("http_fetch: too many redirects")
			}
			return h.checkURL(req.URL)
		},
	}
}

// Name implements crewai.Tool.
func (h *HTTPFetch) Name() string { return "http_fetch" }

// Description implements crewai.Tool.
func (h *HTTPFetch) Description() string {
	return "Fetches a URL over HTTP(S). Input: the URL. " +
		"Only allowlisted hosts are accepted; default method is GET."
}

// Call implements crewai.Tool. Input is the URL to fetch.
func (h *HTTPFetch) Call(ctx context.Context, input string) (string, error) {
	raw := strings.TrimSpace(input)
	if raw == "" {
		return "", fmt.Errorf("http_fetch: empty url")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("http_fetch: parse: %w", err)
	}
	if err := h.checkURL(u); err != nil {
		return "", err
	}
	method := http.MethodGet
	if !h.methods[method] {
		return "", fmt.Errorf("http_fetch: method %s not allowed", method)
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), nil)
	if err != nil {
		return "", fmt.Errorf("http_fetch: %w", err)
	}
	for k, v := range h.headers {
		req.Header.Set(k, v)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("http_fetch: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(h.maxBytes)+1))
	if err != nil {
		return "", fmt.Errorf("http_fetch: read: %w", err)
	}
	if len(body) > h.maxBytes {
		body = body[:h.maxBytes]
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("http_fetch: HTTP %d", resp.StatusCode)
	}
	return string(body), nil
}

func (h *HTTPFetch) checkURL(u *url.URL) error {
	if u == nil {
		return fmt.Errorf("http_fetch: empty url")
	}
	raw := u.String()
	if blockedURL(raw, h.exactAllow) {
		return fmt.Errorf("http_fetch: url blocked: %s", u.Redacted())
	}
	if !h.hostAllowed(u.Hostname()) {
		return fmt.Errorf("http_fetch: host not allowlisted: %s", u.Hostname())
	}
	return nil
}

func (h *HTTPFetch) hostAllowed(host string) bool {
	if len(h.allow) == 0 {
		return false
	}
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	for _, p := range h.allow {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" {
			continue
		}
		if ok, _ := path.Match(p, host); ok {
			return true
		}
	}
	return false
}

var _ crewai.Tool = (*HTTPFetch)(nil)
