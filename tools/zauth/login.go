package zauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mensadb/tools/env"
	"net/http"
	"net/url"
	"strings"

	oidcV2 "github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/oidc/v2"
	sessionV2 "github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/session/v2"
)

type TokenSet struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
}

var (
	ErrUserNotFound    = errors.New("zauth: user not found")
	ErrInvalidPassword = errors.New("zauth: invalid password")
)

// LoginWithPassword esegue un login completo OIDC contro Zitadel
// usando Session API v2 + token exchange. Ritorna access/refresh/id token
// pronti per l'uso lato client come bearer.
func LoginWithPassword(email, password string) (*TokenSet, error) {
	if apiClient == nil {
		return nil, errors.New("zauth: api client not initialized")
	}

	// Pre-check: l'utente esiste su Zitadel?
	if _, exists := UserExists(email); !exists {
		return nil, ErrUserNotFound
	}

	verifier, challenge, err := generatePKCE()
	if err != nil {
		return nil, fmt.Errorf("zauth: generate pkce: %w", err)
	}

	authRequestID, err := startAuthRequest(challenge)
	if err != nil {
		return nil, fmt.Errorf("zauth: start auth request: %w", err)
	}

	sessResp, err := apiClient.SessionServiceV2().CreateSession(ctx, &sessionV2.CreateSessionRequest{
		Checks: &sessionV2.Checks{
			User: &sessionV2.CheckUser{
				Search: &sessionV2.CheckUser_LoginName{LoginName: email},
			},
			Password: &sessionV2.CheckPassword{Password: password},
		},
	})
	if err != nil {
		if isPasswordError(err) {
			return nil, ErrInvalidPassword
		}
		if isUserNotFoundError(err) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("zauth: create session: %w", err)
	}

	cbResp, err := apiClient.OIDCServiceV2().CreateCallback(ctx, &oidcV2.CreateCallbackRequest{
		AuthRequestId: authRequestID,
		CallbackKind: &oidcV2.CreateCallbackRequest_Session{
			Session: &oidcV2.Session{
				SessionId:    sessResp.SessionId,
				SessionToken: sessResp.SessionToken,
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("zauth: create callback: %w", err)
	}

	code, err := extractCodeFromCallback(cbResp.CallbackUrl)
	if err != nil {
		return nil, fmt.Errorf("zauth: extract code: %w", err)
	}

	return exchangeCodeForTokens(code, verifier)
}

func generatePKCE() (verifier, challenge string, err error) {
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge, nil
}

func startAuthRequest(challenge string) (string, error) {
	host := normalizeHTTPHost(env.GetZitadelHost())
	q := url.Values{}
	q.Set("client_id", env.GetZitadelOIDCClientID())
	q.Set("redirect_uri", env.GetZitadelOIDCRedirectURI())
	q.Set("response_type", "code")
	q.Set("scope", "openid profile email offline_access")
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")

	authorizeURL := host + "/oauth/v2/authorize?" + q.Encode()

	// Zitadel risponde con 302 verso /ui/login/login?authRequest=<id>.
	// Intercettiamo il primo redirect senza seguirlo.
	httpClient := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, err := http.NewRequest(http.MethodGet, authorizeURL, nil)
	if err != nil {
		return "", err
	}
	// Header obbligatorio per far generare a Zitadel un auth request v2
	// (visibile via OIDCServiceV2): identifica il service user che agisce
	// da "login client". Senza, Zitadel crea un auth request legacy v1
	// non queryable via gRPC e CreateCallback fallisce con NotFound.
	if lcid := env.GetZitadelLoginClientUserID(); lcid != "" {
		req.Header.Set("x-zitadel-login-client", lcid)
		req.Header.Set("Authorization", "Bearer "+env.GetZitadelPAT())
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 300 || resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("authorize expected redirect, got %d: %s", resp.StatusCode, string(body))
	}

	loc := resp.Header.Get("Location")
	if loc == "" {
		return "", errors.New("authorize redirect missing Location header")
	}
	parsed, err := url.Parse(loc)
	if err != nil {
		return "", err
	}
	// Zitadel redirige verso /ui/login/login?authRequestID=<id>.
	// Accettiamo entrambe le grafie per robustezza fra versioni.
	for _, key := range []string{"authRequestID", "authRequest"} {
		if id := parsed.Query().Get(key); id != "" {
			return id, nil
		}
	}
	// Fallback: Zitadel a volte mette l'id come ultimo segmento del path.
	segs := strings.Split(strings.TrimRight(parsed.Path, "/"), "/")
	if len(segs) > 0 {
		last := segs[len(segs)-1]
		if last != "" && last != "login" {
			return last, nil
		}
	}
	return "", fmt.Errorf("authRequest id not found in redirect: %s", loc)
}

func extractCodeFromCallback(callbackURL string) (string, error) {
	parsed, err := url.Parse(callbackURL)
	if err != nil {
		return "", err
	}
	code := parsed.Query().Get("code")
	if code == "" {
		// Code potrebbe essere nel fragment per response_mode=fragment.
		if parsed.Fragment != "" {
			frag, _ := url.ParseQuery(parsed.Fragment)
			code = frag.Get("code")
		}
	}
	if code == "" {
		return "", fmt.Errorf("callback url has no code: %s", callbackURL)
	}
	return code, nil
}

func exchangeCodeForTokens(code, verifier string) (*TokenSet, error) {
	host := normalizeHTTPHost(env.GetZitadelHost())
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", env.GetZitadelOIDCRedirectURI())
	form.Set("client_id", env.GetZitadelOIDCClientID())
	form.Set("code_verifier", verifier)
	if secret := env.GetZitadelOIDCClientSecret(); secret != "" {
		form.Set("client_secret", secret)
	}

	req, err := http.NewRequest(http.MethodPost, host+"/oauth/v2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	var ts TokenSet
	if err := json.Unmarshal(body, &ts); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}
	return &ts, nil
}

func isPasswordError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	// Zitadel emette "COMMAND-3M0fs" su password invalida (vedi commento nel proto).
	return strings.Contains(msg, "command-3m0fs") ||
		strings.Contains(msg, "password") && strings.Contains(msg, "invalid")
}

func isUserNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not found") || strings.Contains(msg, "notfound")
}

