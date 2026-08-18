# MCP (Model Context Protocol) support

crewai-go can connect to external [Model Context Protocol](https://modelcontextprotocol.io/)
servers over Streamable HTTP (JSON-RPC 2.0) and expose the server's tool
catalog as `crewai.Tool` values. The original `inputSchema` for each tool is
preserved so that native function calling receives the real schema instead of
a placeholder.

The package is `mcp/` and is stdlib-only (no new dependencies).

## Two ways to configure servers

Both forms require the caller to supply endpoint details programmatically —
the framework never searches for config files implicitly.

### Form A — programmatic construction

```go
client := mcp.New("https://mcp.example.com/sse",
    mcp.WithHeader("Authorization", "Bearer "+token),
)
if err := client.Initialize(ctx, "my-auditor", "1.0.0"); err != nil {
    return err
}
tools, err := client.ListTools(ctx)
// turn each tool into a crewai.Tool
var crewTools []crewai.Tool
for _, t := range tools {
    crewTools = append(crewTools, mcp.NewToolAdapter(client, t))
}
```

### Form B — JSON configuration file

`mcp.LoadConfig` reads a JSON file, creates one client per server entry, and
initializes them. The file path is supplied by the caller.

```json
{
  "servers": [
    {
      "name": "contract-data",
      "endpoint": "https://mcp.example.com/sse",
      "headers": { "Authorization": "Bearer TOKEN_HERE" }
    },
    {
      "name": "procurement-db",
      "endpoint": "https://mcp2.example.com/sse",
      "headers": {}
    }
  ]
}
```

```go
clients, err := mcp.LoadConfig(ctx, "/etc/auditor/mcp-servers.json",
    "my-auditor", "1.0.0")
if err != nil { return err }
var crewTools []crewai.Tool
for _, c := range clients {
    tools, err := c.ListTools(ctx)
    if err != nil { return err }
    for _, t := range tools {
        crewTools = append(crewTools, mcp.NewToolAdapter(c, t))
    }
}
```

The configuration file may contain secrets; protect it with permission `0600`.

## Tool adapter behaviour

`mcp.NewToolAdapter` returns a `*ToolAdapter` that implements both
`crewai.Tool` and (optionally) `crewai.SchemaProvider`:

- `Name()` returns the MCP tool name.
- `Description()` returns the MCP tool description.
- `Schema()` returns the JSON Schema from `inputSchema` — preserved verbatim
  for native tool calling.
- `Call(ctx, input)` invokes `tools/call` and concatenates the text of every
  `content` block whose `Type` is `"text"`.

**Tool errors vs protocol errors.** A response with `isError: true` is NOT a
Go error: the adapter returns the text content (prefixed with
`[tool error]`) so the model can observe and recover. Only HTTP or JSON-RPC
failures surface as Go errors.

## Options

- `mcp.WithHTTPClient(*http.Client)` — defaults to `http.DefaultClient`.
- `mcp.WithHeader(key, val)` — added to every request. Header values are never
  logged.

## Security

- All response bodies are read with `io.LimitReader` (16 MiB cap).
- Headers configured via `WithHeader` and `Mcp-Session-Id` are never logged.
- SSE parsing extracts only `data:` lines; nothing is interpreted or executed.
- `LoadConfig` errors name the failing server (by `Name`) but never include
  header values or file contents.

## Coverage

Both `client_test.go`, `config_test.go`, and `adapter_test.go` run under
`go test -race ./mcp/...` and cover >90% of statements.
