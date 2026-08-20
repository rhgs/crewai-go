package anthropic_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rhgs/crewai-go"
	"github.com/rhgs/crewai-go/llm/anthropic"
)

func TestOptionsAndModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req["max_tokens"] != float64(123) {
			t.Errorf("max_tokens = %v", req["max_tokens"])
		}
		if req["temperature"] != 0.1 {
			t.Errorf("temperature = %v", req["temperature"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"hi"}]}`))
	}))
	defer srv.Close()

	c := anthropic.New("claude-test",
		anthropic.WithAPIKey("k"),
		anthropic.WithBaseURL(srv.URL),
		anthropic.WithMaxTokens(123),
		anthropic.WithTemperature(0.1),
		anthropic.WithHTTPClient(&http.Client{Timeout: 5 * time.Second}),
	)
	if c.Model() != "claude-test" {
		t.Fatalf("Model = %q", c.Model())
	}
	out, err := c.Call(context.Background(), []crewai.Message{
		crewai.SystemMessage("sys"),
		crewai.UserMessage("u"),
		crewai.AssistantMessage("a"),
		{Role: crewai.RoleTool, Content: "tool-out", ToolName: "t"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out != "hi" {
		t.Fatalf("out = %q", out)
	}
}

func TestCallErrorPaths(t *testing.T) {
	c := anthropic.New("m", anthropic.WithAPIKey(""))
	if _, err := c.Call(context.Background(), nil); err == nil {
		t.Fatal("expected missing key")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"type":"invalid_request","message":"bad req"}}`))
	}))
	defer srv.Close()
	c2 := anthropic.New("m", anthropic.WithAPIKey("k"), anthropic.WithBaseURL(srv.URL))
	if _, err := c2.Call(context.Background(), []crewai.Message{crewai.UserMessage("x")}); err == nil || !strings.Contains(err.Error(), "bad req") {
		t.Fatalf("err = %v", err)
	}

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"content":[]}`))
	}))
	defer srv2.Close()
	c3 := anthropic.New("m", anthropic.WithAPIKey("k"), anthropic.WithBaseURL(srv2.URL))
	if _, err := c3.Call(context.Background(), []crewai.Message{crewai.UserMessage("x")}); err == nil || !strings.Contains(err.Error(), "unexpected status") {
		t.Fatalf("err = %v", err)
	}

	srv3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer srv3.Close()
	c4 := anthropic.New("m", anthropic.WithAPIKey("k"), anthropic.WithBaseURL(srv3.URL))
	if _, err := c4.Call(context.Background(), []crewai.Message{crewai.UserMessage("x")}); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestCallWithTools_Coverage(t *testing.T) {
	if _, err := anthropic.New("m", anthropic.WithAPIKey("")).CallWithTools(context.Background(), nil, nil); err == nil {
		t.Fatal("expected missing key")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"content":[
				{"type":"text","text":"thinking "},
				{"type":"tool_use","id":"tu1","name":"calc","input":{"x":1}},
				{"type":"text","text":"done"}
			]
		}`))
	}))
	defer srv.Close()

	c := anthropic.New("m", anthropic.WithAPIKey("k"), anthropic.WithBaseURL(srv.URL), anthropic.WithMaxTokens(256))
	resp, err := c.CallWithTools(context.Background(), []crewai.Message{
		crewai.SystemMessage("sys"),
		// Prior assistant turn that requested the tool, replayed with
		// ToolCalls so the client emits a tool_use content block.
		{
			Role:    crewai.RoleAssistant,
			Content: "prev",
			ToolCalls: []crewai.ToolCall{{
				ID: "tu1",
				Function: crewai.ToolCallFunction{
					Name:      "calc",
					Arguments: json.RawMessage(`{"x":1}`),
				},
			}},
		},
		// Tool result keyed by the tool_use id.
		{Role: crewai.RoleTool, Content: "2", ToolName: "calc", ToolCallID: "tu1"},
		crewai.UserMessage("again"),
	}, []crewai.ToolSpec{{
		Type: "function",
		Function: crewai.ToolFunction{
			Name:        "calc",
			Description: "calc",
			Parameters:  json.RawMessage(`{"type":"object"}`),
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "thinking done" {
		t.Fatalf("content = %q", resp.Content)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Function.Name != "calc" {
		t.Fatalf("toolCalls = %+v", resp.ToolCalls)
	}

	srvErr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"type":"x","message":"tools fail"}}`))
	}))
	defer srvErr.Close()
	cErr := anthropic.New("m", anthropic.WithAPIKey("k"), anthropic.WithBaseURL(srvErr.URL))
	if _, err := cErr.CallWithTools(context.Background(), nil, nil); err == nil || !strings.Contains(err.Error(), "tools fail") {
		t.Fatalf("err = %v", err)
	}

	srvStatus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"content":[]}`))
	}))
	defer srvStatus.Close()
	cStatus := anthropic.New("m", anthropic.WithAPIKey("k"), anthropic.WithBaseURL(srvStatus.URL))
	if _, err := cStatus.CallWithTools(context.Background(), nil, nil); err == nil {
		t.Fatal("expected status error")
	}
}

func TestWebSearch_Coverage(t *testing.T) {
	if _, err := anthropic.New("m", anthropic.WithAPIKey("")).WebSearch(context.Background(), "q", 3); err == nil {
		t.Fatal("expected missing key")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"type":"x","message":"search fail"}}`))
	}))
	defer srv.Close()
	c := anthropic.New("m", anthropic.WithAPIKey("k"), anthropic.WithBaseURL(srv.URL))
	if _, err := c.WebSearch(context.Background(), "q", 0); err == nil || !strings.Contains(err.Error(), "search fail") {
		t.Fatalf("err = %v", err)
	}

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"content":[]}`))
	}))
	defer srv2.Close()
	c2 := anthropic.New("m", anthropic.WithAPIKey("k"), anthropic.WithBaseURL(srv2.URL))
	if _, err := c2.WebSearch(context.Background(), "q", 99); err == nil {
		t.Fatal("expected status error")
	}

	srv3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer srv3.Close()
	c3 := anthropic.New("m", anthropic.WithAPIKey("k"), anthropic.WithBaseURL(srv3.URL))
	if _, err := c3.WebSearch(context.Background(), "q", 3); err == nil {
		t.Fatal("expected decode error")
	}
}
