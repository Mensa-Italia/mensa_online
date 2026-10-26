package mcp

import (
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

const (
	// upstreamAuthorize: il vero /authorize di Zitadel.
	upstreamAuthorize = oidcIssuer + "/oauth/v2/authorize"
	// tokenEndpoint: lo esponiamo direttamente perche` non c'e` nulla da
	// modificare sulla richiesta di scambio code → token.
	tokenEndpoint = oidcIssuer + "/oauth/v2/token"
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
// RFC 8414. authorization_endpoint punta direttamente a Zitadel: l'identita`
// dell'utente viene poi risolta dal backend MCP via /oidc/v1/userinfo +
// tabella di cache user_zitadel_auth.
func WellKnownAuthServerHandler() func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		return e.JSON(200, map[string]any{
			"issuer":                                oidcIssuer,
			"authorization_endpoint":                upstreamAuthorize,
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
// Forward semplice a Zitadel: lo teniamo per i client (es. Claude Web)
// che fanno fallback su {mcp_origin}/authorize quando non leggono il
// metadata. Niente injection di scope.
func AuthorizeRedirectHandler() func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		target := upstreamAuthorize
		if q := e.Request.URL.RawQuery; q != "" {
			target += "?" + q
		}
		return e.Redirect(http.StatusFound, target)
	}
}
