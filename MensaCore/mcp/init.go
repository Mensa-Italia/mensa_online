package mcp

import (
	"context"
	"fmt"
	"net/http"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/pocketbase/pocketbase/core"
)

// Init costruisce il server MCP, registra tool e ritorna l'http.Handler
// pronto per essere montato dal router PB. Prima di registrare il route,
// il chiamante (main.go) DEVE aver chiamato SetServerAddr con l'indirizzo
// del server PB cosi` i tool che fanno proxy via loopback funzionano.
func Init(app core.App) http.Handler {
	s := server.NewMCPServer("Mensa Italia MCP", "1.0.0",
		server.WithToolCapabilities(true),
	)

	s.AddTool(
		mcp.NewTool("ping",
			mcp.WithDescription("Returns pong – useful to verify the MCP endpoint is reachable."),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithIdempotentHintAnnotation(true),
		),
		func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText("pong"), nil
		},
	)

	// whoami: ritorna lo stato di identificazione dell'utente MCP. Utile per
	// diagnosticare casi in cui le tool dicono "MCP user non risolvibile"
	// — mostra cosa il backend vede del token (email, sub, azp) e se ha
	// trovato un PB users record con cui impersonare.
	s.AddTool(
		mcp.NewTool("whoami",
			mcp.WithDescription(
				"Diagnostico: ritorna le info di identificazione dell'utente MCP "+
					"corrente (email/sub dal JWT Zitadel + se e` stato mappato a "+
					"un users PB). Chiamare quando una list_X o get_X fallisce "+
					"con \"MCP user non risolvibile\".",
			),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithIdempotentHintAnnotation(true),
		),
		func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			claims, ok := ClaimsFromContext(ctx)
			if !ok {
				return nil, fmt.Errorf("no MCP claims in request context")
			}
			info := map[string]any{
				"zitadel": map[string]any{
					"sub":            claims.Subject,
					"email":          claims.Email,
					"email_verified": claims.EmailVerified,
					"azp":            claims.AuthorizedParty,
					"issuer":         claims.Issuer,
				},
			}
			bearer, _ := BearerFromContext(ctx)
			if user, err := resolveUserFromClaimsCtx(ctx, app, claims, bearer); err == nil {
				info["pb_user"] = map[string]any{
					"id":       user.Id,
					"email":    user.Email(),
					"username": user.GetString("username"),
					"name":     user.GetString("name"),
				}
				info["status"] = "mapped"
			} else {
				info["pb_user"] = nil
				info["status"] = "unmapped"
				info["error"] = err.Error()
			}
			return jsonResult(info)
		},
	)

	// 1. Search Bleve: globale + per-tipo.
	registerSearchTools(s, app)

	// 2. Collection access: list_<plural> + get_<singular> per ogni tipo.
	//    Proxy verso PB → rule applicate automaticamente.
	registerCollectionTools(s, app)

	return newAuthMiddleware(server.NewStreamableHTTPServer(s))
}
