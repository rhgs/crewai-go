package ollama_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rhgs/crewai-go"
	"github.com/rhgs/crewai-go/llm/ollama"
)

func TestOptionsLocalAndCloud(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer cloud-key" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		opts, _ := req["options"].(map[string]any)
		if opts["temperature"] != 0.3 {
			t.Errorf("temperature = %v", opts["temperature"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"ok"},"done":true}`))
	}))
	defer srv.Close()

	c := ollama.New("m",
		ollama.WithBaseURL(srv.URL),
		ollama.WithTemperature(0.3),
		ollama.WithHTTPClient(&http.Client{Timeout: 5 * time.Second}),
		ollama.WithAPIKey("cloud-key"),
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

	// NewCloud without key against cloud URL path in Call.
	cloud := ollama.NewCloud("m")
	if _, err := cloud.Call(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "requires a token") {
		t.Fatalf("err = %v", err)
	}
}

func TestCallErrorPaths(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"error":"model missing"}`))
	}))
	defer srv.Close()
	c := ollama.New("m", ollama.WithBaseURL(srv.URL))
	if _, err := c.Call(context.Background(), []crewai.Message{crewai.UserMessage("x")}); err == nil || !strings.Contains(err.Error(), "model missing") {
		t.Fatalf("err = %v", err)
	}

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":{"content":""}}`))
	}))
	defer srv2.Close()
	c2 := ollama.New("m", ollama.WithBaseURL(srv2.URL))
	if _, err := c2.Call(context.Background(), []crewai.Message{crewai.UserMessage("x")}); err == nil || !strings.Contains(err.Error(), "unexpected status") {
		t.Fatalf("err = %v", err)
	}

	srv3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer srv3.Close()
	c3 := ollama.New("m", ollama.WithBaseURL(srv3.URL))
	if _, err := c3.Call(context.Background(), []crewai.Message{crewai.UserMessage("x")}); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestCallWithTools_Coverage(t *testing.T) {
	cloud := ollama.NewCloud("m")
	if _, err := cloud.CallWithTools(context.Background(), nil, nil); err == nil {
		t.Fatal("expected cloud token error")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"message":{
				"role":"assistant",
				"content":"using tools",
				"tool_calls":[{"function":{"name":"calc","arguments":{"x":1}}}]
			},
			"done":true
		}`))
	}))
	defer srv.Close()
	c := ollama.New("m", ollama.WithBaseURL(srv.URL), ollama.WithTemperature(0.2))
	resp, err := c.CallWithTools(context.Background(), []crewai.Message{
		crewai.UserMessage("calc"),
		{Role: crewai.RoleTool, Content: "2", ToolName: "calc"},
	}, []crewai.ToolSpec{{
		Type: "function",
		Function: crewai.ToolFunction{
			Name:        "calc",
			Description: "c",
			Parameters:  json.RawMessage(`{}`),
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "using tools" || len(resp.ToolCalls) != 1 {
		t.Fatalf("resp = %+v", resp)
	}

	srvErr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"error":"tools down"}`))
	}))
	defer srvErr.Close()
	cErr := ollama.New("m", ollama.WithBaseURL(srvErr.URL))
	if _, err := cErr.CallWithTools(context.Background(), nil, nil); err == nil || !strings.Contains(err.Error(), "tools down") {
		t.Fatalf("err = %v", err)
	}

	srvStatus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"message":{}}`))
	}))
	defer srvStatus.Close()
	cStatus := ollama.New("m", ollama.WithBaseURL(srvStatus.URL))
	if _, err := cStatus.CallWithTools(context.Background(), nil, nil); err == nil {
		t.Fatal("expected status error")
	}

	srvBad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer srvBad.Close()
	cBad := ollama.New("m", ollama.WithBaseURL(srvBad.URL))
	if _, err := cBad.CallWithTools(context.Background(), nil, nil); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestWebSearch_Coverage(t *testing.T) {
	cloud := ollama.NewCloud("m")
	if _, err := cloud.WebSearch(context.Background(), "q", 3); err == nil {
		t.Fatal("expected cloud token error")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`nope`))
	}))
	defer srv.Close()
	c := ollama.New("m", ollama.WithBaseURL(srv.URL), ollama.WithAPIKey("k"))
	if _, err := c.WebSearch(context.Background(), "q", 0); err == nil || !strings.Contains(err.Error(), "web search HTTP") {
		t.Fatalf("err = %v", err)
	}

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer srv2.Close()
	c2 := ollama.New("m", ollama.WithBaseURL(srv2.URL))
	if _, err := c2.WebSearch(context.Background(), "q", 99); err == nil {
		t.Fatal("expected decode error")
	}
}
