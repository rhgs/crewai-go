package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rhgs/crewai-go"
)

// ToolAdapter wraps an MCP tool so it can be used as a crewai.Tool. The
// adapter holds a reference to the Client (shared, thread-safe) and the
// MCP Tool descriptor.
//
// The adapter implements crewai.SchemaProvider so the original
// inputSchema is preserved when used with native tool calling (the
// executor's toToolSpecs picks up Schema() in place of the empty
// object default).
type ToolAdapter struct {
	client *Client
	tool   Tool

	// descLimit, when > 0, caps Description() to that many runes after
	// stripping ASCII control characters. Zero means no limit and no
	// control-char stripping (backward compatible default).
	descLimit int
}

// AdapterOption configures a ToolAdapter.
type AdapterOption func(*ToolAdapter)

// WithDescriptionLimit caps the adapter Description() to n runes
// (Unicode code points). When n > 0, ASCII control characters are also
// stripped from the description before truncation. n <= 0 leaves the
// description unchanged (no strip, no truncate).
//
// This is a least-privilege / prompt-hygiene helper, not a jailbreak
// sanitizer. Prefer attaching only the tools each agent needs.
func WithDescriptionLimit(n int) AdapterOption {
	return func(a *ToolAdapter) {
		if n < 0 {
			n = 0
		}
		a.descLimit = n
	}
}

// NewToolAdapter builds an adapter for one MCP tool. The Client is the
// connection returned by LoadConfig or by New/Initialize directly.
// Optional AdapterOption values (e.g. WithDescriptionLimit) configure
// the adapter; omitting them preserves the previous default behavior.
func NewToolAdapter(c *Client, tool Tool, opts ...AdapterOption) *ToolAdapter {
	a := &ToolAdapter{client: c, tool: tool}
	for _, opt := range opts {
		if opt != nil {
			opt(a)
		}
	}
	return a
}

// FilterTools returns the subset of tools whose Name is present in
// allow. Order of tools is preserved. A nil or empty allow set yields
// an empty slice (deny by default when filtering). Tools with an empty
// name never match.
//
// Typical use after ListTools:
//
//	tools = mcp.FilterTools(tools, map[string]struct{}{
//	    "search_docs": {},
//	    "get_ticket":  {},
//	})
func FilterTools(tools []Tool, allow map[string]struct{}) []Tool {
	if len(tools) == 0 || len(allow) == 0 {
		return nil
	}
	out := make([]Tool, 0, len(allow))
	for _, t := range tools {
		if t.Name == "" {
			continue
		}
		if _, ok := allow[t.Name]; ok {
			out = append(out, t)
		}
	}
	return out
}

// Name implements crewai.Tool.
func (a *ToolAdapter) Name() string { return a.tool.Name }

// Description implements crewai.Tool. When WithDescriptionLimit was set
// with n > 0, ASCII control characters are stripped and the result is
// truncated to n runes.
func (a *ToolAdapter) Description() string {
	d := a.tool.Description
	if a.descLimit <= 0 {
		return d
	}
	d = stripASCIIControls(d)
	return truncateRunes(d, a.descLimit)
}

// Schema implements crewai.SchemaProvider. The returned raw JSON is
// the tool's inputSchema as the MCP server declared it; it is passed
// verbatim to the model as ToolFunction.Parameters when used in native
// tool calling mode.
func (a *ToolAdapter) Schema() json.RawMessage {
	if len(a.tool.InputSchema) == 0 {
		return nil
	}
	return a.tool.InputSchema
}

// Call implements crewai.Tool. It invokes the MCP tools/call method
// and concatenates the text of every content block whose Type is
// "text". A isError:true response is NOT a Go error: the tool's
// textual content is returned so the model can observe and recover.
func (a *ToolAdapter) Call(ctx context.Context, input string) (string, error) {
	res, err := a.client.CallTool(ctx, a.tool.Name, json.RawMessage(input))
	if err != nil {
		return "", err
	}
	if res == nil {
		return "", nil
	}
	var b strings.Builder
	for _, c := range res.Content {
		if c.Type == "text" {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(c.Text)
		}
	}
	out := b.String()
	if out == "" {
		// No text blocks — return a sentinel so the model can see that
		// the call completed but produced no text. If the server
		// indicated an error, surface that label explicitly.
		if res.IsError {
			return "tool reported an error without text content", nil
		}
		return "tool returned no text content", nil
	}
	if res.IsError {
		// Prefix so the model can tell this came back as a tool error,
		// not a normal observation.
		return "[tool error] " + out, nil
	}
	return out, nil
}

// stripASCIIControls removes bytes < 0x20 (except tab) and DEL (0x7f).
// Tab is kept so multi-word layouts in descriptions remain readable.
func stripASCIIControls(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '\t' {
			b.WriteRune(r)
			continue
		}
		if r < 0x20 || r == 0x7f {
			continue
		}
		// Also drop other Unicode control categories (Cc) for safety.
		if unicode.Is(unicode.Cc, r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// truncateRunes returns the first n runes of s. If s is shorter, it is
// returned unchanged. n must be > 0.
func truncateRunes(s string, n int) string {
	if n <= 0 || s == "" {
		return s
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	i := 0
	for pos := range s {
		if i == n {
			return s[:pos]
		}
		i++
	}
	return s
}

// Compile-time check that ToolAdapter satisfies crewai.Tool and the
// optional SchemaProvider interface.
var (
	_ crewai.Tool           = (*ToolAdapter)(nil)
	_ crewai.SchemaProvider = (*ToolAdapter)(nil)
)
