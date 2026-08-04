package xai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// oauthServer simula um servidor RFC 8628 (device flow) + refresh.
func oauthServer(t *testing.T, pendingPolls int32) *httptest.Server {
	t.Helper()
	var polls int32
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth2/device/code", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"device_code":"dev123",
			"user_code":"ABCD-EFGH",
			"verification_uri":"https://accounts.x.ai/device",
			"verification_uri_complete":"https://accounts.x.ai/device?code=ABCD-EFGH",
			"expires_in":600,
			"interval":1
		}`))
	})
	mux.HandleFunc("/oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		switch r.Form.Get("grant_type") {
		case "refresh_token":
			_, _ = w.Write([]byte(`{"access_token":"renovado","refresh_token":"rt2","token_type":"Bearer","expires_in":3600}`))
		default: // device_code
			if atomic.AddInt32(&polls, 1) <= pendingPolls {
				_, _ = w.Write([]byte(`{"error":"authorization_pending"}`))
				return
			}
			_, _ = w.Write([]byte(`{"access_token":"inicial","refresh_token":"rt1","token_type":"Bearer","expires_in":3600}`))
		}
	})
	return httptest.NewServer(mux)
}

func TestDeviceFlowAuthorize(t *testing.T) {
	srv := oauthServer(t, 2) // 2 polls pendentes antes de autorizar
	defer srv.Close()

	var prompted bool
	df := NewDeviceFlow("client-abc",
		WithDeviceCodeURL(srv.URL+"/oauth2/device/code"),
		WithTokenURL(srv.URL+"/oauth2/token"),
		WithPrompt(func(info VerificationInfo) {
			prompted = true
			if info.UserCode != "ABCD-EFGH" {
				t.Errorf("UserCode = %q", info.UserCode)
			}
		}),
	)

	tok, err := df.Authorize(context.Background())
	if err != nil {
		t.Fatalf("Authorize erro: %v", err)
	}
	if !prompted {
		t.Error("Prompt não foi chamado")
	}
	if tok.AccessToken != "inicial" || tok.RefreshToken != "rt1" {
		t.Errorf("token = %+v", tok)
	}
	if !tok.Valid() {
		t.Error("token deveria ser válido")
	}
}

func TestDeviceFlowRefresh(t *testing.T) {
	srv := oauthServer(t, 0)
	defer srv.Close()

	df := NewDeviceFlow("client-abc",
		WithTokenURL(srv.URL+"/oauth2/token"),
	)
	tok, err := df.Refresh(context.Background(), "rt1")
	if err != nil {
		t.Fatalf("Refresh erro: %v", err)
	}
	if tok.AccessToken != "renovado" {
		t.Errorf("AccessToken = %q", tok.AccessToken)
	}
}

func TestRefreshingTokenSource(t *testing.T) {
	srv := oauthServer(t, 0)
	defer srv.Close()

	df := NewDeviceFlow("client-abc", WithTokenURL(srv.URL+"/oauth2/token"))

	// Token já expirado força a renovação na primeira chamada.
	expired := Token{AccessToken: "velho", RefreshToken: "rt1", Expiry: time.Now().Add(-time.Hour)}
	ts := df.TokenSource(expired, nil)

	got, err := ts.Token(context.Background())
	if err != nil {
		t.Fatalf("Token erro: %v", err)
	}
	if got != "renovado" {
		t.Errorf("token = %q, quer 'renovado'", got)
	}
}

func TestTokenSourceNoRefresh(t *testing.T) {
	df := NewDeviceFlow("c")
	expired := Token{AccessToken: "velho", Expiry: time.Now().Add(-time.Hour)} // sem refresh
	ts := df.TokenSource(expired, nil)
	if _, err := ts.Token(context.Background()); err == nil {
		t.Error("esperava erro sem refresh token")
	}
}

func TestSaveLoadToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tok.json")
	orig := Token{AccessToken: "a", RefreshToken: "r", TokenType: "Bearer", Expiry: time.Now().Add(time.Hour)}

	if err := SaveToken(path, orig); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}
	loaded, err := LoadToken(path)
	if err != nil {
		t.Fatalf("LoadToken: %v", err)
	}
	if loaded.AccessToken != "a" || loaded.RefreshToken != "r" {
		t.Errorf("loaded = %+v", loaded)
	}
}

func TestLoadTokenSourcePersistsRefresh(t *testing.T) {
	srv := oauthServer(t, 0)
	defer srv.Close()
	df := NewDeviceFlow("c", WithTokenURL(srv.URL+"/oauth2/token"))

	dir := t.TempDir()
	path := filepath.Join(dir, "tok.json")
	_ = SaveToken(path, Token{AccessToken: "velho", RefreshToken: "rt1", Expiry: time.Now().Add(-time.Hour)})

	ts, err := LoadTokenSource(path, df)
	if err != nil {
		t.Fatalf("LoadTokenSource: %v", err)
	}
	if _, err := ts.Token(context.Background()); err != nil {
		t.Fatalf("Token: %v", err)
	}
	// O arquivo deve ter sido regravado com o token renovado.
	reloaded, _ := LoadToken(path)
	if reloaded.AccessToken != "renovado" {
		t.Errorf("arquivo não atualizado: %+v", reloaded)
	}
}

func TestPKCEPair(t *testing.T) {
	v, c, err := pkcePair()
	if err != nil {
		t.Fatal(err)
	}
	if v == "" || c == "" || v == c {
		t.Errorf("pkce inválido: verifier=%q challenge=%q", v, c)
	}
}
