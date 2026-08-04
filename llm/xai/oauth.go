package xai

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/rhgs/crewai-go/llm/openai"
)

// DefaultAuthServer is the xAI identity server used for subscription OAuth
// (SuperGrok / X Premium).
const DefaultAuthServer = "https://accounts.x.ai"

// IMPORTANT NOTE: xAI does not yet officially publish the exact OAuth endpoint
// paths or the public client_id for third-party clients. This package
// implements the standard Device Authorization Grant (RFC 8628) with PKCE
// (RFC 7636). Provide the ClientID and, if necessary, override the endpoint
// paths with the official xAI values via the options below.

// DeviceFlow conducts the OAuth 2.0 Device Authorization Grant (RFC 8628) with
// PKCE.
type DeviceFlow struct {
	// ClientID is the OAuth application identifier (provided by xAI).
	ClientID string
	// DeviceCodeURL is the device code endpoint.
	DeviceCodeURL string
	// TokenURL is the token exchange/renewal endpoint.
	TokenURL string
	// Scopes requested (e.g. "openid", "offline_access").
	Scopes []string
	// HTTPClient used for the requests.
	HTTPClient *http.Client
	// Prompt is called to instruct the user to authorize the device.
	// If nil, the instructions are printed to os.Stderr.
	Prompt func(VerificationInfo)
}

// VerificationInfo is the data the user needs to authorize.
type VerificationInfo struct {
	VerificationURI         string
	VerificationURIComplete string
	UserCode                string
	ExpiresIn               int
}

// DeviceFlowOption configures a DeviceFlow.
type DeviceFlowOption func(*DeviceFlow)

// WithAuthServer sets the base authentication server, deriving the
// conventional device code and token paths, unless already overridden.
func WithAuthServer(base string) DeviceFlowOption {
	return func(d *DeviceFlow) {
		if d.DeviceCodeURL == "" {
			d.DeviceCodeURL = base + "/oauth2/device/code"
		}
		if d.TokenURL == "" {
			d.TokenURL = base + "/oauth2/token"
		}
	}
}

// WithDeviceCodeURL overrides the device code endpoint.
func WithDeviceCodeURL(u string) DeviceFlowOption {
	return func(d *DeviceFlow) { d.DeviceCodeURL = u }
}

// WithTokenURL overrides the token endpoint.
func WithTokenURL(u string) DeviceFlowOption { return func(d *DeviceFlow) { d.TokenURL = u } }

// WithScopes sets the requested scopes.
func WithScopes(scopes ...string) DeviceFlowOption {
	return func(d *DeviceFlow) { d.Scopes = scopes }
}

// WithPrompt sets the user-instruction callback.
func WithPrompt(fn func(VerificationInfo)) DeviceFlowOption {
	return func(d *DeviceFlow) { d.Prompt = fn }
}

// WithDeviceFlowHTTPClient injects an *http.Client.
func WithDeviceFlowHTTPClient(h *http.Client) DeviceFlowOption {
	return func(d *DeviceFlow) { d.HTTPClient = h }
}

// NewDeviceFlow creates a Device Flow. By default it uses DefaultAuthServer and
// the conventional RFC 8628 paths; override them per the official xAI
// documentation.
func NewDeviceFlow(clientID string, opts ...DeviceFlowOption) *DeviceFlow {
	d := &DeviceFlow{
		ClientID:   clientID,
		Scopes:     []string{"openid", "offline_access"},
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
	WithAuthServer(DefaultAuthServer)(d)
	for _, o := range opts {
		o(d)
	}
	return d
}

// Token is an OAuth token with an expiry and a refresh token.
type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	Expiry       time.Time `json:"expiry"`
}

// Valid reports whether the token exists and has not expired (with a 60s grace
// period).
func (t Token) Valid() bool {
	return t.AccessToken != "" && time.Now().Add(60*time.Second).Before(t.Expiry)
}

type deviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

type tokenResponse struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	TokenType        string `json:"token_type"`
	ExpiresIn        int    `json:"expires_in"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// Authorize runs the full Device Flow: requests a code, instructs the user,
// and polls until authorization, returning the final Token.
func (d *DeviceFlow) Authorize(ctx context.Context) (Token, error) {
	if d.ClientID == "" {
		return Token{}, fmt.Errorf("xai/oauth: ClientID is required")
	}

	verifier, challenge, err := pkcePair()
	if err != nil {
		return Token{}, err
	}

	// 1) Request the device code.
	form := url.Values{
		"client_id":             {d.ClientID},
		"scope":                 {strings.Join(d.Scopes, " ")},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	var dc deviceCodeResponse
	if err := d.postForm(ctx, d.DeviceCodeURL, form, &dc); err != nil {
		return Token{}, fmt.Errorf("xai/oauth: requesting device code: %w", err)
	}

	// 2) Instruct the user.
	info := VerificationInfo{
		VerificationURI:         dc.VerificationURI,
		VerificationURIComplete: dc.VerificationURIComplete,
		UserCode:                dc.UserCode,
		ExpiresIn:               dc.ExpiresIn,
	}
	if d.Prompt != nil {
		d.Prompt(info)
	} else {
		defaultPrompt(info)
	}

	// 3) Poll for the token. Honors the server interval; uses 5s (the RFC 8628
	// default) when not provided.
	pollSecs := dc.Interval
	if pollSecs <= 0 {
		pollSecs = 5
	}
	interval := time.Duration(pollSecs) * time.Second
	for {
		select {
		case <-ctx.Done():
			return Token{}, ctx.Err()
		case <-time.After(interval):
		}

		tokForm := url.Values{
			"grant_type":    {"urn:ietf:params:oauth:grant-type:device_code"},
			"device_code":   {dc.DeviceCode},
			"client_id":     {d.ClientID},
			"code_verifier": {verifier},
		}
		var tr tokenResponse
		if err := d.postForm(ctx, d.TokenURL, tokForm, &tr); err != nil {
			return Token{}, fmt.Errorf("xai/oauth: exchanging token: %w", err)
		}
		switch tr.Error {
		case "":
			return tokenFromResponse(tr), nil
		case "authorization_pending":
			continue
		case "slow_down":
			interval += 5 * time.Second
		default:
			return Token{}, fmt.Errorf("xai/oauth: authorization failed: %s (%s)", tr.Error, tr.ErrorDescription)
		}
	}
}

// Refresh renews a token using its refresh token.
func (d *DeviceFlow) Refresh(ctx context.Context, refreshToken string) (Token, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {d.ClientID},
	}
	var tr tokenResponse
	if err := d.postForm(ctx, d.TokenURL, form, &tr); err != nil {
		return Token{}, fmt.Errorf("xai/oauth: refreshing token: %w", err)
	}
	if tr.Error != "" {
		return Token{}, fmt.Errorf("xai/oauth: refresh failed: %s (%s)", tr.Error, tr.ErrorDescription)
	}
	tok := tokenFromResponse(tr)
	if tok.RefreshToken == "" {
		tok.RefreshToken = refreshToken // some servers don't reissue it
	}
	return tok, nil
}

func (d *DeviceFlow) postForm(ctx context.Context, endpoint string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := d.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("invalid response (status %d): %s", resp.StatusCode, string(data))
	}
	return nil
}

// --- TokenSource with auto-renewal -------------------------------------------

// TokenSource returns a token source that automatically renews using the
// refresh token when the access token expires. If save != nil, it is called on
// each renewal to persist the new token.
func (d *DeviceFlow) TokenSource(tok Token, save func(Token) error) openai.TokenSource {
	return &refreshingSource{df: d, tok: tok, save: save}
}

type refreshingSource struct {
	mu   sync.Mutex
	df   *DeviceFlow
	tok  Token
	save func(Token) error
}

// Token implements openai.TokenSource.
func (s *refreshingSource) Token(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.tok.Valid() {
		return s.tok.AccessToken, nil
	}
	if s.tok.RefreshToken == "" {
		return "", fmt.Errorf("xai/oauth: token expired and no refresh token; log in again")
	}
	newTok, err := s.df.Refresh(ctx, s.tok.RefreshToken)
	if err != nil {
		return "", err
	}
	s.tok = newTok
	if s.save != nil {
		if err := s.save(newTok); err != nil {
			return "", fmt.Errorf("xai/oauth: persisting renewed token: %w", err)
		}
	}
	return s.tok.AccessToken, nil
}

// --- Persistence -------------------------------------------------------------

// SaveToken writes the token to a JSON file (permission 0600).
func SaveToken(path string, tok Token) error {
	data, err := json.MarshalIndent(tok, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// LoadToken reads a token from a JSON file.
func LoadToken(path string) (Token, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Token{}, err
	}
	var tok Token
	if err := json.Unmarshal(data, &tok); err != nil {
		return Token{}, err
	}
	return tok, nil
}

// LoadTokenSource loads a token from a file and returns a TokenSource that
// renews and rewrites the token to that same file automatically.
func LoadTokenSource(path string, d *DeviceFlow) (openai.TokenSource, error) {
	tok, err := LoadToken(path)
	if err != nil {
		return nil, err
	}
	return d.TokenSource(tok, func(t Token) error { return SaveToken(path, t) }), nil
}

// --- Helpers -----------------------------------------------------------------

func tokenFromResponse(tr tokenResponse) Token {
	exp := time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	if tr.ExpiresIn == 0 {
		exp = time.Now().Add(time.Hour) // conservative fallback
	}
	tt := tr.TokenType
	if tt == "" {
		tt = "Bearer"
	}
	return Token{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		TokenType:    tt,
		Expiry:       exp,
	}
}

func pkcePair() (verifier, challenge string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge, nil
}

func defaultPrompt(info VerificationInfo) {
	fmt.Fprintln(os.Stderr, "\n=== xAI Authorization (subscription OAuth) ===")
	if info.VerificationURIComplete != "" {
		fmt.Fprintf(os.Stderr, "Open: %s\n", info.VerificationURIComplete)
	} else {
		fmt.Fprintf(os.Stderr, "Open: %s\n", info.VerificationURI)
		fmt.Fprintf(os.Stderr, "And enter the code: %s\n", info.UserCode)
	}
	fmt.Fprintln(os.Stderr, "Waiting for authorization...")
}
