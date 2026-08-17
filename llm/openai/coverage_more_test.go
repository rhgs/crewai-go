package openai_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rhgs/crewai-go"
	"github.com/rhgs/crewai-go/llm/openai"
)

func TestCall_NetworkAndStatusPaths(t *testing.T) {
	// unexpected status without error object
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"x"}}]}`))
	}))
	defer srv.Close()
	c := openai.New("m", openai.WithAPIKey("k"), openai.WithBaseURL(srv.URL))
	if _, err := c.Call(context.Background(), []crewai.Message{crewai.UserMessage("x")}); err == nil || !strings.Contains(err.Error(), "unexpected status") {
		t.Fatalf("err = %v", err)
	}

	// connection error
	c2 := openai.New("m", openai.WithAPIKey("k"), openai.WithBaseURL("http://127.0.0.1:1"))
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

func TestWebSearch_SuccessClamp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"choices":[{
				"message":{
					"content":"summary",
					"annotations":[
						{"type":"url_citation","url_citation":{"url":"https://a.example","title":"A","start_index":0,"end_index":1}},
						{"type":"url_citation","url_citation":{"url":"https://b.example","title":"B","start_index":2,"end_index":3}},
						{"type":"url_citation","url_citation":{"url":"https://c.example","title":"C","start_index":4,"end_index":5}}
					]
				}
			}]
		}`))
	}))
	defer srv.Close()
	c := openai.New("m", openai.WithAPIKey("k"), openai.WithBaseURL(srv.URL))
	hits, err := c.WebSearch(context.Background(), "q", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("expected hits")
	}
}
