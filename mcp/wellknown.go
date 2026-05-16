package mcp

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

const (
	// upstreamAuthorize: il vero /authorize di Zitadel.
	upstreamAuthorize = oidcIssuer + "/oauth/v2/authorize"
	// tokenEndpoint: lo esponiamo direttamente perche` non c'e` nulla da
	// modificare sulla richiesta di scambio code → token.
	tokenEndpoint = oidcIssuer + "/oauth/v2/token"
	// requiredScopes: scope minimi che ci servono per identificare l'utente
	// PB collegato al token. "openid" e` sempre richiesto da OIDC, "email"
	// (+ "profile") serve a noi per il mapping users.email.
	requiredScopes = "openid email profile"
)

// WellKnownProtectedResourceHandler handles GET /.well-known/oauth-protected-resource.
// RFC 9728 — tells MCP clients where the authorization server is.
func WellKnownProtectedResourceHandler(app core.App) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		appURL := strings.TrimRight(app.Settings().Meta.AppURL, "/")
		return e.JSON(200, map[string]any{
			"resource":                 appURL + "/mcp",
			"authorization_servers":    []string{oidcIssuer},
			"bearer_methods_supported": []string{"header"},
		})
	}
}

// WellKnownAuthServerHandler handles GET /.well-known/oauth-authorization-server.
// RFC 8414. authorization_endpoint punta al NOSTRO /authorize (non a quello
// di Zitadel diretto) cosi` possiamo iniettare lo scope `email`/`profile`
// necessario al mapping users.email lato server.
func WellKnownAuthServerHandler() func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		base := publicBase(e)
		return e.JSON(200, map[string]any{
			"issuer":                                oidcIssuer,
			"authorization_endpoint":                base + "/authorize",
			"token_endpoint":                        tokenEndpoint,
			"jwks_uri":                              jwksURL,
			"scopes_supported":                      []string{"openid", "profile", "email", "offline_access"},
			"response_types_supported":              []string{"code"},
			"code_challenge_methods_supported":      []string{"S256"},
			"token_endpoint_auth_methods_supported": []string{"none", "client_secret_post", "client_secret_basic"},
		})
	}
}

// AuthorizeRedirectHandler handles GET /authorize.
// Inietta `email` e `profile` nello scope richiesto al cliente (per
// garantire che il JWT che ci tornera` contenga la claim email, necessaria
// al mapping con users PB), poi 302 a Zitadel.
func AuthorizeRedirectHandler() func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		q := e.Request.URL.Query()
		q.Set("scope", mergeScopes(q.Get("scope"), requiredScopes))
		return e.Redirect(http.StatusFound, upstreamAuthorize+"?"+q.Encode())
	}
}

// mergeScopes prende lo scope richiesto dal client (space-separated) e
// aggiunge eventuali scope mancanti elencati in extras. Mantiene l'ordine
// originale, aggiunge i mancanti in coda.
func mergeScopes(current, extras string) string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range strings.Fields(current) {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	for _, s := range strings.Fields(extras) {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return strings.Join(out, " ")
}

// publicBase ricava lo scheme+host pubblico da una RequestEvent in modo
// robusto rispetto a reverse-proxy che terminano TLS upstream.
func publicBase(e *core.RequestEvent) string {
	scheme := "https"
	r := e.Request
	if r.TLS == nil && !strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "http"
	}
	u := &url.URL{Scheme: scheme, Host: r.Host}
	return u.String()
}
