package xai_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rhgs/crewai-go"
	"github.com/rhgs/crewai-go/llm/xai"
)

func chatServer(t *testing.T, wantAuth string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != wantAuth {
			t.Errorf("Authorization = %q, want %q", got, wantAuth)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"hello Grok"}}]}`))
	}))
}

func TestAPIKey(t *testing.T) {
	srv := chatServer(t, "Bearer xai-key")
	defer srv.Close()

	llm := xai.New("grok-4", xai.WithAPIKey("xai-key"), xai.WithBaseURL(srv.URL))
	if llm.Model() != "grok-4" {
		t.Errorf("Model() = %q", llm.Model())
	}
	out, err := llm.Call(context.Background(), []crewai.Message{crewai.UserMessage("hi")})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if out != "hello Grok" {
		t.Errorf("out = %q", out)
	}
}

// staticSource is a fixed TokenSource for testing.
type staticSource struct{ tok string }

func (s staticSource) Token(context.Context) (string, error) { return s.tok, nil }

func TestOAuthTokenSource(t *testing.T) {
	srv := chatServer(t, "Bearer oauth-access-token")
	defer srv.Close()

	llm := xai.NewWithOAuth("grok-4", staticSource{tok: "oauth-access-token"}, xai.WithBaseURL(srv.URL))
	out, err := llm.Call(context.Background(), []crewai.Message{crewai.UserMessage("hi")})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if out != "hello Grok" {
		t.Errorf("out = %q", out)
	}
}

func TestCallWithTools(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// xAI uses the same wire format as OpenAI.
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"choices": [{
				"message": {
					"role": "assistant",
					"content": "",
					"tool_calls": [{
						"id": "call_xai_1",
						"type": "function",
						"function": {"name": "calculator", "arguments": "{\"expr\": \"3+3\"}"}
					}]
				}
			}]
		}`))
	}))
	defer srv.Close()

	llm := xai.New("grok-4", xai.WithAPIKey("xai-key"), xai.WithBaseURL(srv.URL))
	tools := []crewai.ToolSpec{
		{Type: "function", Function: crewai.ToolFunction{Name: "calculator", Description: "calc", Parameters: json.RawMessage(`{"type":"object"}`)}},
	}
	resp, err := llm.CallWithTools(context.Background(), []crewai.Message{
		crewai.UserMessage("calc 3+3"),
	}, tools)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("toolCalls = %d, want 1", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Function.Name != "calculator" {
		t.Errorf("name = %q", resp.ToolCalls[0].Function.Name)
	}
	// OpenAI wire format: arguments as string, normalized to json.RawMessage.
	var args map[string]any
	if err := json.Unmarshal(resp.ToolCalls[0].Function.Arguments, &args); err != nil {
		t.Errorf("arguments not valid JSON: %v", err)
	}
	if args["expr"] != "3+3" {
		t.Errorf("args expr = %v", args["expr"])
	}
}

func TestCallWithTools_NoToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"final answer from Grok"}}]}`))
	}))
	defer srv.Close()

	llm := xai.New("grok-4", xai.WithAPIKey("xai-key"), xai.WithBaseURL(srv.URL))
	resp, err := llm.CallWithTools(context.Background(), []crewai.Message{crewai.UserMessage("hi")}, nil)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if resp.Content != "final answer from Grok" {
		t.Errorf("content = %q", resp.Content)
	}
	if len(resp.ToolCalls) != 0 {
		t.Errorf("toolCalls = %d, want 0", len(resp.ToolCalls))
	}
}
