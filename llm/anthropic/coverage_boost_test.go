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

func TestCallWithTools_EmptyParamsAndToolNameFallback(t *testing.T) {
	var sawBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &sawBody)
		_, _ = w.Write([]byte(`{
			"content": [
				{"type":"text","text":"ok"},
				{"type":"tool_use","id":"t1","name":"calc","input":{"n":1}},
				{"type":"something_else"}
			]
		}`))
	}))
	defer srv.Close()

	c := anthropic.New("m", anthropic.WithAPIKey("k"), anthropic.WithBaseURL(srv.URL))
	resp, err := c.CallWithTools(context.Background(), []crewai.Message{
		crewai.SystemMessage("sys"),
		crewai.UserMessage("hi"),
		{
			Role:    crewai.RoleAssistant,
			Content: "thinking",
			ToolCalls: []crewai.ToolCall{{
				ID: "prev",
				Function: crewai.ToolCallFunction{
					Name:      "calc",
					Arguments: nil,
				},
			}},
		},
		{Role: crewai.RoleTool, Content: "2", ToolName: "calc"},
	}, []crewai.ToolSpec{{
		Type: "function",
		Function: crewai.ToolFunction{
			Name:        "calc",
			Description: "c",
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "ok" {
		t.Fatalf("content=%q", resp.Content)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].ID != "t1" {
		t.Fatalf("toolCalls=%+v", resp.ToolCalls)
	}

	tools, _ := sawBody["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools=%v", sawBody["tools"])
	}
	tdef, _ := tools[0].(map[string]any)
	schema, _ := tdef["input_schema"].(map[string]any)
	if schema["type"] != "object" {
		t.Fatalf("default schema missing: %v", tdef["input_schema"])
	}
	msgs, _ := sawBody["messages"].([]any)
	found := false
	for _, m := range msgs {
		mm, _ := m.(map[string]any)
		if mm["role"] != "user" {
			continue
		}
		parts, ok := mm["content"].([]any)
		if !ok {
			continue
		}
		for _, p := range parts {
			pm, _ := p.(map[string]any)
			if pm["type"] == "tool_result" && pm["tool_use_id"] == "calc" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("expected tool_result with tool_use_id=calc in %v", msgs)
	}
}

func TestCallWithTools_EmptyToolsSlice(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"done"}]}`))
	}))
	defer srv.Close()
	c := anthropic.New("m", anthropic.WithAPIKey("k"), anthropic.WithBaseURL(srv.URL))
	resp, err := c.CallWithTools(context.Background(), []crewai.Message{
		crewai.UserMessage("x"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "done" || len(resp.ToolCalls) != 0 {
		t.Fatalf("%+v", resp)
	}
}

func TestCallWithTools_DecodeErrorAndNetwork(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer srv.Close()
	c := anthropic.New("m", anthropic.WithAPIKey("k"), anthropic.WithBaseURL(srv.URL))
	if _, err := c.CallWithTools(context.Background(), []crewai.Message{crewai.UserMessage("x")}, nil); err == nil {
		t.Fatal("expected decode error")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.CallWithTools(ctx, []crewai.Message{crewai.UserMessage("x")}, nil); err == nil {
		t.Fatal("expected ctx error")
	}
}

func TestWebSearch_SuccessClampAndSkipBlocks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"content": [
				{"type":"text","text":"thinking"},
				"not-an-object",
				{"type":"web_search_tool_result","tool_use_id":"w1","content":[
					{"type":"web_search_result","title":"A","url":"https://a.example","encrypted_content":"ca"},
					{"type":"other","title":"skip"},
					{"type":"web_search_result","title":"B","url":"https://b.example","encrypted_content":"cb"},
					{"type":"web_search_result","title":"C","url":"https://c.example","encrypted_content":"cc"}
				]},
				{"type":"web_search_tool_result","tool_use_id":"bad"}
			]
		}`))
	}))
	defer srv.Close()
	c := anthropic.New("m", anthropic.WithAPIKey("k"), anthropic.WithBaseURL(srv.URL))
	hits, err := c.WebSearch(context.Background(), "q", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("want 2 hits after clamp, got %d %+v", len(hits), hits)
	}
	if hits[0].Title != "A" || hits[1].Title != "B" {
		t.Fatalf("%+v", hits)
	}
}

func TestWebSearch_HTTPStatusOnBadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer srv.Close()
	c := anthropic.New("m", anthropic.WithAPIKey("k"), anthropic.WithBaseURL(srv.URL))
	_, err := c.WebSearch(context.Background(), "q", 3)
	if err == nil || !strings.Contains(err.Error(), "HTTP 502") {
		t.Fatalf("err=%v", err)
	}
}

func TestWebSearch_MalformedResultBlockSkipped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"content":[
			{"type":"web_search_tool_result","tool_use_id":"w1","content":"not-array"},
			{"type":"web_search_tool_result","tool_use_id":"w2","content":[
				{"type":"web_search_result","title":"Only","url":"https://o.example","encrypted_content":"x"}
			]}
		]}`))
	}))
	defer srv.Close()
	c := anthropic.New("m", anthropic.WithAPIKey("k"), anthropic.WithBaseURL(srv.URL))
	hits, err := c.WebSearch(context.Background(), "q", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Title != "Only" {
		t.Fatalf("%+v", hits)
	}
}
