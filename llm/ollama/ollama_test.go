package ollama_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rhgs/crewai-go"
	"github.com/rhgs/crewai-go/llm/ollama"
)

func TestLocalCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("path = %q", r.URL.Path)
		}
		// Local não deve enviar Authorization.
		if r.Header.Get("Authorization") != "" {
			t.Errorf("local não deveria enviar Authorization: %q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"olá local"},"done":true}`))
	}))
	defer srv.Close()

	llm := ollama.New("llama3.2", ollama.WithBaseURL(srv.URL))
	if llm.Model() != "llama3.2" {
		t.Errorf("Model() = %q", llm.Model())
	}
	out, err := llm.Call(context.Background(), []crewai.Message{crewai.UserMessage("oi")})
	if err != nil {
		t.Fatalf("erro: %v", err)
	}
	if out != "olá local" {
		t.Errorf("out = %q", out)
	}
}

func TestCloudCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer cloud-key" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"olá cloud"},"done":true}`))
	}))
	defer srv.Close()

	llm := ollama.NewCloud("gpt-oss:120b",
		ollama.WithBaseURL(srv.URL),
		ollama.WithAPIKey("cloud-key"),
	)
	out, err := llm.Call(context.Background(), []crewai.Message{crewai.UserMessage("oi")})
	if err != nil {
		t.Fatalf("erro: %v", err)
	}
	if out != "olá cloud" {
		t.Errorf("out = %q", out)
	}
}

func TestCloudRequiresKey(t *testing.T) {
	// Sem WithBaseURL, aponta para o cloud real; sem key deve falhar antes da rede.
	llm := ollama.NewCloud("x", ollama.WithAPIKey(""))
	if _, err := llm.Call(context.Background(), nil); err == nil {
		t.Error("esperava erro por token ausente no cloud")
	}
}

func TestAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"error":"model not found"}`))
	}))
	defer srv.Close()

	llm := ollama.New("x", ollama.WithBaseURL(srv.URL))
	if _, err := llm.Call(context.Background(), []crewai.Message{crewai.UserMessage("oi")}); err == nil {
		t.Error("esperava erro da API")
	}
}
