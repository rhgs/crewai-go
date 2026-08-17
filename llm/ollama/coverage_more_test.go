package ollama_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rhgs/crewai-go"
	"github.com/rhgs/crewai-go/llm/ollama"
)

func TestNetworkErrorsAndWebSearchSuccess(t *testing.T) {
	c := ollama.New("m", ollama.WithBaseURL("http://127.0.0.1:1"))
	if _, err := c.Call(context.Background(), []crewai.Message{crewai.UserMessage("x")}); err == nil {
		t.Fatal("expected call network error")
	}
	if _, err := c.CallWithTools(context.Background(), nil, nil); err == nil {
		t.Fatal("expected tools network error")
	}
	if _, err := c.WebSearch(context.Background(), "q", 2); err == nil {
		t.Fatal("expected search network error")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"title":"T","url":"https://example.com","content":"c"}]}`))
	}))
	defer srv.Close()
	c2 := ollama.New("m", ollama.WithBaseURL(srv.URL), ollama.WithAPIKey("k"))
	hits, err := c2.WebSearch(context.Background(), "q", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Title != "T" {
		t.Fatalf("hits = %+v", hits)
	}
}
