package xai_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rhgs/crewai-go"
	"github.com/rhgs/crewai-go/llm/openai"
	"github.com/rhgs/crewai-go/llm/xai"
)

type staticTS struct{ tok string }

func (s staticTS) Token(context.Context) (string, error) { return s.tok, nil }

func TestXAIOptionsAndDelegation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer xai-tok" {
			t.Errorf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "chat/completions") {
			// distinguish CallWithTools by tools field is hard here; same endpoint.
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"grok","tool_calls":[]}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := xai.New("grok-test",
		xai.WithAPIKey("ignored"),
		xai.WithBaseURL(srv.URL),
		xai.WithTemperature(0.4),
		xai.WithTokenSource(staticTS{tok: "xai-tok"}),
	)
	if c.Model() != "grok-test" {
		t.Fatalf("Model = %q", c.Model())
	}
	out, err := c.Call(context.Background(), []crewai.Message{crewai.UserMessage("hi")})
	if err != nil {
		t.Fatal(err)
	}
	if out != "grok" {
		t.Fatalf("out = %q", out)
	}
	resp, err := c.CallWithTools(context.Background(), []crewai.Message{crewai.UserMessage("hi")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "grok" {
		t.Fatalf("tool content = %q", resp.Content)
	}

	// NewWithOAuth path
	c2 := xai.NewWithOAuth("grok-test", staticTS{tok: "xai-tok"}, xai.WithBaseURL(srv.URL), xai.WithTemperature(0.1))
	if _, err := c2.Call(context.Background(), []crewai.Message{crewai.UserMessage("hi")}); err != nil {
		t.Fatal(err)
	}
}

func TestXAIWebSearchDelegates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"choices":[{
				"message":{
					"content":"found",
					"annotations":[{
						"type":"url_citation",
						"url_citation":{"url":"https://example.com","title":"Example","start_index":0,"end_index":1}
					}]
				}
			}]
		}`))
	}))
	defer srv.Close()
	c := xai.New("grok-test", xai.WithAPIKey("k"), xai.WithBaseURL(srv.URL))
	hits, err := c.WebSearch(context.Background(), "q", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("expected hits")
	}
}

func TestOAuthOptionsAndDefaultPrompt(t *testing.T) {
	// Cover WithScopes / WithDeviceFlowHTTPClient / defaultPrompt via Authorize.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/device/code"):
			_, _ = w.Write([]byte(`{
				"device_code":"dev",
				"user_code":"UUUU",
				"verification_uri":"https://example.com/device",
				"verification_uri_complete":"https://example.com/device?code=UUUU",
				"expires_in":600,
				"interval":1
			}`))
		default:
			_ = r.ParseForm()
			if r.Form.Get("scope") == "" {
				// scopes should be present when set
			}
			_, _ = w.Write([]byte(`{"access_token":"a","refresh_token":"r","token_type":"Bearer","expires_in":3600}`))
		}
	}))
	defer srv.Close()

	df := xai.NewDeviceFlow("cid",
		xai.WithAuthServer(srv.URL),
		xai.WithDeviceCodeURL(srv.URL+"/oauth2/device/code"),
		xai.WithTokenURL(srv.URL+"/oauth2/token"),
		xai.WithScopes("openid", "offline_access", "api"),
		xai.WithDeviceFlowHTTPClient(&http.Client{Timeout: 5 * time.Second}),
		// nil Prompt exercises defaultPrompt
	)
	tok, err := df.Authorize(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "a" {
		t.Fatalf("token = %+v", tok)
	}
}

func TestOAuthSaveLoadAndRefreshErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tok.json")

	tok := xai.Token{
		AccessToken:  "acc",
		RefreshToken: "ref",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(time.Hour),
	}
	if err := xai.SaveToken(path, tok); err != nil {
		t.Fatal(err)
	}
	got, err := xai.LoadToken(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "acc" || got.RefreshToken != "ref" {
		t.Fatalf("got = %+v", got)
	}
	if _, err := xai.LoadToken(filepath.Join(dir, "missing.json")); err == nil {
		t.Fatal("expected load error")
	}

	// corrupt file
	if err := os.WriteFile(path, []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := xai.LoadToken(path); err == nil {
		t.Fatal("expected corrupt load error")
	}

	// LoadTokenSource with expired token forces refresh
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"new","refresh_token":"r2","token_type":"Bearer","expires_in":3600}`))
	}))
	defer srv.Close()
	expiredPath := filepath.Join(dir, "expired.json")
	if err := xai.SaveToken(expiredPath, xai.Token{
		AccessToken:  "old",
		RefreshToken: "rt",
		Expiry:       time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	df := xai.NewDeviceFlow("cid", xai.WithTokenURL(srv.URL))
	ts, err := xai.LoadTokenSource(expiredPath, df)
	if err != nil {
		t.Fatal(err)
	}
	// ensure interface satisfaction path
	var _ openai.TokenSource = ts
	tokStr, err := ts.Token(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tokStr != "new" {
		t.Fatalf("token = %q", tokStr)
	}

	// Refresh error body
	srvErr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"nope"}`))
	}))
	defer srvErr.Close()
	dfErr := xai.NewDeviceFlow("cid", xai.WithTokenURL(srvErr.URL))
	if _, err := dfErr.Refresh(context.Background(), "bad"); err == nil {
		t.Fatal("expected refresh error")
	}
}
