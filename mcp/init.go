package mcp

import (
	"context"
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

	// 1. Search Bleve: globale + per-tipo.
	registerSearchTools(s, app)

	// 2. Collection access: list_<plural> + get_<singular> per ogni tipo.
	//    Proxy verso PB → rule applicate automaticamente.
	registerCollectionTools(s, app)

	return newAuthMiddleware(server.NewStreamableHTTPServer(s))
}
