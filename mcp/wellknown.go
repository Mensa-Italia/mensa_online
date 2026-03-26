package mcp

import (
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

const (
	authorizationEndpoint = oidcIssuer + "/oauth/v2/authorize"
	tokenEndpoint         = oidcIssuer + "/oauth/v2/token"
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
// RFC 8414 — some clients (including Claude Web) check this before the protected-resource
// endpoint and expect to find the authorization_endpoint directly here.
func WellKnownAuthServerHandler() func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		return e.JSON(200, map[string]any{
			"issuer":                 oidcIssuer,
			"authorization_endpoint": authorizationEndpoint,
			"token_endpoint":         tokenEndpoint,
			"jwks_uri":               jwksURL,
			"scopes_supported":       []string{"openid", "profile", "email", "offline_access"},
			"response_types_supported":              []string{"code"},
			"code_challenge_methods_supported":      []string{"S256"},
			"token_endpoint_auth_methods_supported": []string{"none", "client_secret_post", "client_secret_basic"},
		})
	}
}

// AuthorizeRedirectHandler handles GET /authorize.
// Claude Web falls back to {mcp_origin}/authorize when it can't (or doesn't)
// discover the real authorization server. This handler forwards the request
// with all its query parameters to the actual auth.mensa.it authorization
// endpoint via a 302 redirect.
func AuthorizeRedirectHandler() func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		target := authorizationEndpoint
		if q := e.Request.URL.RawQuery; q != "" {
			target += "?" + q
		}
		return e.Redirect(http.StatusFound, target)
	}
}
