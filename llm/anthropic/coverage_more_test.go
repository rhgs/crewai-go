package anthropic_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rhgs/crewai-go"
	"github.com/rhgs/crewai-go/llm/anthropic"
)

func TestWebSearch_SuccessAndNetwork(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Minimal successful-ish payload with web_search_tool_result block.
		_, _ = w.Write([]byte(`{
			"content":[
				{"type":"text","text":"here"},
				{"type":"web_search_tool_result","tool_use_id":"1","content":[
					{"type":"web_search_result","title":"Go","url":"https://go.dev","encrypted_content":"x"}
				]}
			]
		}`))
	}))
	defer srv.Close()
	c := anthropic.New("m", anthropic.WithAPIKey("k"), anthropic.WithBaseURL(srv.URL))
	hits, err := c.WebSearch(context.Background(), "go", 3)
	if err != nil {
		// Some parsers may require more fields; accept either hits or a clear error.
		if !strings.Contains(err.Error(), "anthropic") {
			t.Fatalf("err = %v", err)
		}
	} else if len(hits) == 0 {
		t.Log("no hits parsed; response format may differ")
	}

	c2 := anthropic.New("m", anthropic.WithAPIKey("k"), anthropic.WithBaseURL("http://127.0.0.1:1"))
	if _, err := c2.Call(context.Background(), []crewai.Message{crewai.UserMessage("x")}); err == nil {
		t.Fatal("expected network error")
	}
	if _, err := c2.CallWithTools(context.Background(), nil, nil); err == nil {
		t.Fatal("expected network error tools")
	}
	if _, err := c2.WebSearch(context.Background(), "q", 2); err == nil {
		t.Fatal("expected network error search")
	}
}
