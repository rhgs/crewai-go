package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
)

// Config is the top-level structure of an MCP configuration JSON file.
// Servers are independent connections; LoadConfig initializes each one
// and returns them in declaration order.
type Config struct {
	Servers []ServerConfig `json:"servers"`
}

// ServerConfig describes a single MCP server connection. Headers are sent
// on every request to this server; typical use is
// `{"Authorization": "Bearer ..."}`.
type ServerConfig struct {
	Name     string            `json:"name"`
	Endpoint string            `json:"endpoint"`
	Headers  map[string]string `json:"headers,omitempty"`
}

// LoadConfig reads a JSON MCP configuration file from the given path and
// returns one *Client per server entry, already initialized.
//
// The path is supplied by the caller — the framework never searches for
// configuration files implicitly. Errors from Initialize name the
// server (by Name when set, else by Endpoint) but never include header
// values or file contents. An empty Endpoint is rejected before any
// network call, identified by Name. If Initialize fails for a later
// server, clients already initialized are Closed before the error
// returns so their MCP sessions are not abandoned.
func LoadConfig(ctx context.Context, path, clientName, clientVersion string) ([]*Client, error) {
	if path == "" {
		return nil, fmt.Errorf("mcp: config path is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("mcp: reading config %q: %w", path, err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("mcp: parsing config %q: %w", path, err)
	}
	if len(cfg.Servers) == 0 {
		return nil, fmt.Errorf("mcp: no servers in config file %q", path)
	}

	clients := make([]*Client, 0, len(cfg.Servers))
	for i, sc := range cfg.Servers {
		identity := sc.Name
		if identity == "" {
			identity = sc.Endpoint
		}
		if identity == "" {
			identity = fmt.Sprintf("servers[%d]", i)
		}
		if sc.Endpoint == "" {
			return nil, fmt.Errorf("mcp: server %q: empty endpoint", identity)
		}

		opts := []Option{}
		for k, v := range sc.Headers {
			opts = append(opts, WithHeader(k, v))
		}

		c := New(sc.Endpoint, opts...)
		if err := c.Initialize(ctx, clientName, clientVersion); err != nil {
			// Tear down sessions already opened so a mid-list
			// failure does not leak MCP sessions on earlier
			// servers. Close errors are secondary.
			for _, prev := range clients {
				_ = prev.Close(ctx)
			}
			return nil, fmt.Errorf("mcp: initialize server %q: %w", identity, err)
		}
		clients = append(clients, c)
	}
	return clients, nil
}
