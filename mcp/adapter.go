package mcp

import (
	"context"
	"encoding/json"
	"strings"

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
}

// NewToolAdapter builds an adapter for one MCP tool. The Client is the
// connection returned by LoadConfig or by New/Initialize directly.
func NewToolAdapter(c *Client, tool Tool) *ToolAdapter {
	return &ToolAdapter{client: c, tool: tool}
}

// Name implements crewai.Tool.
func (a *ToolAdapter) Name() string { return a.tool.Name }

// Description implements crewai.Tool.
func (a *ToolAdapter) Description() string { return a.tool.Description }

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
// and concatenates the text of every `content` block whose Type is
// "text". A `isError: true` response is NOT a Go error: the tool's
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

// Compile-time check that ToolAdapter satisfies crewai.Tool and the
// optional SchemaProvider interface.
var (
	_ crewai.Tool           = (*ToolAdapter)(nil)
	_ crewai.SchemaProvider = (*ToolAdapter)(nil)
)
