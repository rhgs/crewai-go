package openai_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rhgs/crewai-go"
	"github.com/rhgs/crewai-go/llm/openai"
)

func TestClientCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		if req["model"] != "gpt-test" {
			t.Errorf("model = %v", req["model"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"hello from mock"}}]}`))
	}))
	defer srv.Close()

	c := openai.New("gpt-test",
		openai.WithAPIKey("test-key"),
		openai.WithBaseURL(srv.URL),
	)
	if c.Model() != "gpt-test" {
		t.Errorf("Model() = %q", c.Model())
	}

	out, err := c.Call(context.Background(), []crewai.Message{crewai.UserMessage("hi")})
	if err != nil {
		t.Fatalf("Call error: %v", err)
	}
	if out != "hello from mock" {
		t.Errorf("out = %q", out)
	}
}

func TestClientAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid model","type":"invalid_request"}}`))
	}))
	defer srv.Close()

	c := openai.New("x", openai.WithAPIKey("k"), openai.WithBaseURL(srv.URL))
	_, err := c.Call(context.Background(), []crewai.Message{crewai.UserMessage("hi")})
	if err == nil || !strings.Contains(err.Error(), "invalid model") {
		t.Errorf("error = %v", err)
	}
}

func TestClientMissingKey(t *testing.T) {
	c := openai.New("x", openai.WithAPIKey(""))
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
		// OpenAI returns arguments as a STRING, not an object.
		_, _ = w.Write([]byte(`{
			"choices": [{
				"message": {
					"role": "assistant",
					"content": "",
					"tool_calls": [{
						"id": "call_abc",
						"type": "function",
						"function": {"name": "calculator", "arguments": "{\"expr\": \"2+2\"}"}
					}]
				}
			}]
		}`))
	}))
	defer srv.Close()

	c := openai.New("gpt-test", openai.WithAPIKey("k"), openai.WithBaseURL(srv.URL))
	tools := []crewai.ToolSpec{
		{Type: "function", Function: crewai.ToolFunction{Name: "calculator", Description: "calc", Parameters: json.RawMessage(`{"type":"object"}`)}},
	}
	resp, err := c.CallWithTools(context.Background(), []crewai.Message{
		crewai.UserMessage("calc 2+2"),
	}, tools)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("toolCalls = %d, want 1", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].ID != "call_abc" {
		t.Errorf("id = %q", resp.ToolCalls[0].ID)
	}
	if resp.ToolCalls[0].Function.Name != "calculator" {
		t.Errorf("name = %q", resp.ToolCalls[0].Function.Name)
	}
	// Arguments should be normalized to json.RawMessage.
	var args map[string]any
	if err := json.Unmarshal(resp.ToolCalls[0].Function.Arguments, &args); err != nil {
		t.Errorf("arguments not valid JSON: %v", err)
	}
	if args["expr"] != "2+2" {
		t.Errorf("args expr = %v", args["expr"])
	}
}

func TestCallWithTools_NoToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"final answer"}}]}`))
	}))
	defer srv.Close()

	c := openai.New("gpt-test", openai.WithAPIKey("k"), openai.WithBaseURL(srv.URL))
	resp, err := c.CallWithTools(context.Background(), []crewai.Message{
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

func TestCallWithTools_ArgumentsNotJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"choices": [{
				"message": {
					"role": "assistant",
					"content": "",
					"tool_calls": [{
						"id": "call_1",
						"type": "function",
						"function": {"name": "tool", "arguments": "not valid json"}
					}]
				}
			}]
		}`))
	}))
	defer srv.Close()

	c := openai.New("gpt-test", openai.WithAPIKey("k"), openai.WithBaseURL(srv.URL))
	resp, err := c.CallWithTools(context.Background(), []crewai.Message{crewai.UserMessage("hi")}, nil)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("toolCalls = %d, want 1", len(resp.ToolCalls))
	}
	// Best-effort: raw bytes should be kept.
	if string(resp.ToolCalls[0].Function.Arguments) != "not valid json" {
		t.Errorf("args = %q", string(resp.ToolCalls[0].Function.Arguments))
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

	c := openai.New("gpt-test", openai.WithAPIKey("k"), openai.WithBaseURL(srv.URL))
	_, err := c.CallWithTools(context.Background(), nil, nil)
	if err == nil {
		t.Error("expected error for oversized response")
	}
}
