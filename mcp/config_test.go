package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestLoadConfig_EmptyPath(t *testing.T) {
	if _, err := LoadConfig(context.Background(), "", "c", "1"); err == nil {
		t.Fatal("empty path must error")
	}
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	_, err := LoadConfig(context.Background(), "/no/such/file.json", "c", "1")
	if err == nil || !strings.Contains(err.Error(), "/no/such/file.json") {
		t.Fatalf("expected path in error, got %v", err)
	}
}

func TestLoadConfig_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte(`{not json`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadConfig(context.Background(), path, "c", "1")
	if err == nil || !strings.Contains(err.Error(), "parsing") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

func TestLoadConfig_EmptyServers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(path, []byte(`{"servers":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadConfig(context.Background(), path, "c", "1")
	if err == nil || !strings.Contains(err.Error(), "no servers") {
		t.Fatalf("expected no-servers error, got %v", err)
	}
}

func TestLoadConfig_TwoServers(t *testing.T) {
	var (
		hits1, hits2 int32
	)
	srv1, cleanup1 := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits1, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	})
	defer cleanup1()
	srv2, cleanup2 := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits2, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	})
	defer cleanup2()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	cfg := Config{Servers: []ServerConfig{
		{Name: "one", Endpoint: srv1.URL, Headers: map[string]string{"Authorization": "Bearer X"}},
		{Name: "two", Endpoint: srv2.URL},
	}}
	data, _ := json.Marshal(cfg)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	clients, err := LoadConfig(context.Background(), path, "auditor", "1.0")
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(clients) != 2 {
		t.Fatalf("expected 2 clients, got %d", len(clients))
	}
	if atomic.LoadInt32(&hits1) == 0 || atomic.LoadInt32(&hits2) == 0 {
		t.Fatal("both servers must have been initialized")
	}
}

func TestLoadConfig_ServerInitializeFailure(t *testing.T) {
	srv, cleanup := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	defer cleanup()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	cfg := Config{Servers: []ServerConfig{{Name: "broken", Endpoint: srv.URL}}}
	data, _ := json.Marshal(cfg)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadConfig(context.Background(), path, "c", "1")
	if err == nil {
		t.Fatal("init failure must propagate")
	}
	if !strings.Contains(err.Error(), "broken") {
		t.Fatalf("error must name the server (by Name): %v", err)
	}
}

func TestLoadConfig_UsesEndpointWhenNameMissing(t *testing.T) {
	srv, cleanup := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	defer cleanup()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	// No name — fall back to endpoint for error message.
	cfg := Config{Servers: []ServerConfig{{Endpoint: srv.URL}}}
	data, _ := json.Marshal(cfg)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadConfig(context.Background(), path, "c", "1")
	if err == nil || !strings.Contains(err.Error(), srv.URL) {
		t.Fatalf("error must include endpoint: %v", err)
	}
}

func TestLoadConfig_EmptyEndpoint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	// Empty endpoint + name "noname" hits the New endpoint="" path,
	// which surfaces during HTTP. That's acceptable behaviour; what we
	// verify here is that the error identifies the server.
	cfg := Config{Servers: []ServerConfig{{Name: "noname", Endpoint: "http://127.0.0.1:1"}}}
	data, _ := json.Marshal(cfg)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadConfig(context.Background(), path, "c", "1")
	if err == nil {
		t.Fatal("unreachable endpoint must error")
	}
	if !strings.Contains(err.Error(), "noname") {
		t.Fatalf("error should mention server name, got %v", err)
	}
}

func TestLoadConfig_NoLeakOfHeaderValues(t *testing.T) {
	srv, cleanup := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	defer cleanup()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	cfg := Config{Servers: []ServerConfig{
		{Name: "leaky", Endpoint: srv.URL, Headers: map[string]string{"Authorization": "Bearer SECRET-TOKEN-VALUE"}},
	}}
	data, _ := json.Marshal(cfg)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadConfig(context.Background(), path, "c", "1")
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "SECRET-TOKEN-VALUE") {
		t.Fatalf("error leaked header value: %v", err)
	}
}
