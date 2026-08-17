package ollama_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rhgs/crewai-go"
	"github.com/rhgs/crewai-go/llm/ollama"
)

func TestLocalCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("path = %q", r.URL.Path)
		}
		// Local must not send Authorization.
		if r.Header.Get("Authorization") != "" {
			t.Errorf("local should not send Authorization: %q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"hello local"},"done":true}`))
	}))
	defer srv.Close()

	llm := ollama.New("llama3.2", ollama.WithBaseURL(srv.URL))
	if llm.Model() != "llama3.2" {
		t.Errorf("Model() = %q", llm.Model())
	}
	out, err := llm.Call(context.Background(), []crewai.Message{crewai.UserMessage("hi")})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if out != "hello local" {
		t.Errorf("out = %q", out)
	}
}

func TestCloudCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer cloud-key" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"hello cloud"},"done":true}`))
	}))
	defer srv.Close()

	llm := ollama.NewCloud("gpt-oss:120b",
		ollama.WithBaseURL(srv.URL),
		ollama.WithAPIKey("cloud-key"),
	)
	out, err := llm.Call(context.Background(), []crewai.Message{crewai.UserMessage("hi")})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if out != "hello cloud" {
		t.Errorf("out = %q", out)
	}
}

func TestCloudRequiresKey(t *testing.T) {
	// Without WithBaseURL, it points to the real cloud; with no key it must
	// fail before hitting the network.
	llm := ollama.NewCloud("x", ollama.WithAPIKey(""))
	if _, err := llm.Call(context.Background(), nil); err == nil {
		t.Error("expected an error for a missing cloud token")
	}
}

func TestAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"error":"model not found"}`))
	}))
	defer srv.Close()

	llm := ollama.New("x", ollama.WithBaseURL(srv.URL))
	if _, err := llm.Call(context.Background(), []crewai.Message{crewai.UserMessage("hi")}); err == nil {
		t.Error("expected an API error")
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
		_, _ = w.Write([]byte(`{
			"message": {
				"role": "assistant",
				"content": "",
				"tool_calls": [
					{"function": {"name": "calculator", "arguments": {"expr": "2+2"}}}
				]
			},
			"done": true
		}`))
	}))
	defer srv.Close()

	llm := ollama.New("llama3.2", ollama.WithBaseURL(srv.URL))
	tools := []crewai.ToolSpec{
		{Type: "function", Function: crewai.ToolFunction{Name: "calculator", Description: "calc", Parameters: json.RawMessage(`{"type":"object"}`)}},
	}
	resp, err := llm.CallWithTools(context.Background(), []crewai.Message{
		crewai.UserMessage("calc 2+2"),
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
	// Ollama returns arguments as object JSON, so it should be valid json.RawMessage.
	var args map[string]any
	if err := json.Unmarshal(resp.ToolCalls[0].Function.Arguments, &args); err != nil {
		t.Errorf("arguments not valid JSON: %v", err)
	}
}

func TestCallWithTools_NoToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"final answer"},"done":true}`))
	}))
	defer srv.Close()

	llm := ollama.New("llama3.2", ollama.WithBaseURL(srv.URL))
	resp, err := llm.CallWithTools(context.Background(), []crewai.Message{
		crewai.UserMessage("hi"),
	}, nil)
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
		// Write more than MaxProviderResponseBytes (10 MiB).
		w.Header().Set("Content-Type", "application/json")
		chunk := strings.Repeat("x", 4096)
		for i := 0; i < 3000; i++ { // ~12 MiB
			_, _ = w.Write([]byte(chunk))
		}
	}))
	defer srv.Close()

	llm := ollama.New("llama3.2", ollama.WithBaseURL(srv.URL))
	_, err := llm.CallWithTools(context.Background(), nil, nil)
	if err == nil {
		t.Error("expected error for oversized response")
	}
}
