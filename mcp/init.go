package mcp

import (
	"context"
	"net/http"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/pocketbase/pocketbase/core"
)

func Init(app core.App) http.Handler {
	s := server.NewMCPServer("Mensa App MCP", "0.0.1",
		server.WithToolCapabilities(true),
	)

	s.AddTool(
		mcp.NewTool("ping",
			mcp.WithDescription("Returns pong – useful to verify the MCP endpoint is reachable"),
		),
		func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText("pong"), nil
		},
	)

	registerDocumentTools(s, app)
	registerGroupTools(s, app)

	return newAuthMiddleware(server.NewStreamableHTTPServer(s))
}
