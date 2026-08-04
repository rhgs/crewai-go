package anthropic_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rhgs/crewai-go"
	"github.com/rhgs/crewai-go/llm/anthropic"
)

func TestClientCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "k" {
			t.Errorf("x-api-key = %q", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") == "" {
			t.Error("anthropic-version missing")
		}
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		// The system prompt must be separate from the messages.
		if req["system"] != "instructions" {
			t.Errorf("system = %v", req["system"])
		}
		msgs, _ := req["messages"].([]any)
		if len(msgs) != 1 {
			t.Errorf("expected 1 message, got %d", len(msgs))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"Claude response"}]}`))
	}))
	defer srv.Close()

	c := anthropic.New("claude-sonnet-5",
		anthropic.WithAPIKey("k"),
		anthropic.WithBaseURL(srv.URL),
	)
	out, err := c.Call(context.Background(), []crewai.Message{
		crewai.SystemMessage("instructions"),
		crewai.UserMessage("hi"),
	})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if out != "Claude response" {
		t.Errorf("out = %q", out)
	}
}

func TestClientMissingKey(t *testing.T) {
	c := anthropic.New("claude-sonnet-5", anthropic.WithAPIKey(""))
	if _, err := c.Call(context.Background(), nil); err == nil {
		t.Error("expected an error for a missing key")
	}
}
