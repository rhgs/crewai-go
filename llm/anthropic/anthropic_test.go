package anthropic_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rodolphosa/crewai-go"
	"github.com/rodolphosa/crewai-go/llm/anthropic"
)

func TestClientCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "k" {
			t.Errorf("x-api-key = %q", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") == "" {
			t.Error("anthropic-version ausente")
		}
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		// O system prompt deve ser separado das mensagens.
		if req["system"] != "instruções" {
			t.Errorf("system = %v", req["system"])
		}
		msgs, _ := req["messages"].([]any)
		if len(msgs) != 1 {
			t.Errorf("esperava 1 mensagem, obteve %d", len(msgs))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"resposta do Claude"}]}`))
	}))
	defer srv.Close()

	c := anthropic.New("claude-sonnet-5",
		anthropic.WithAPIKey("k"),
		anthropic.WithBaseURL(srv.URL),
	)
	out, err := c.Call(context.Background(), []crewai.Message{
		crewai.SystemMessage("instruções"),
		crewai.UserMessage("olá"),
	})
	if err != nil {
		t.Fatalf("erro: %v", err)
	}
	if out != "resposta do Claude" {
		t.Errorf("out = %q", out)
	}
}

func TestClientMissingKey(t *testing.T) {
	c := anthropic.New("claude-sonnet-5", anthropic.WithAPIKey(""))
	if _, err := c.Call(context.Background(), nil); err == nil {
		t.Error("esperava erro por chave ausente")
	}
}
