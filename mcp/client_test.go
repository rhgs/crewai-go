package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func newTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, func()) {
	t.Helper()
	srv := httptest.NewServer(handler)
	return srv, srv.Close
}

func TestNew_DefaultHTTPClient(t *testing.T) {
	c := New("http://example")
	if c.httpClient != http.DefaultClient {
		t.Fatal("New must default to http.DefaultClient")
	}
}

func TestWithHTTPClient_NilIgnored(t *testing.T) {
	c := New("http://example", WithHTTPClient(nil))
	if c.httpClient != http.DefaultClient {
		t.Fatal("WithHTTPClient(nil) must not replace the default client")
	}
}

func TestWithHeader_EmptyKeyDropped(t *testing.T) {
	c := New("http://example", WithHeader("", "v"))
	if len(c.headers) != 0 {
		t.Fatalf("empty key must be dropped, got %d headers", len(c.headers))
	}
}

// --- Initialize / session id --------------------------------------------------

func TestInitialize_CapturesSessionID(t *testing.T) {
	var (
		calls      int
		firstSeen  string
		secondSeen string
	)
	srv, cleanup := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			firstSeen = r.Header.Get("Mcp-Session-Id")
			w.Header().Set("Mcp-Session-Id", "sess-123")
		} else {
			secondSeen = r.Header.Get("Mcp-Session-Id")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{}}`)
	})
	defer cleanup()

	c := New(srv.URL)
	if err := c.Initialize(context.Background(), "test", "1.0"); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if c.sessionID != "sess-123" {
		t.Fatalf("expected session id sess-123, got %q", c.sessionID)
	}
	if firstSeen != "" {
		t.Fatalf("first call must not carry a session id, got %q", firstSeen)
	}
	if secondSeen != "sess-123" {
		t.Fatalf("second call must echo session id, got %q", secondSeen)
	}
}

func TestInitialize_ProtocolVersionAndClientInfo(t *testing.T) {
	var gotParams map[string]any
	srv, cleanup := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		var req jsonRPCRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if p, ok := req.Params.(map[string]any); ok {
			gotParams = p
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{}}`)
	})
	defer cleanup()

	c := New(srv.URL)
	if err := c.Initialize(context.Background(), "auditor", "2.3"); err != nil {
		t.Fatal(err)
	}
	if gotParams["protocolVersion"] != protocolVersion {
		t.Fatalf("expected protocolVersion=%s, got %v", protocolVersion, gotParams["protocolVersion"])
	}
	info, _ := gotParams["clientInfo"].(map[string]any)
	if info["name"] != "auditor" || info["version"] != "2.3" {
		t.Fatalf("clientInfo missing/wrong: %v", info)
	}
}

func TestInitialize_DefaultClientInfo(t *testing.T) {
	var gotParams map[string]any
	srv, cleanup := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		var req jsonRPCRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if p, ok := req.Params.(map[string]any); ok {
			gotParams = p
		}
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{}}`)
	})
	defer cleanup()
	c := New(srv.URL)
	if err := c.Initialize(context.Background(), "", ""); err != nil {
		t.Fatal(err)
	}
	info, _ := gotParams["clientInfo"].(map[string]any)
	if info["name"] == nil || info["version"] == nil {
		t.Fatalf("missing defaults in clientInfo: %v", info)
	}
}

func TestInitialize_HTTPError(t *testing.T) {
	srv, cleanup := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `boom`)
	})
	defer cleanup()

	c := New(srv.URL)
	err := c.Initialize(context.Background(), "t", "1")
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("expected 500 in error, got %v", err)
	}
}

func TestInitialize_HTTPError_DoesNotLeakHeaders(t *testing.T) {
	srv, cleanup := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		// Echo back what the client sent under a sensitive name. The
		// client must NOT propagate it in error messages.
		w.Header().Set("X-Sensitive", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusBadRequest)
	})
	defer cleanup()
	c := New(srv.URL, WithHeader("Authorization", "Bearer SECRET"))
	err := c.Initialize(context.Background(), "t", "1")
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("error leaked header value: %v", err)
	}
}

func TestInitialize_JSONRPCError(t *testing.T) {
	srv, cleanup := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"bad request"}}`)
	})
	defer cleanup()

	c := New(srv.URL)
	err := c.Initialize(context.Background(), "t", "1")
	if err == nil || !strings.Contains(err.Error(), "bad request") {
		t.Fatalf("expected jsonrpc error in wrapped error, got %v", err)
	}
}

func TestInitialize_MalformedBody(t *testing.T) {
	srv, cleanup := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `not json`)
	})
	defer cleanup()
	c := New(srv.URL)
	if err := c.Initialize(context.Background(), "t", "1"); err == nil {
		t.Fatal("malformed response must error")
	}
}

func TestInitialize_NotifyFailure(t *testing.T) {
	srv, cleanup := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	defer cleanup()
	c := New(srv.URL)
	if err := c.Initialize(context.Background(), "t", "1"); err == nil {
		t.Fatal("notification failure must surface")
	}
}

// --- ListTools / pagination --------------------------------------------------

func TestListTools_NoPagination(t *testing.T) {
	srv, cleanup := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"a","description":"A","inputSchema":{"type":"object"}},{"name":"b","description":"B","inputSchema":{"type":"object"}}]}}`)
	})
	defer cleanup()

	c := New(srv.URL)
	tools, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 2 || tools[0].Name != "a" || tools[1].Name != "b" {
		t.Fatalf("unexpected tools: %+v", tools)
	}
	if string(tools[0].InputSchema) != `{"type":"object"}` {
		t.Fatalf("inputSchema not preserved verbatim: %s", tools[0].InputSchema)
	}
}

func TestListTools_PaginationWithCursor(t *testing.T) {
	var (
		mu        sync.Mutex
		pageCalls int32
	)
	srv, cleanup := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		var req jsonRPCRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		mu.Lock()
		defer mu.Unlock()
		pageCalls++
		cursor := ""
		if p, ok := req.Params.(map[string]any); ok {
			if v, ok := p["cursor"].(string); ok {
				cursor = v
			}
		}
		switch cursor {
		case "":
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"a"}],"nextCursor":"page2"}}`)
		case "page2":
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"b"}]}}`)
		default:
			t.Errorf("unexpected cursor: %q", cursor)
		}
	})
	defer cleanup()

	c := New(srv.URL)
	tools, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 2 || tools[0].Name != "a" || tools[1].Name != "b" {
		t.Fatalf("pagination did not concatenate: %+v", tools)
	}
	if atomic.LoadInt32(&pageCalls) != 2 {
		t.Fatalf("expected 2 paginated calls, got %d", pageCalls)
	}
}

func TestListTools_TolerateSnakeCaseCursor(t *testing.T) {
	srv, cleanup := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"tools":[],"next_cursor":""}}`)
	})
	defer cleanup()
	c := New(srv.URL)
	tools, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 0 {
		t.Fatalf("expected empty, got %d tools", len(tools))
	}
}

func TestListTools_EmptyCatalog(t *testing.T) {
	srv, cleanup := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`)
	})
	defer cleanup()
	c := New(srv.URL)
	tools, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 0 {
		t.Fatalf("expected 0 tools, got %d", len(tools))
	}
}

func TestListTools_TransportError(t *testing.T) {
	srv, cleanup := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"error":{"code":-1,"message":"x"}}`)
	})
	defer cleanup()
	c := New(srv.URL)
	if _, err := c.ListTools(context.Background()); err == nil {
		t.Fatal("jsonrpc error must propagate")
	}
}

// --- CallTool ----------------------------------------------------------------

func TestCallTool_Success(t *testing.T) {
	srv, cleanup := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"hello"}]}}`)
	})
	defer cleanup()
	c := New(srv.URL)
	res, err := c.CallTool(context.Background(), "echo", json.RawMessage(`{"x":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatal("IsError should be false")
	}
	if len(res.Content) != 1 || res.Content[0].Text != "hello" {
		t.Fatalf("unexpected content: %+v", res.Content)
	}
}

func TestCallTool_IsErrorNotTreatedAsGoError(t *testing.T) {
	srv, cleanup := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"db down"}],"isError":true}}`)
	})
	defer cleanup()
	c := New(srv.URL)
	res, err := c.CallTool(context.Background(), "lookup", nil)
	if err != nil {
		t.Fatalf("isError=true must NOT be a Go error; got %v", err)
	}
	if !res.IsError {
		t.Fatal("expected IsError=true to be preserved in the result")
	}
	if res.Content[0].Text != "db down" {
		t.Fatalf("unexpected content: %q", res.Content[0].Text)
	}
}

func TestCallTool_EmptyName(t *testing.T) {
	c := New("http://x")
	if _, err := c.CallTool(context.Background(), "", nil); err == nil {
		t.Fatal("empty tool name must error")
	}
}

func TestCallTool_HTTP500(t *testing.T) {
	srv, cleanup := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, `bad gateway`)
	})
	defer cleanup()
	c := New(srv.URL)
	_, err := c.CallTool(context.Background(), "t", nil)
	if err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("expected 502 error, got %v", err)
	}
}

func TestCallTool_InvalidArgsJSON(t *testing.T) {
	c := New("http://x")
	_, err := c.CallTool(context.Background(), "t", json.RawMessage(`{not json`))
	if err == nil {
		t.Fatal("invalid args JSON must error")
	}
}

func TestCallTool_AcceptsArrayArgs(t *testing.T) {
	var gotParams map[string]any
	srv, cleanup := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		var req jsonRPCRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if p, ok := req.Params.(map[string]any); ok {
			gotParams = p
		}
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"content":[]}}`)
	})
	defer cleanup()
	c := New(srv.URL)
	if _, err := c.CallTool(context.Background(), "t", json.RawMessage(`[1,2,3]`)); err != nil {
		t.Fatal(err)
	}
	args, _ := gotParams["arguments"].([]any)
	if len(args) != 3 {
		t.Fatalf("expected array args, got %v", gotParams["arguments"])
	}
}

// --- Header forwarding -------------------------------------------------------

func TestWithHeader_AppliedToRequests(t *testing.T) {
	var gotAuth string
	srv, cleanup := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{}}`)
	})
	defer cleanup()
	c := New(srv.URL, WithHeader("Authorization", "Bearer secret"))
	if err := c.Initialize(context.Background(), "t", "1"); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer secret" {
		t.Fatalf("expected Authorization header to be sent, got %q", gotAuth)
	}
}

// --- Concurrency -------------------------------------------------------------

func TestClient_ConcurrentCallTool(t *testing.T) {
	var hits int32
	srv, cleanup := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"content":[]}}`)
	})
	defer cleanup()
	c := New(srv.URL)
	if err := c.Initialize(context.Background(), "t", "1"); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = c.CallTool(context.Background(), "t", nil)
		}()
	}
	wg.Wait()
	if got := atomic.LoadInt32(&hits); got < 8 {
		t.Fatalf("expected >= 8 server hits, got %d", got)
	}
}

func TestClient_ConcurrentListTools(t *testing.T) {
	srv, cleanup := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`)
	})
	defer cleanup()
	c := New(srv.URL)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = c.ListTools(context.Background())
		}()
	}
	wg.Wait()
}

// --- Limit reader ------------------------------------------------------------

func TestCallTool_ResponseBounded(t *testing.T) {
	srv, cleanup := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"`)
		buf := make([]byte, MaxMCPResponseBytes+1024)
		for i := range buf {
			buf[i] = 'x'
		}
		_, _ = w.Write(buf)
		_, _ = io.WriteString(w, `"}]}}`)
	})
	defer cleanup()
	c := New(srv.URL)
	// The live path wraps the body in LimitReader(MaxMCPResponseBytes),
	// so the oversized payload is truncated and cannot decode as JSON.
	_, err := c.CallTool(context.Background(), "t", nil)
	if err == nil {
		t.Fatal("oversized response must fail to decode after the 16 MiB cap")
	}
}

func TestCallTool_SSESuccess(t *testing.T) {
	srv, cleanup := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, ": keepalive\n")
		_, _ = io.WriteString(w, "event: message\n")
		_, _ = io.WriteString(w, `data: {"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"from-sse"}]}}`+"\n\n")
	})
	defer cleanup()
	c := New(srv.URL)
	res, err := c.CallTool(context.Background(), "echo", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("SSE CallTool: %v", err)
	}
	if len(res.Content) != 1 || res.Content[0].Text != "from-sse" {
		t.Fatalf("unexpected SSE content: %+v", res.Content)
	}
}

func TestInitialize_SSEHandshake(t *testing.T) {
	var calls int
	srv, cleanup := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "text/event-stream")
		if calls == 1 {
			w.Header().Set("Mcp-Session-Id", "sse-sess")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `data: {"jsonrpc":"2.0","id":1,"result":{}}`+"\n\n")
	})
	defer cleanup()
	c := New(srv.URL)
	if err := c.Initialize(context.Background(), "t", "1"); err != nil {
		t.Fatalf("SSE initialize: %v", err)
	}
	if c.sessionID != "sse-sess" {
		t.Fatalf("session id not captured from SSE response: %q", c.sessionID)
	}
}

func TestListTools_PageCap(t *testing.T) {
	srv, cleanup := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"a"}],"nextCursor":"more"}}`)
	})
	defer cleanup()
	c := New(srv.URL)
	_, err := c.ListTools(context.Background())
	if err == nil || !strings.Contains(err.Error(), "exceeded") {
		t.Fatalf("expected page-cap error, got %v", err)
	}
}

// --- SSE parsing -------------------------------------------------------------

func TestParseSSEChunk_SingleDataBlock(t *testing.T) {
	s := strings.NewReader("event: message\ndata: {\"k\":1}\n\n")
	sc := bufio.NewScanner(s)
	sc.Buffer(make([]byte, 0, 1024), 1<<20)
	got, ok, err := parseSSEChunk(sc)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	if string(got) != `{"k":1}` {
		t.Fatalf("got %q", got)
	}
}

func TestParseSSEChunk_MultipleDataLines(t *testing.T) {
	s := strings.NewReader("data: line1\ndata: line2\n\n")
	sc := bufio.NewScanner(s)
	sc.Buffer(make([]byte, 0, 1024), 1<<20)
	got, ok, err := parseSSEChunk(sc)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	if string(got) != "line1\nline2" {
		t.Fatalf("got %q", got)
	}
}

func TestParseSSEChunk_IgnoresComments(t *testing.T) {
	s := strings.NewReader(": keepalive\ndata: {\"k\":1}\n\n")
	sc := bufio.NewScanner(s)
	sc.Buffer(make([]byte, 0, 1024), 1<<20)
	_, ok, err := parseSSEChunk(sc)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

func TestParseSSEChunk_NoData(t *testing.T) {
	s := strings.NewReader("event: ping\n\n")
	sc := bufio.NewScanner(s)
	sc.Buffer(make([]byte, 0, 1024), 1<<20)
	_, ok, err := parseSSEChunk(sc)
	if err != nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

func TestParseSSEChunk_EmptyLines(t *testing.T) {
	s := strings.NewReader("\n\ndata: {\"k\":1}\n\n")
	sc := bufio.NewScanner(s)
	sc.Buffer(make([]byte, 0, 1024), 1<<20)
	_, ok, err := parseSSEChunk(sc)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

func TestNew_EndpointStored(t *testing.T) {
	c := New("http://example/x")
	if c.endpoint != "http://example/x" {
		t.Fatalf("endpoint not stored: %q", c.endpoint)
	}
}

func TestRequestIDMonotonic(t *testing.T) {
	ids := map[uint64]struct{}{}
	for i := 0; i < 50; i++ {
		id := nextRequestID()
		if _, dup := ids[id]; dup {
			t.Fatalf("duplicate id %d", id)
		}
		ids[id] = struct{}{}
	}
}

func TestReadLimited(t *testing.T) {
	r := readLimited(strings.NewReader(strings.Repeat("a", 1024)))
	b, _ := io.ReadAll(r)
	if len(b) != 1024 {
		t.Fatalf("small body must be untouched, got %d", len(b))
	}
	r2 := readLimited(strings.NewReader(strings.Repeat("a", MaxMCPResponseBytes+10)))
	b2, _ := io.ReadAll(r2)
	if len(b2) != MaxMCPResponseBytes {
		t.Fatalf("oversized body must be capped at MaxMCPResponseBytes, got %d", len(b2))
	}
}
