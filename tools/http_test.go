package tools

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rhgs/crewai-go"
)

func TestNewHTTPFetch_DefensiveDefaults(t *testing.T) {
	h := NewHTTPFetch(func(h *HTTPFetch) {
		h.maxBytes = 0
		h.timeout = 0
	})
	if h.maxBytes != crewai.MaxToolOutputBytes {
		t.Fatalf("cap %d", h.maxBytes)
	}
	if h.timeout != defaultHTTPTimeout {
		t.Fatalf("timeout %v", h.timeout)
	}
}

func TestHTTPFetch_DenyByDefault(t *testing.T) {
	h := NewHTTPFetch()
	if _, err := h.Call(context.Background(), "https://example.com"); err == nil {
		t.Fatal("empty allowlist must deny")
	}
	if h.Name() != "http_fetch" || h.Description() == "" {
		t.Fatal("name/desc")
	}
}

func TestHTTPFetch_EmptyURL(t *testing.T) {
	h := NewHTTPFetch(WithHTTPAllowlist("example.com"))
	if _, err := h.Call(context.Background(), "  "); err == nil {
		t.Fatal("empty url")
	}
}

func TestHTTPFetch_SSRFMatrix(t *testing.T) {
	h := NewHTTPFetch(WithHTTPAllowlist("*"))
	blocked := []string{
		"ftp://example.com",
		"file:///etc/passwd",
		"http://localhost/a",
		"http://127.0.0.1/a",
		"http://[::1]/a",
		"http://192.168.0.1/a",
		"http://10.0.0.5/a",
		"http://172.16.1.1/a",
		"http://169.254.169.254/latest",
		"http://0.0.0.0/",
		"http://metadata.google.internal/",
		"http://metadata/",
		"http://100.64.0.1/",
		"http://224.0.0.1/",
		"http://user:pass@example.com/",
		"not a url",
		"http:///",
	}
	for _, u := range blocked {
		if _, err := h.Call(context.Background(), u); err == nil {
			t.Errorf("expected blocked: %s", u)
		}
	}
}

func TestHTTPFetch_AllowlistAndGET(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method %s", r.Method)
		}
		if r.Header.Get("X-Test") != "1" {
			t.Errorf("missing header")
		}
		_, _ = io.WriteString(w, "hello-body")
	}))
	defer srv.Close()

	host := httptestHost(t, srv)
	h := NewHTTPFetch(
		WithHTTPAllowlist(host),
		WithHTTPHeader("X-Test", "1"),
		WithHTTPTimeout(5*time.Second),
		WithHTTPMaxResponse(1024),
	)
	out, err := h.Call(context.Background(), srv.URL+"/x")
	if err != nil {
		t.Fatal(err)
	}
	if out != "hello-body" {
		t.Fatalf("got %q", out)
	}
}

func TestHTTPFetch_HostNotAllowlisted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "nope")
	}))
	defer srv.Close()
	h := NewHTTPFetch(WithHTTPAllowlist("other.example"))
	if _, err := h.Call(context.Background(), srv.URL); err == nil {
		t.Fatal("expected host reject")
	}
}

func TestHTTPFetch_GlobAllowlist(t *testing.T) {
	h := NewHTTPFetch(WithHTTPAllowlist("docs.*.internal", "api.example.com"))
	if !h.hostAllowed("docs.foo.internal") {
		t.Fatal("glob")
	}
	if !h.hostAllowed("api.example.com") {
		t.Fatal("exact")
	}
	if h.hostAllowed("evil.example.com") {
		t.Fatal("other")
	}
	if h.hostAllowed("") {
		t.Fatal("empty")
	}
}

func TestHTTPFetch_RedirectRevalidated(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			http.Redirect(w, r, srv.URL+"/next", http.StatusFound)
		case "/next":
			http.Redirect(w, r, "http://169.254.169.254/latest", http.StatusFound)
		default:
			_, _ = io.WriteString(w, "should-not")
		}
	}))
	defer srv.Close()
	host := httptestHost(t, srv)
	h := NewHTTPFetch(WithHTTPAllowlist(host))
	if _, err := h.Call(context.Background(), srv.URL+"/start"); err == nil {
		t.Fatal("redirect to blocked hop must fail")
	}
}

func TestHTTPFetch_RedirectTooMany(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srv.URL+"/loop", http.StatusFound)
	}))
	defer srv.Close()
	h := NewHTTPFetch(WithHTTPAllowlist(httptestHost(t, srv)))
	if _, err := h.Call(context.Background(), srv.URL+"/loop"); err == nil {
		t.Fatal("too many redirects")
	}
}

func TestHTTPFetch_RedirectSameHostOK(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/a" {
			http.Redirect(w, r, srv.URL+"/b", http.StatusFound)
			return
		}
		_, _ = io.WriteString(w, "landed")
	}))
	defer srv.Close()
	h := NewHTTPFetch(WithHTTPAllowlist(httptestHost(t, srv)))
	out, err := h.Call(context.Background(), srv.URL+"/a")
	if err != nil {
		t.Fatal(err)
	}
	if out != "landed" {
		t.Fatalf("got %q", out)
	}
}

func TestHTTPFetch_HTTPStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusTeapot)
	}))
	defer srv.Close()
	h := NewHTTPFetch(WithHTTPAllowlist(httptestHost(t, srv)))
	if _, err := h.Call(context.Background(), srv.URL); err == nil {
		t.Fatal("expected HTTP error")
	}
}

func TestHTTPFetch_MaxResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("x", 64))
	}))
	defer srv.Close()
	h := NewHTTPFetch(WithHTTPAllowlist(httptestHost(t, srv)), WithHTTPMaxResponse(8))
	out, err := h.Call(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 8 {
		t.Fatalf("len=%d", len(out))
	}
}

func TestHTTPFetch_DefaultCap(t *testing.T) {
	h := NewHTTPFetch(WithHTTPMaxResponse(0))
	if h.maxBytes != crewai.MaxToolOutputBytes {
		t.Fatalf("cap %d", h.maxBytes)
	}
}

func TestHTTPFetch_MethodsOption(t *testing.T) {
	h := NewHTTPFetch(WithHTTPAllowlist("example.com"), WithHTTPMethods("POST"))
	if h.methods[http.MethodGet] {
		t.Fatal("GET should be gone")
	}
	if _, err := h.Call(context.Background(), "https://example.com"); err == nil {
		t.Fatal("GET not allowed")
	}
}

func TestHTTPFetch_CanceledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = io.WriteString(w, "late")
	}))
	defer srv.Close()
	h := NewHTTPFetch(WithHTTPAllowlist(httptestHost(t, srv)))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := h.Call(ctx, srv.URL); err == nil {
		t.Fatal("canceled ctx")
	}
}

func TestHTTPFetch_CustomClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "via-client")
	}))
	defer srv.Close()
	c := &http.Client{Timeout: 2 * time.Second}
	h := NewHTTPFetch(WithHTTPAllowlist(httptestHost(t, srv)), WithHTTPClient(c))
	out, err := h.Call(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if out != "via-client" {
		t.Fatalf("%q", out)
	}
}

func TestHTTPFetch_ParseAndNilURL(t *testing.T) {
	h := NewHTTPFetch(WithHTTPAllowlist("*"))
	if _, err := h.Call(context.Background(), "http://["); err == nil {
		t.Fatal("parse")
	}
	if err := h.checkURL(nil); err == nil {
		t.Fatal("nil url")
	}
}

func TestHTTPFetch_LocalhostExactAllow(t *testing.T) {
	if blockedURL("http://localhost/x", map[string]bool{"localhost": true}) {
		t.Fatal("exact localhost allow should bypass name block")
	}
	if !blockedURL("http://localhost/x", nil) {
		t.Fatal("localhost still blocked without allow")
	}
	if !blockedURL("http://169.254.169.254/", map[string]bool{"169.254.169.254": true}) {
		t.Fatal("link-local never allow")
	}
}

func TestHTTPFetch_EmptyMethodsSkipped(t *testing.T) {
	h := NewHTTPFetch(WithHTTPMethods("GET", "  ", ""))
	if !h.methods[http.MethodGet] || len(h.methods) != 1 {
		t.Fatalf("%v", h.methods)
	}
}

func TestHTTPFetch_HostAllowedEmptyPatterns(t *testing.T) {
	h := NewHTTPFetch(WithHTTPAllowlist("  ", "example.com"))
	if !h.hostAllowed("example.com") {
		t.Fatal("trim")
	}
	if h.hostAllowed("EXAMPLE.com") && !h.hostAllowed("example.com") {
		t.Fatal("case")
	}
}

func httptestHost(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	u := srv.URL
	host := strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}
