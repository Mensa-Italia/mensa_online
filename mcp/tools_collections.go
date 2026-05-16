package mcp

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/pocketbase/pocketbase/core"
)

const (
	defaultListLimit = 50
	maxListLimit     = 200
)

// registerCollectionTools registra per ogni tipo in allTypes due tool MCP:
//
//   - list_<plural>(filter?, sort?, expand?, page?, perPage?)
//   - get_<singular>(id, expand?)
//
// I tool sono PROXY verso PocketBase via loopback HTTP, impersonando l'utente
// MCP autenticato. Cosi` listRule / viewRule definite sulla collection si
// applicano automaticamente — zero logica di rule duplicata lato MCP.
func registerCollectionTools(s *server.MCPServer, app core.App) {
	for i := range allTypes {
		t := &allTypes[i]
		registerListTool(s, app, t)
		registerGetTool(s, app, t)
	}
}

func registerListTool(s *server.MCPServer, app core.App, t *searchableType) {
	toolName := snake("list_" + t.Plural)
	desc := "List " + t.Plural + ". " + t.Description +
		" Proxy verso PB: rispetta automaticamente le listRule della collection. " +
		"Usa search_" + snake(t.Plural) + " per ricerca full-text Bleve invece di filtri LIKE."

	tool := mcp.NewTool(toolName,
		mcp.WithDescription(desc),
		mcp.WithString("filter",
			mcp.Description("PocketBase filter expression opzionale (es. \"name ~ 'concorso'\" o \"created >= '2026-01-01'\")."),
		),
		mcp.WithString("sort",
			mcp.Description("Sort opzionale formato PB (es. \"-created\" o \"name,-published\")."),
		),
		mcp.WithString("expand",
			mcp.Description("Relazioni da espandere, csv (es. \"owner,position\")."),
		),
		mcp.WithNumber("page",
			mcp.Description("Pagina 1-based (default 1)."),
		),
		mcp.WithNumber("perPage",
			mcp.Description(fmt.Sprintf("Record per pagina (default %d, max %d).", defaultListLimit, maxListLimit)),
		),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
	)
	s.AddTool(tool, makeListHandler(app, t))
}

func registerGetTool(s *server.MCPServer, app core.App, t *searchableType) {
	toolName := snake("get_" + t.Singular)
	desc := "Fetch a single " + t.Singular + " by id, all fields. " + t.Description +
		" Proxy verso PB: rispetta viewRule della collection."

	tool := mcp.NewTool(toolName,
		mcp.WithDescription(desc),
		mcp.WithString("id",
			mcp.Required(),
			mcp.Description("Record id (15-char alphanumeric)."),
		),
		mcp.WithString("expand",
			mcp.Description("Relazioni da espandere, csv (es. \"owner\")."),
		),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(true),
	)
	s.AddTool(tool, makeGetHandler(app, t))
}

func makeListHandler(app core.App, t *searchableType) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims, ok := ClaimsFromContext(ctx)
		if !ok {
			return nil, fmt.Errorf("unauthorized: missing MCP auth context")
		}

		filter := strings.TrimSpace(req.GetString("filter", ""))
		sortSpec := strings.TrimSpace(req.GetString("sort", ""))
		expand := strings.TrimSpace(req.GetString("expand", ""))

		perPage := int(req.GetFloat("perPage", float64(defaultListLimit)))
		if perPage < 1 {
			perPage = defaultListLimit
		}
		if perPage > maxListLimit {
			perPage = maxListLimit
		}
		page := int(req.GetFloat("page", 1))
		if page < 1 {
			page = 1
		}

		q := url.Values{}
		if filter != "" {
			q.Set("filter", filter)
		}
		if sortSpec != "" {
			q.Set("sort", sortSpec)
		}
		if expand != "" {
			q.Set("expand", expand)
		}
		q.Set("page", strconv.Itoa(page))
		q.Set("perPage", strconv.Itoa(perPage))

		status, body, err := pbCollectionList(ctx, app, claims, t.Collection, q)
		if err != nil {
			return nil, err
		}
		raw, err := rawJSON(status, body)
		if err != nil {
			return nil, err
		}
		return mcp.NewToolResultText(raw), nil
	}
}

func makeGetHandler(app core.App, t *searchableType) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims, ok := ClaimsFromContext(ctx)
		if !ok {
			return nil, fmt.Errorf("unauthorized: missing MCP auth context")
		}
		id := strings.TrimSpace(req.GetString("id", ""))
		if id == "" {
			return nil, fmt.Errorf("id is required")
		}

		q := url.Values{}
		if expand := strings.TrimSpace(req.GetString("expand", "")); expand != "" {
			q.Set("expand", expand)
		}

		status, body, err := pbCollectionGet(ctx, app, claims, t.Collection, id, q)
		if err != nil {
			return nil, err
		}
		raw, err := rawJSON(status, body)
		if err != nil {
			return nil, err
		}
		return mcp.NewToolResultText(raw), nil
	}
}

// snake produce identifier MCP-friendly: lowercase + spazi → underscore.
// Necessario perche` Singular/Plural in allTypes possono contenere spazi
// (es. "Quid issues" → "quid_issues").
func snake(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "_")
	return s
}
