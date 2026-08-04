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
