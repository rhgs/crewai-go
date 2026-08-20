package openai_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rhgs/crewai-go"
	"github.com/rhgs/crewai-go/llm/openai"
)

type staticTS struct{ tok string }

func (s staticTS) Token(context.Context) (string, error) { return s.tok, nil }

type errTS struct{}

func (errTS) Token(context.Context) (string, error) { return "", errors.New("token boom") }

func TestOptionsAndTokenSource(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer dyn-token" {
			t.Errorf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer srv.Close()

	c := openai.New("m",
		openai.WithAPIKey("ignored-when-ts"),
		openai.WithBaseURL(srv.URL),
		openai.WithTemperature(0.2),
		openai.WithHTTPClient(&http.Client{Timeout: 5 * time.Second}),
		openai.WithTokenSource(staticTS{tok: "dyn-token"}),
	)
	if c.Model() != "m" {
		t.Fatalf("Model = %q", c.Model())
	}
	out, err := c.Call(context.Background(), []crewai.Message{crewai.UserMessage("hi")})
	if err != nil {
		t.Fatal(err)
	}
	if out != "ok" {
		t.Fatalf("out = %q", out)
	}
}

func TestTokenSourceError(t *testing.T) {
	c := openai.New("m", openai.WithTokenSource(errTS{}))
	if _, err := c.Call(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "token boom") {
		t.Fatalf("err = %v", err)
	}
	if _, err := c.CallWithTools(context.Background(), nil, nil); err == nil {
		t.Fatal("expected CallWithTools auth error")
	}
	if _, err := c.WebSearch(context.Background(), "q", 3); err == nil {
		t.Fatal("expected WebSearch auth error")
	}
}

func TestCallNoChoicesAndBadJSON(t *testing.T) {
	// empty choices
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}))
	defer srv.Close()
	c := openai.New("m", openai.WithAPIKey("k"), openai.WithBaseURL(srv.URL))
	if _, err := c.Call(context.Background(), []crewai.Message{crewai.UserMessage("x")}); err == nil || !strings.Contains(err.Error(), "no choices") {
		t.Fatalf("err = %v", err)
	}

	// non-JSON body
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer srv2.Close()
	c2 := openai.New("m", openai.WithAPIKey("k"), openai.WithBaseURL(srv2.URL))
	if _, err := c2.Call(context.Background(), []crewai.Message{crewai.UserMessage("x")}); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestCallWithTools_ErrorsAndToolMessages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		msgs, _ := req["messages"].([]any)
		if len(msgs) < 2 {
			t.Errorf("expected tool-bearing messages, got %d", len(msgs))
		}
		w.Header().Set("Content-Type", "application/json")
		// Invalid JSON arguments should still be accepted best-effort.
		_, _ = w.Write([]byte(`{
			"choices":[{
				"message":{
					"role":"assistant",
					"content":"done",
					"tool_calls":[{
						"id":"1",
						"type":"function",
						"function":{"name":"calc","arguments":"not-json"}
					}]
				}
			}]
		}`))
	}))
	defer srv.Close()

	c := openai.New("m", openai.WithAPIKey("k"), openai.WithBaseURL(srv.URL), openai.WithTemperature(0.1))
	msgs := []crewai.Message{
		crewai.UserMessage("use tool"),
		{
			Role: crewai.RoleAssistant,
			ToolCalls: []crewai.ToolCall{{
				ID: "1",
				Function: crewai.ToolCallFunction{
					Name:      "calc",
					Arguments: json.RawMessage(`{"x":1}`),
				},
			}},
		},
		{Role: crewai.RoleTool, Content: "2", ToolName: "calc", ToolCallID: "1"},
	}
	resp, err := c.CallWithTools(context.Background(), msgs, []crewai.ToolSpec{{
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
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Function.Name != "calc" {
		t.Fatalf("tool calls = %+v", resp.ToolCalls)
	}

	// API error path
	srvErr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"tools broken","type":"invalid_request"}}`))
	}))
	defer srvErr.Close()
	cErr := openai.New("m", openai.WithAPIKey("k"), openai.WithBaseURL(srvErr.URL))
	if _, err := cErr.CallWithTools(context.Background(), nil, nil); err == nil || !strings.Contains(err.Error(), "tools broken") {
		t.Fatalf("err = %v", err)
	}

	// empty choices -> empty response
	srvEmpty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}))
	defer srvEmpty.Close()
	cEmpty := openai.New("m", openai.WithAPIKey("k"), openai.WithBaseURL(srvEmpty.URL))
	resp, err = cEmpty.CallWithTools(context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp == nil || len(resp.ToolCalls) != 0 {
		t.Fatalf("resp = %+v", resp)
	}

	// unexpected status without error object
	srvStatus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}))
	defer srvStatus.Close()
	cStatus := openai.New("m", openai.WithAPIKey("k"), openai.WithBaseURL(srvStatus.URL))
	if _, err := cStatus.CallWithTools(context.Background(), nil, nil); err == nil || !strings.Contains(err.Error(), "unexpected status") {
		t.Fatalf("err = %v", err)
	}
}

func TestWebSearch_ErrorPaths(t *testing.T) {
	// API error
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"no search","type":"invalid_request"}}`))
	}))
	defer srv.Close()
	c := openai.New("m", openai.WithAPIKey("k"), openai.WithBaseURL(srv.URL))
	if _, err := c.WebSearch(context.Background(), "q", 0); err == nil || !strings.Contains(err.Error(), "no search") {
		t.Fatalf("err = %v", err)
	}

	// unexpected status
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`oops`))
	}))
	defer srv2.Close()
	c2 := openai.New("m", openai.WithAPIKey("k"), openai.WithBaseURL(srv2.URL))
	if _, err := c2.WebSearch(context.Background(), "q", 99); err == nil {
		t.Fatal("expected status error")
	}

	// bad JSON
	srv3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer srv3.Close()
	c3 := openai.New("m", openai.WithAPIKey("k"), openai.WithBaseURL(srv3.URL))
	if _, err := c3.WebSearch(context.Background(), "q", 3); err == nil {
		t.Fatal("expected decode error")
	}
}
