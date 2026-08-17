package xai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAuthorize_MissingClientID(t *testing.T) {
	df := &DeviceFlow{}
	if _, err := df.Authorize(context.Background()); err == nil {
		t.Fatal("expected ClientID error")
	}
}

func TestAuthorize_SlowDownAndDefaultIntervalAndURIOnlyPrompt(t *testing.T) {
	var polls int32
	mux := http.NewServeMux()
	mux.HandleFunc("/device", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// interval 0 triggers RFC default 5s — too slow for tests; use 1 and slow_down once.
		_, _ = w.Write([]byte(`{
			"device_code":"dev",
			"user_code":"CODE",
			"verification_uri":"https://example.com/device",
			"verification_uri_complete":"",
			"expires_in":600,
			"interval":1
		}`))
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		n := atomic.AddInt32(&polls, 1)
		switch n {
		case 1:
			_, _ = w.Write([]byte(`{"error":"slow_down"}`))
		case 2:
			_, _ = w.Write([]byte(`{"error":"authorization_pending"}`))
		default:
			// expires_in 0 + empty token_type exercise tokenFromResponse fallbacks
			_, _ = w.Write([]byte(`{"access_token":"atk","refresh_token":"rtk"}`))
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Capture defaultPrompt branch without VerificationURIComplete.
	df := NewDeviceFlow("cid",
		WithDeviceCodeURL(srv.URL+"/device"),
		WithTokenURL(srv.URL+"/token"),
		// nil Prompt -> defaultPrompt
	)
	tok, err := df.Authorize(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "atk" || tok.TokenType != "Bearer" || !tok.Valid() {
		t.Fatalf("tok = %+v", tok)
	}
}

func TestAuthorize_AuthorizationFailedAndCancel(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/device", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"device_code":"dev","user_code":"U",
			"verification_uri":"https://example.com",
			"verification_uri_complete":"https://example.com/c",
			"expires_in":60,"interval":1
		}`))
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":"access_denied","error_description":"nope"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	df := NewDeviceFlow("cid",
		WithDeviceCodeURL(srv.URL+"/device"),
		WithTokenURL(srv.URL+"/token"),
		WithPrompt(func(VerificationInfo) {}),
	)
	if _, err := df.Authorize(context.Background()); err == nil || !strings.Contains(err.Error(), "access_denied") {
		t.Fatalf("err = %v", err)
	}

	// cancel during poll
	mux2 := http.NewServeMux()
	mux2.HandleFunc("/device", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"device_code":"dev","user_code":"U",
			"verification_uri":"https://example.com",
			"verification_uri_complete":"https://example.com/c",
			"expires_in":60,"interval":1
		}`))
	})
	mux2.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":"authorization_pending"}`))
	})
	srv2 := httptest.NewServer(mux2)
	defer srv2.Close()
	ctx, cancel := context.WithCancel(context.Background())
	df2 := NewDeviceFlow("cid",
		WithDeviceCodeURL(srv2.URL+"/device"),
		WithTokenURL(srv2.URL+"/token"),
		WithPrompt(func(VerificationInfo) {}),
	)
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	if _, err := df2.Authorize(ctx); err == nil {
		t.Fatal("expected cancel")
	}
}

func TestPostForm_InvalidJSONAndRefreshKeepRefreshToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer srv.Close()
	df := NewDeviceFlow("cid", WithTokenURL(srv.URL), WithDeviceCodeURL(srv.URL))
	if _, err := df.Authorize(context.Background()); err == nil {
		t.Fatal("expected invalid device code response")
	}

	// Refresh returns access token but no refresh_token -> keep old
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"new","token_type":"Bearer","expires_in":3600}`))
	}))
	defer srv2.Close()
	df2 := NewDeviceFlow("cid", WithTokenURL(srv2.URL))
	tok, err := df2.Refresh(context.Background(), "old-rt")
	if err != nil {
		t.Fatal(err)
	}
	if tok.RefreshToken != "old-rt" || tok.AccessToken != "new" {
		t.Fatalf("tok = %+v", tok)
	}
}

func TestRefreshingSource_ValidSaveAndErrors(t *testing.T) {
	// valid token short-circuit
	df := NewDeviceFlow("cid")
	ts := df.TokenSource(Token{
		AccessToken: "live",
		Expiry:      time.Now().Add(time.Hour),
	}, nil)
	got, err := ts.Token(context.Background())
	if err != nil || got != "live" {
		t.Fatalf("got=%q err=%v", got, err)
	}

	// expired without refresh
	ts2 := df.TokenSource(Token{AccessToken: "old", Expiry: time.Now().Add(-time.Hour)}, nil)
	if _, err := ts2.Token(context.Background()); err == nil {
		t.Fatal("expected no refresh token error")
	}

	// expired with refresh + save error
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"n","refresh_token":"r","expires_in":3600}`))
	}))
	defer srv.Close()
	df3 := NewDeviceFlow("cid", WithTokenURL(srv.URL))
	ts3 := df3.TokenSource(Token{
		AccessToken:  "old",
		RefreshToken: "rt",
		Expiry:       time.Now().Add(-time.Hour),
	}, func(Token) error { return os.ErrPermission })
	if _, err := ts3.Token(context.Background()); err == nil || !strings.Contains(err.Error(), "persisting") {
		t.Fatalf("err = %v", err)
	}

	// save success path
	var saved Token
	ts4 := df3.TokenSource(Token{
		AccessToken:  "old",
		RefreshToken: "rt",
		Expiry:       time.Now().Add(-time.Hour),
	}, func(tok Token) error { saved = tok; return nil })
	got, err = ts4.Token(context.Background())
	if err != nil || got != "n" || saved.AccessToken != "n" {
		t.Fatalf("got=%q saved=%+v err=%v", got, saved, err)
	}
}

func TestSaveToken_LoadTokenSourceMissing(t *testing.T) {
	if _, err := LoadTokenSource(filepath.Join(t.TempDir(), "nope.json"), NewDeviceFlow("c")); err == nil {
		t.Fatal("expected missing file")
	}
	// Token Valid false when empty access
	if (Token{}).Valid() {
		t.Fatal("empty token should be invalid")
	}
}

func TestDefaultPromptBothBranches(t *testing.T) {
	// Directly exercise both branches.
	defaultPrompt(VerificationInfo{VerificationURIComplete: "https://c", VerificationURI: "https://u", UserCode: "X"})
	defaultPrompt(VerificationInfo{VerificationURI: "https://u", UserCode: "Y"})
}

func TestDeviceCodeRequestError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	defer srv.Close()
	// non-JSON body from postForm
	df := NewDeviceFlow("cid", WithDeviceCodeURL(srv.URL), WithTokenURL(srv.URL), WithPrompt(func(VerificationInfo) {}))
	if _, err := df.Authorize(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestTokenFromResponseJSONRoundTrip(t *testing.T) {
	// ensure SaveToken path with indent works on zero-value-ish token
	path := filepath.Join(t.TempDir(), "t.json")
	tok := tokenFromResponse(tokenResponse{AccessToken: "a", ExpiresIn: 0, TokenType: ""})
	if err := SaveToken(path, tok); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m["access_token"] != "a" {
		t.Fatalf("%v", m)
	}
}
