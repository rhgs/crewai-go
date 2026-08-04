package xai_test

import (
	"context"
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
			t.Errorf("Authorization = %q, quer %q", got, wantAuth)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"olá Grok"}}]}`))
	}))
}

func TestAPIKey(t *testing.T) {
	srv := chatServer(t, "Bearer xai-key")
	defer srv.Close()

	llm := xai.New("grok-4", xai.WithAPIKey("xai-key"), xai.WithBaseURL(srv.URL))
	if llm.Model() != "grok-4" {
		t.Errorf("Model() = %q", llm.Model())
	}
	out, err := llm.Call(context.Background(), []crewai.Message{crewai.UserMessage("oi")})
	if err != nil {
		t.Fatalf("erro: %v", err)
	}
	if out != "olá Grok" {
		t.Errorf("out = %q", out)
	}
}

// staticSource é uma TokenSource fixa para teste.
type staticSource struct{ tok string }

func (s staticSource) Token(context.Context) (string, error) { return s.tok, nil }

func TestOAuthTokenSource(t *testing.T) {
	srv := chatServer(t, "Bearer oauth-access-token")
	defer srv.Close()

	llm := xai.NewWithOAuth("grok-4", staticSource{tok: "oauth-access-token"}, xai.WithBaseURL(srv.URL))
	out, err := llm.Call(context.Background(), []crewai.Message{crewai.UserMessage("oi")})
	if err != nil {
		t.Fatalf("erro: %v", err)
	}
	if out != "olá Grok" {
		t.Errorf("out = %q", out)
	}
}
