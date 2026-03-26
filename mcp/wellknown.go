package mcp

import (
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// WellKnownHandler returns the handler for GET /.well-known/oauth-protected-resource.
//
// This is required by the MCP authorization spec (RFC 9728) so that clients
// like Claude Web can discover the real authorization server (auth.mensa.it)
// instead of falling back to {mcp_server_origin}/authorize.
//
// Response example:
//
//	{
//	  "resource":                 "https://svc.mensa.it/mcp",
//	  "authorization_servers":    ["https://auth.mensa.it"],
//	  "bearer_methods_supported": ["header"]
//	}
func WellKnownHandler(app core.App) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		appURL := strings.TrimRight(app.Settings().Meta.AppURL, "/")
		return e.JSON(200, map[string]any{
			"resource":                 appURL + "/mcp",
			"authorization_servers":    []string{oidcIssuer},
			"bearer_methods_supported": []string{"header"},
		})
	}
}
