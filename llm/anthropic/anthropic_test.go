package anthropic_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestCallWithTools(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		if _, ok := req["tools"]; !ok {
			t.Error("request should include tools field")
		}
		w.Header().Set("Content-Type", "application/json")
		// Anthropic returns tool_use as content blocks with input as object.
		_, _ = w.Write([]byte(`{
			"content": [
				{"type": "text", "text": "Let me calculate that."},
				{"type": "tool_use", "id": "tool_1", "name": "calculator", "input": {"expr": "2+2"}}
			]
		}`))
	}))
	defer srv.Close()

	c := anthropic.New("claude-sonnet-5", anthropic.WithAPIKey("k"), anthropic.WithBaseURL(srv.URL))
	tools := []crewai.ToolSpec{
		{Type: "function", Function: crewai.ToolFunction{Name: "calculator", Description: "calc", Parameters: json.RawMessage(`{"type":"object"}`)}},
	}
	resp, err := c.CallWithTools(context.Background(), []crewai.Message{
		crewai.UserMessage("calc 2+2"),
	}, tools)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if resp.Content != "Let me calculate that." {
		t.Errorf("content = %q", resp.Content)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("toolCalls = %d, want 1", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].ID != "tool_1" {
		t.Errorf("id = %q", resp.ToolCalls[0].ID)
	}
	if resp.ToolCalls[0].Function.Name != "calculator" {
		t.Errorf("name = %q", resp.ToolCalls[0].Function.Name)
	}
	// Input is an object, so it should be valid json.RawMessage.
	var input map[string]any
	if err := json.Unmarshal(resp.ToolCalls[0].Function.Arguments, &input); err != nil {
		t.Errorf("arguments not valid JSON: %v", err)
	}
	if input["expr"] != "2+2" {
		t.Errorf("input expr = %v", input["expr"])
	}
}

func TestCallWithTools_MultipleToolUse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"content": [
				{"type": "tool_use", "id": "t1", "name": "calc", "input": {"a": 1}},
				{"type": "tool_use", "id": "t2", "name": "time", "input": {}}
			]
		}`))
	}))
	defer srv.Close()

	c := anthropic.New("claude", anthropic.WithAPIKey("k"), anthropic.WithBaseURL(srv.URL))
	resp, err := c.CallWithTools(context.Background(), []crewai.Message{crewai.UserMessage("hi")}, nil)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(resp.ToolCalls) != 2 {
		t.Fatalf("toolCalls = %d, want 2", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Function.Name != "calc" {
		t.Errorf("toolCalls[0] name = %q", resp.ToolCalls[0].Function.Name)
	}
	if resp.ToolCalls[1].Function.Name != "time" {
		t.Errorf("toolCalls[1] name = %q", resp.ToolCalls[1].Function.Name)
	}
}

func TestCallWithTools_NoToolUse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"final answer"}]}`))
	}))
	defer srv.Close()

	c := anthropic.New("claude", anthropic.WithAPIKey("k"), anthropic.WithBaseURL(srv.URL))
	resp, err := c.CallWithTools(context.Background(), []crewai.Message{crewai.UserMessage("hi")}, nil)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if resp.Content != "final answer" {
		t.Errorf("content = %q", resp.Content)
	}
	if len(resp.ToolCalls) != 0 {
		t.Errorf("toolCalls = %d, want 0", len(resp.ToolCalls))
	}
}

func TestCallWithTools_ResponseTooLarge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunk := strings.Repeat("x", 4096)
		for i := 0; i < 3000; i++ {
			_, _ = w.Write([]byte(chunk))
		}
	}))
	defer srv.Close()

	c := anthropic.New("claude", anthropic.WithAPIKey("k"), anthropic.WithBaseURL(srv.URL))
	_, err := c.CallWithTools(context.Background(), nil, nil)
	if err == nil {
		t.Error("expected error for oversized response")
	}
}
