// Debug-only: replica il flusso di tools/zauth.LoginWithPassword con log
// step-by-step. Lanciare con:
//
//	ZITADEL_HOST=https://...                  \
//	ZITADEL_PAT=...                           \
//	ZITADEL_ORGANIZATION_ID=...               \
//	ZITADEL_OIDC_CLIENT_ID=...                \
//	ZITADEL_OIDC_CLIENT_SECRET=...   (opz)    \
//	ZITADEL_OIDC_REDIRECT_URI=https://...     \
//	go run ./cmd/debug_zitadel_login \
//	    -email <user@example.com> -password <password>
//
// Stampa cosa risponde Zitadel ad ogni step.
package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/zitadel/zitadel-go/v3/pkg/client"
	oidcV2 "github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/oidc/v2"
	sessionV2 "github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/session/v2"
	v21 "github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/object/v2"
	user "github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/user/v2"
	"github.com/zitadel/zitadel-go/v3/pkg/zitadel"
)

func mustEnv(k string) string {
	v := os.Getenv(k)
	if v == "" {
		log.Fatalf("env %s mancante", k)
	}
	return v
}

func main() {
	email := flag.String("email", "", "login email")
	password := flag.String("password", "", "password")
	flag.Parse()
	if *email == "" || *password == "" {
		log.Fatal("usa -email e -password")
	}

	rawHost := strings.TrimRight(mustEnv("ZITADEL_HOST"), "/")
	// Per il gRPC SDK la zitadel.New() vuole il domain (es. auth.mensa.it),
	// mentre le chiamate HTTP a /oauth/v2/* vogliono lo schema completo.
	grpcDomain := strings.TrimPrefix(strings.TrimPrefix(rawHost, "https://"), "http://")
	httpHost := rawHost
	if !strings.HasPrefix(httpHost, "http") {
		httpHost = "https://" + httpHost
	}
	pat := mustEnv("ZITADEL_PAT")
	clientID := mustEnv("ZITADEL_OIDC_CLIENT_ID")
	redirectURI := mustEnv("ZITADEL_OIDC_REDIRECT_URI")
	clientSecret := os.Getenv("ZITADEL_OIDC_CLIENT_SECRET")

	fmt.Println("== STEP 0 == config")
	fmt.Println("  httpHost         =", httpHost)
	fmt.Println("  client_id    =", clientID)
	fmt.Println("  redirect_uri =", redirectURI)
	fmt.Println("  client_secret set =", clientSecret != "")

	ctx := client.BearerTokenCtx(context.Background(), pat)
	apiClient, err := client.New(ctx, zitadel.New(grpcDomain), client.WithAuth(client.PAT(pat)))
	if err != nil {
		log.Fatalf("client init: %v", err)
	}

	fmt.Println("\n== STEP 1 == UserExists(email)")
	listResp, err := apiClient.UserServiceV2().ListUsers(ctx, &user.ListUsersRequest{
		Queries: []*user.SearchQuery{{
			Query: &user.SearchQuery_UserNameQuery{
				UserNameQuery: &user.UserNameQuery{
					UserName: *email,
					Method:   v21.TextQueryMethod_TEXT_QUERY_METHOD_EQUALS_IGNORE_CASE,
				},
			},
		}},
	})
	if err != nil {
		log.Fatalf("ListUsers ERR: %v", err)
	}
	fmt.Printf("  trovati %d utenti\n", len(listResp.Result))
	for _, u := range listResp.Result {
		fmt.Printf("    - id=%s userName=%s state=%s\n", u.UserId, u.Username, u.State)
	}
	if len(listResp.Result) == 0 {
		log.Fatalf("utente non esiste su Zitadel — fallback Area32 sarebbe gestito dall'handler, qui ci fermiamo")
	}

	fmt.Println("\n== STEP 2 == startAuthRequest")
	verifierRaw := make([]byte, 32)
	rand.Read(verifierRaw)
	verifier := base64.RawURLEncoding.EncodeToString(verifierRaw)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	q := url.Values{}
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("response_type", "code")
	q.Set("scope", "openid profile email offline_access")
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	authorizeURL := httpHost + "/oauth/v2/authorize?" + q.Encode()
	fmt.Println("  URL:", authorizeURL)

	httpClient := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, _ := http.NewRequest(http.MethodGet, authorizeURL, nil)
	if lcid := os.Getenv("ZITADEL_LOGIN_CLIENT_USER_ID"); lcid != "" {
		req.Header.Set("x-zitadel-login-client", lcid)
		req.Header.Set("Authorization", "Bearer "+pat)
		fmt.Println("  header x-zitadel-login-client:", lcid)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		log.Fatalf("authorize GET err: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("  HTTP status: %d\n", resp.StatusCode)
	for k, v := range resp.Header {
		fmt.Printf("  header %s: %v\n", k, v)
	}
	fmt.Printf("  body (%d bytes): %s\n", len(body), truncate(string(body), 500))

	loc := resp.Header.Get("Location")
	if loc == "" {
		log.Fatal("authorize: nessun Location header — non possiamo estrarre authRequest")
	}
	parsed, _ := url.Parse(loc)
	authReqID := parsed.Query().Get("authRequestID")
	if authReqID == "" {
		authReqID = parsed.Query().Get("authRequest")
	}
	if authReqID == "" {
		segs := strings.Split(strings.TrimRight(parsed.Path, "/"), "/")
		if len(segs) > 0 {
			last := segs[len(segs)-1]
			if last != "" && last != "login" {
				authReqID = last
			}
		}
	}
	fmt.Println("  Location:", loc)
	fmt.Println("  authRequest id estratto:", authReqID)
	if authReqID == "" {
		log.Fatal("authRequest id non estratto dal Location")
	}

	fmt.Println("\n== STEP 2.5 == GetAuthRequest (verifica visibilita`)")
	garResp, err := apiClient.OIDCServiceV2().GetAuthRequest(ctx, &oidcV2.GetAuthRequestRequest{
		AuthRequestId: authReqID,
	})
	if err != nil {
		fmt.Println("  ERR:", err)
	} else {
		fmt.Println("  ok, client_id:", garResp.AuthRequest.ClientId, "scope:", garResp.AuthRequest.Scope)
	}

	fmt.Println("\n== STEP 3 == CreateSession (user + password)")
	sessResp, err := apiClient.SessionServiceV2().CreateSession(ctx, &sessionV2.CreateSessionRequest{
		Checks: &sessionV2.Checks{
			User: &sessionV2.CheckUser{
				Search: &sessionV2.CheckUser_LoginName{LoginName: *email},
			},
			Password: &sessionV2.CheckPassword{Password: *password},
		},
	})
	if err != nil {
		log.Fatalf("CreateSession ERR: %v", err)
	}
	fmt.Println("  session_id:", sessResp.SessionId)
	fmt.Println("  session_token len:", len(sessResp.SessionToken))

	fmt.Println("\n== STEP 4 == CreateCallback")
	cbResp, err := apiClient.OIDCServiceV2().CreateCallback(ctx, &oidcV2.CreateCallbackRequest{
		AuthRequestId: authReqID,
		CallbackKind: &oidcV2.CreateCallbackRequest_Session{
			Session: &oidcV2.Session{
				SessionId:    sessResp.SessionId,
				SessionToken: sessResp.SessionToken,
			},
		},
	})
	if err != nil {
		log.Fatalf("CreateCallback ERR: %v", err)
	}
	fmt.Println("  callback url:", cbResp.CallbackUrl)
	cbParsed, _ := url.Parse(cbResp.CallbackUrl)
	code := cbParsed.Query().Get("code")
	fmt.Println("  code:", truncate(code, 40))

	fmt.Println("\n== STEP 5 == Token exchange")
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("client_id", clientID)
	form.Set("code_verifier", verifier)
	if clientSecret != "" {
		form.Set("client_secret", clientSecret)
	}
	treq, _ := http.NewRequest(http.MethodPost, httpHost+"/oauth/v2/token", strings.NewReader(form.Encode()))
	treq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tresp, err := http.DefaultClient.Do(treq)
	if err != nil {
		log.Fatalf("token POST err: %v", err)
	}
	defer tresp.Body.Close()
	tbody, _ := io.ReadAll(tresp.Body)
	fmt.Printf("  HTTP status: %d\n", tresp.StatusCode)
	fmt.Printf("  body: %s\n", truncate(string(tbody), 1000))
	if tresp.StatusCode != http.StatusOK {
		log.Fatal("token exchange failed")
	}
	var ts struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(tbody, &ts); err != nil {
		log.Fatalf("parse token: %v", err)
	}
	fmt.Println("\n== OK ==")
	fmt.Println("  access_token  len:", len(ts.AccessToken))
	fmt.Println("  refresh_token len:", len(ts.RefreshToken))
	fmt.Println("  id_token      len:", len(ts.IDToken))
	fmt.Println("  expires_in:", ts.ExpiresIn)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + fmt.Sprintf("... (+%d bytes)", len(s)-n)
}
