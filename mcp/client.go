// Package mcp implements a minimal client for the Model Context Protocol
// over Streamable HTTP transport (JSON-RPC 2.0). It exposes the server's
// tool catalog as values compatible with crewai.Tool via ToolAdapter and
// preserves the original inputSchema for native function calling.
//
// The package is stdlib-only. All network reads use io.LimitReader to
// cap response sizes.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
)

// MaxMCPResponseBytes caps the body size of any MCP HTTP response.
// 16 MiB matches the spec's Streamable HTTP guidance for typical tool
// payloads while keeping memory bounded under DoS. Applied on every
// response in do() so callers cannot bypass the cap.
const MaxMCPResponseBytes = 16 << 20

// maxListToolsPages stops a malicious or buggy server from returning
// an endless nextCursor chain and growing the catalog without bound.
const maxListToolsPages = 256

// protocolVersion is the MCP spec version this client targets.
const protocolVersion = "2025-06-18"

// Client is a connection to one MCP server. A Client is safe for
// concurrent use: the session id is stored under a mutex and shared
// across adapters derived from the same Client.
type Client struct {
	endpoint   string
	httpClient *http.Client
	headers    []headerPair

	// sessionID is set during Initialize; subsequent requests must echo
	// it back as the Mcp-Session-Id header. Protected by sessionMu.
	sessionMu sync.Mutex
	sessionID string
}

type headerPair struct {
	key, val string
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient sets a custom *http.Client. When not supplied, the
// client uses http.DefaultClient.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		if hc != nil {
			c.httpClient = hc
		}
	}
}

// WithHeader adds a header to every request, e.g. "Authorization",
// "Bearer XXX". Header values are never logged.
func WithHeader(key, val string) Option {
	return func(c *Client) {
		if key == "" {
			return
		}
		c.headers = append(c.headers, headerPair{key: key, val: val})
	}
}

// New creates a Client for the given Streamable HTTP endpoint.
func New(endpoint string, opts ...Option) *Client {
	c := &Client{
		endpoint:   endpoint,
		httpClient: http.DefaultClient,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Initialize performs the MCP handshake: it sends an `initialize`
// request, captures the Mcp-Session-Id header from the response, and
// then sends the `notifications/initialized` notification.
func (c *Client) Initialize(ctx context.Context, name, version string) error {
	if name == "" {
		name = "crewai-go"
	}
	if version == "" {
		version = "0.0.0"
	}

	params := map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    name,
			"version": version,
		},
	}
	var raw json.RawMessage
	if err := c.call(ctx, "initialize", params, &raw); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}

	// Send the notifications/initialized notification. JSON-RPC
	// notifications have no `id` field and return no response body.
	if err := c.notify(ctx, "notifications/initialized", nil); err != nil {
		return fmt.Errorf("notifications/initialized: %w", err)
	}
	return nil
}

// Close ends the MCP session if one is active. It sends an HTTP DELETE
// with the Mcp-Session-Id header (Streamable HTTP session teardown) and
// then forgets the local session id. Close is idempotent and is a no-op
// on a client that was never initialized. Transport errors are returned
// but the local session is cleared either way so a later Close is a no-op.
// Header values are never logged.
func (c *Client) Close(ctx context.Context) error {
	c.sessionMu.Lock()
	sid := c.sessionID
	c.sessionID = ""
	c.sessionMu.Unlock()
	if sid == "" {
		return nil
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.endpoint, nil)
	if err != nil {
		return fmt.Errorf("close session: %w", err)
	}
	httpReq.Header.Set("Mcp-Session-Id", sid)
	for _, h := range c.headers {
		httpReq.Header.Set(h.key, h.val)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("close session: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 512))
	// 404/405: server has no session resource to delete. Anything
	// else non-2xx is reported but the local id is already gone.
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("close session: HTTP %d", resp.StatusCode)
	}
	return nil
}

// Tool is an MCP tool descriptor returned by tools/list. InputSchema
// is the JSON Schema the tool accepts and must be preserved verbatim
// for native function calling (see SchemaProvider).
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// listToolsResult mirrors the JSON-RPC `tools/list` payload shape
// (subset we depend on).
type listToolsResult struct {
	Tools           []Tool `json:"tools"`
	NextCursor      string `json:"nextCursor,omitempty"`
	NextCursorField string `json:"next_cursor,omitempty"` // tolerate snake_case variant
}

// cursor returns whichever cursor field was populated.
func (r listToolsResult) cursor() string {
	if r.NextCursor != "" {
		return r.NextCursor
	}
	return r.NextCursorField
}

// ListTools returns the full tool catalog, transparently paginated
// via cursor. An empty catalog returns an empty slice and a nil error.
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	var all []Tool
	var cursor string
	for pageNum := 0; pageNum < maxListToolsPages; pageNum++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var page listToolsResult
		if err := c.call(ctx, "tools/list", params, &page); err != nil {
			return nil, fmt.Errorf("tools/list: %w", err)
		}
		all = append(all, page.Tools...)
		next := page.cursor()
		if next == "" {
			return all, nil
		}
		cursor = next
	}
	return nil, fmt.Errorf("tools/list: exceeded %d pages", maxListToolsPages)
}

// ContentBlock is one item inside a CallToolResult. Only `text` fields
// are surfaced by the adapter; other types are passed through as
// empty strings.
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// CallToolResult is the response from tools/call. Per the MCP spec,
// protocol-level errors (HTTP / JSON-RPC faults) do not produce a
// CallToolResult — they surface as Go errors. A non-nil CallToolResult
// with `IsError == true` is a tool-level error and is NOT a Go error.
type CallToolResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError"`
}

// CallTool invokes tools/call on the MCP server.
func (c *Client) CallTool(ctx context.Context, name string, args json.RawMessage) (*CallToolResult, error) {
	if name == "" {
		return nil, fmt.Errorf("call tool: empty name")
	}
	var params map[string]any
	if len(args) > 0 {
		// Re-marshal RawMessage through json so non-object arguments
		// (e.g. arrays, scalars) are accepted as the spec allows.
		var decoded any
		if err := json.Unmarshal(args, &decoded); err != nil {
			return nil, fmt.Errorf("call tool: invalid args JSON: %w", err)
		}
		params = map[string]any{"name": name, "arguments": decoded}
	} else {
		params = map[string]any{"name": name}
	}
	var res CallToolResult
	if err := c.call(ctx, "tools/call", params, &res); err != nil {
		return nil, fmt.Errorf("tools/call %q: %w", name, err)
	}
	return &res, nil
}

// --- Transport ---------------------------------------------------------------

// jsonRPCRequest is the wire envelope used for both requests and
// notifications (omitting ID for notifications).
type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      uint64 `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// jsonRPCError is the standard JSON-RPC 2.0 error object.
type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// jsonRPCResponse is the standard envelope returned by the server.
// Either `Result` or `Error` is set; never both.
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      uint64          `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

// requestID increments atomically to give each call a unique id.
var requestID uint64

func nextRequestID() uint64 {
	return atomic.AddUint64(&requestID, 1)
}

// call sends a JSON-RPC request and decodes the result into out.
// Protocol errors (transport, HTTP non-2xx, JSON-RPC error) become
// Go errors.
func (c *Client) call(ctx context.Context, method string, params any, out any) error {
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      nextRequestID(),
		Method:  method,
		Params:  params,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	resp, err := c.do(ctx, body, false)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Drain a bounded prefix so the connection can be reused;
		// never include the body in the error — servers may echo
		// Authorization or other secrets.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 512))
		return fmt.Errorf("mcp: HTTP %d", resp.StatusCode)
	}

	payload, err := readJSONRPCPayload(resp)
	if err != nil {
		return err
	}
	var rpc jsonRPCResponse
	if err := json.Unmarshal(payload, &rpc); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if rpc.Error != nil {
		return fmt.Errorf("jsonrpc error %d: %s", rpc.Error.Code, rpc.Error.Message)
	}
	if out == nil {
		return nil
	}
	if len(rpc.Result) == 0 {
		return nil
	}
	return json.Unmarshal(rpc.Result, out)
}

// notify sends a JSON-RPC notification (no `id` field, no response
// expected). The server may return 202 Accepted or 204 No Content; any
// 2xx is accepted.
func (c *Client) notify(ctx context.Context, method string, params any) error {
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}
	resp, err := c.do(ctx, body, true)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("mcp: notification %q: HTTP %d", method, resp.StatusCode)
	}
	return nil
}

// do executes the HTTP POST. When asNotification is true, the
// Accept header advertises both JSON and SSE.
func (c *Client) do(ctx context.Context, body []byte, asNotification bool) (*http.Response, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
	for _, h := range c.headers {
		httpReq.Header.Set(h.key, h.val)
	}
	if asNotification {
		// Notifications may be answered with 202/204; ignore response
		// body. The Accept header advertises both formats so the server
		// can pick.
	}

	c.sessionMu.Lock()
	sid := c.sessionID
	c.sessionMu.Unlock()
	if sid != "" {
		httpReq.Header.Set("Mcp-Session-Id", sid)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("transport: %w", err)
	}

	// Capture the session id from initialize. The server sends it on
	// the FIRST response of the session only.
	c.sessionMu.Lock()
	if c.sessionID == "" {
		if got := resp.Header.Get("Mcp-Session-Id"); got != "" {
			c.sessionID = got
		}
	}
	c.sessionMu.Unlock()

	// Bound every response body before the caller sees it. A second
	// LimitReader further down the stack is then a no-op relative to
	// this cap.
	resp.Body = &cappedReadCloser{Reader: readLimited(resp.Body), Closer: resp.Body}

	return resp, nil
}

// cappedReadCloser pairs a LimitReader with the original Body closer
// so Close still releases the HTTP connection.
type cappedReadCloser struct {
	io.Reader
	io.Closer
}

// readJSONRPCPayload extracts a single JSON-RPC envelope from resp.
// When Content-Type is text/event-stream the first complete SSE data
// block is used; otherwise the (already capped) body is read as JSON.
// Only `data:` lines are considered — comments, event names, and ids
// are ignored and never interpreted.
func readJSONRPCPayload(resp *http.Response) ([]byte, error) {
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.Contains(ct, "text/event-stream") {
		scan := bufio.NewScanner(resp.Body)
		scan.Buffer(make([]byte, 0, 64*1024), MaxMCPResponseBytes)
		raw, ok, err := parseSSEChunk(scan)
		if err != nil {
			return nil, fmt.Errorf("decode sse: %w", err)
		}
		if !ok {
			return nil, fmt.Errorf("decode sse: empty event stream")
		}
		return raw, nil
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	return data, nil
}

// parseSSEChunk extracts the JSON payload from a single SSE chunk's
// lines. Lines starting with `data:` carry the payload; everything
// else (comments, event names, blanks) is ignored. Per the HTML5
// spec, multiple `data:` lines are joined with "\n", but MCP responses
// always emit a single complete JSON object per chunk.
func parseSSEChunk(scan *bufio.Scanner) (json.RawMessage, bool, error) {
	var data []string
	for scan.Scan() {
		line := scan.Text()
		if line == "" {
			if len(data) > 0 {
				return json.RawMessage(strings.Join(data, "\n")), true, nil
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			// SSE comment; ignore.
			continue
		}
		if strings.HasPrefix(line, "data:") {
			rest := strings.TrimPrefix(line, "data:")
			rest = strings.TrimPrefix(rest, " ")
			data = append(data, rest)
			continue
		}
		// Other fields (event:, id:, retry:) — ignored; we only need
		// the data block.
	}
	if err := scan.Err(); err != nil {
		return nil, false, err
	}
	if len(data) > 0 {
		return json.RawMessage(strings.Join(data, "\n")), true, nil
	}
	return nil, false, nil
}

// readLimited wraps a Reader with MaxMCPResponseBytes. The remainder,
// if any, is silently dropped to prevent OOM on malicious servers.
func readLimited(r io.Reader) io.Reader {
	return io.LimitReader(r, MaxMCPResponseBytes)
}
