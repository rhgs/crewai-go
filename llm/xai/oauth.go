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

// DefaultAuthServer é o servidor de identidade da xAI usado no OAuth de
// assinatura (SuperGrok / X Premium).
const DefaultAuthServer = "https://accounts.x.ai"

// NOTA IMPORTANTE: a xAI ainda não publica oficialmente os caminhos exatos do
// endpoint de OAuth nem o client_id público para clientes de terceiros. Este
// pacote implementa o Device Authorization Grant padrão (RFC 8628) com PKCE
// (RFC 7636). Forneça o ClientID e, se necessário, sobrescreva os caminhos dos
// endpoints com os valores oficiais da xAI via as opções abaixo.

// DeviceFlow conduz o OAuth 2.0 Device Authorization Grant (RFC 8628) com PKCE.
type DeviceFlow struct {
	// ClientID é o identificador do aplicativo OAuth (fornecido pela xAI).
	ClientID string
	// DeviceCodeURL é o endpoint de código de dispositivo.
	DeviceCodeURL string
	// TokenURL é o endpoint de troca/renovação de token.
	TokenURL string
	// Scopes solicitados (ex.: "openid", "offline_access").
	Scopes []string
	// HTTPClient usado nas requisições.
	HTTPClient *http.Client
	// Prompt é chamado para instruir o usuário a autorizar o dispositivo.
	// Se nil, as instruções são impressas em os.Stderr.
	Prompt func(VerificationInfo)
}

// VerificationInfo são os dados que o usuário precisa para autorizar.
type VerificationInfo struct {
	VerificationURI         string
	VerificationURIComplete string
	UserCode                string
	ExpiresIn               int
}

// DeviceFlowOption configura um DeviceFlow.
type DeviceFlowOption func(*DeviceFlow)

// WithAuthServer define o servidor de autenticação base, derivando os caminhos
// convencionais de device code e token, salvo se já sobrescritos.
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

// WithDeviceCodeURL sobrescreve o endpoint de device code.
func WithDeviceCodeURL(u string) DeviceFlowOption {
	return func(d *DeviceFlow) { d.DeviceCodeURL = u }
}

// WithTokenURL sobrescreve o endpoint de token.
func WithTokenURL(u string) DeviceFlowOption { return func(d *DeviceFlow) { d.TokenURL = u } }

// WithScopes define os escopos solicitados.
func WithScopes(scopes ...string) DeviceFlowOption {
	return func(d *DeviceFlow) { d.Scopes = scopes }
}

// WithPrompt define o callback de instrução ao usuário.
func WithPrompt(fn func(VerificationInfo)) DeviceFlowOption {
	return func(d *DeviceFlow) { d.Prompt = fn }
}

// WithDeviceFlowHTTPClient injeta um *http.Client.
func WithDeviceFlowHTTPClient(h *http.Client) DeviceFlowOption {
	return func(d *DeviceFlow) { d.HTTPClient = h }
}

// NewDeviceFlow cria um Device Flow. Por padrão usa DefaultAuthServer e os
// caminhos convencionais de RFC 8628; sobrescreva conforme a documentação
// oficial da xAI.
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

// Token é um token OAuth com validade e refresh token.
type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	Expiry       time.Time `json:"expiry"`
}

// Valid indica se o token existe e ainda não expirou (com 60s de folga).
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

// Authorize executa o Device Flow completo: solicita um código, instrui o
// usuário e faz o polling até a autorização, devolvendo o Token final.
func (d *DeviceFlow) Authorize(ctx context.Context) (Token, error) {
	if d.ClientID == "" {
		return Token{}, fmt.Errorf("xai/oauth: ClientID é obrigatório")
	}

	verifier, challenge, err := pkcePair()
	if err != nil {
		return Token{}, err
	}

	// 1) Solicita o device code.
	form := url.Values{
		"client_id":             {d.ClientID},
		"scope":                 {strings.Join(d.Scopes, " ")},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	var dc deviceCodeResponse
	if err := d.postForm(ctx, d.DeviceCodeURL, form, &dc); err != nil {
		return Token{}, fmt.Errorf("xai/oauth: solicitando device code: %w", err)
	}

	// 2) Instrui o usuário.
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

	// 3) Polling do token. Honra o intervalo do servidor; usa 5s (padrão da
	// RFC 8628) quando não informado.
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
			return Token{}, fmt.Errorf("xai/oauth: trocando token: %w", err)
		}
		switch tr.Error {
		case "":
			return tokenFromResponse(tr), nil
		case "authorization_pending":
			continue
		case "slow_down":
			interval += 5 * time.Second
		default:
			return Token{}, fmt.Errorf("xai/oauth: autorização falhou: %s (%s)", tr.Error, tr.ErrorDescription)
		}
	}
}

// Refresh renova um token usando seu refresh token.
func (d *DeviceFlow) Refresh(ctx context.Context, refreshToken string) (Token, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {d.ClientID},
	}
	var tr tokenResponse
	if err := d.postForm(ctx, d.TokenURL, form, &tr); err != nil {
		return Token{}, fmt.Errorf("xai/oauth: renovando token: %w", err)
	}
	if tr.Error != "" {
		return Token{}, fmt.Errorf("xai/oauth: renovação falhou: %s (%s)", tr.Error, tr.ErrorDescription)
	}
	tok := tokenFromResponse(tr)
	if tok.RefreshToken == "" {
		tok.RefreshToken = refreshToken // alguns servidores não reemitem
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
		return fmt.Errorf("resposta inválida (status %d): %s", resp.StatusCode, string(data))
	}
	return nil
}

// --- TokenSource com renovação automática ------------------------------------

// TokenSource devolve uma fonte de token que renova automaticamente usando o
// refresh token quando o access token expira. Se save != nil, ele é chamado a
// cada renovação para persistir o novo token.
func (d *DeviceFlow) TokenSource(tok Token, save func(Token) error) openai.TokenSource {
	return &refreshingSource{df: d, tok: tok, save: save}
}

type refreshingSource struct {
	mu   sync.Mutex
	df   *DeviceFlow
	tok  Token
	save func(Token) error
}

// Token implementa openai.TokenSource.
func (s *refreshingSource) Token(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.tok.Valid() {
		return s.tok.AccessToken, nil
	}
	if s.tok.RefreshToken == "" {
		return "", fmt.Errorf("xai/oauth: token expirado e sem refresh token; refaça o login")
	}
	newTok, err := s.df.Refresh(ctx, s.tok.RefreshToken)
	if err != nil {
		return "", err
	}
	s.tok = newTok
	if s.save != nil {
		if err := s.save(newTok); err != nil {
			return "", fmt.Errorf("xai/oauth: persistindo token renovado: %w", err)
		}
	}
	return s.tok.AccessToken, nil
}

// --- Persistência ------------------------------------------------------------

// SaveToken grava o token em um arquivo JSON (permissão 0600).
func SaveToken(path string, tok Token) error {
	data, err := json.MarshalIndent(tok, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// LoadToken lê um token de um arquivo JSON.
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

// LoadTokenSource carrega um token de um arquivo e devolve uma TokenSource que
// renova e regrava o token nesse mesmo arquivo automaticamente.
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
		exp = time.Now().Add(time.Hour) // fallback conservador
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
	fmt.Fprintln(os.Stderr, "\n=== Autorização xAI (OAuth de assinatura) ===")
	if info.VerificationURIComplete != "" {
		fmt.Fprintf(os.Stderr, "Abra: %s\n", info.VerificationURIComplete)
	} else {
		fmt.Fprintf(os.Stderr, "Abra: %s\n", info.VerificationURI)
		fmt.Fprintf(os.Stderr, "E informe o código: %s\n", info.UserCode)
	}
	fmt.Fprintln(os.Stderr, "Aguardando autorização...")
}
