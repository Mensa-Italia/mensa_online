package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/pocketbase/pocketbase/core"

	"mensadb/tools/search"
)

// searchHit e` un risultato compatto per il modello chiamante: id + type
// + label leggibile. Per i dettagli si chiama get_<type>(id).
type searchHit struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Label string `json:"label,omitempty"`
}

func registerSearchTools(s *server.MCPServer, app core.App) {
	// Global
	registerSearch(s, app, nil)
	// Per-type
	for i := range allTypes {
		t := &allTypes[i]
		registerSearch(s, app, t)
	}
}

func registerSearch(s *server.MCPServer, app core.App, only *searchableType) {
	var (
		toolName    string
		description string
	)
	if only == nil {
		keys := make([]string, 0, len(allTypes))
		for _, t := range allTypes {
			keys = append(keys, t.Key)
		}
		toolName = "search"
		description = "Bleve full-text search GLOBAL su tutti i tipi indicizzati: " +
			strings.Join(keys, ", ") + ". " +
			"Analyzer italiano (stemming) + fuzzy 1-edit per typo. " +
			"Usa `types` per restringere a uno o piu` tipi, `region` per " +
			"scope regionale (es. \"Lombardia\"). " +
			"Restituisce id+type+label; per il record completo chiamare get_<type>(id)."
	} else {
		toolName = "search_" + snake(only.Plural)
		description = "Bleve full-text search ristretto a " + only.Plural + ". " +
			only.Description + " Analyzer italiano + fuzzy 1-edit."
	}

	opts := []mcp.ToolOption{
		mcp.WithDescription(description),
		mcp.WithString("q",
			mcp.Required(),
			mcp.Description("Query string (italiano preferito, max 256 chars)."),
		),
		mcp.WithString("region",
			mcp.Description("Regione italiana opzionale (es. \"Lombardia\")."),
		),
		mcp.WithNumber("limit",
			mcp.Description("Max risultati (default 10, max 50)."),
		),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
	}
	if only == nil {
		keys := make([]string, 0, len(allTypes))
		for _, t := range allTypes {
			keys = append(keys, t.Key)
		}
		opts = append(opts, mcp.WithString("types",
			mcp.Description("Csv di tipi da filtrare. Valori validi: "+strings.Join(keys, ", ")),
		))
	}

	s.AddTool(mcp.NewTool(toolName, opts...), makeSearchHandler(app, only))
}

func makeSearchHandler(app core.App, only *searchableType) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims, ok := ClaimsFromContext(ctx)
		if !ok {
			return nil, fmt.Errorf("unauthorized: missing MCP auth context")
		}

		q := strings.TrimSpace(req.GetString("q", ""))
		if q == "" {
			return nil, fmt.Errorf("q is required")
		}
		if len(q) > 256 {
			q = q[:256]
		}
		region := strings.TrimSpace(req.GetString("region", ""))
		limit := int(req.GetFloat("limit", 10))
		if limit < 1 {
			limit = 10
		}
		if limit > 50 {
			limit = 50
		}

		var types []string
		if only != nil {
			types = []string{only.Key}
		} else if raw := req.GetString("types", ""); raw != "" {
			for _, t := range strings.Split(raw, ",") {
				if t = strings.TrimSpace(t); t != "" {
					types = append(types, t)
				}
			}
		} else {
			types = make([]string, 0, len(allTypes))
			for _, tt := range allTypes {
				types = append(types, tt.Key)
			}
		}

		hits, err := search.Query(q, search.Filters{Types: types, Region: region}, limit)
		if err != nil {
			return nil, fmt.Errorf("bleve query: %w", err)
		}

		// Idrata via pbCollectionGet per ogni hit: cosi` le viewRule della
		// collection vengono applicate dall'utente MCP autenticato. Se PB
		// ritorna 404 (non autorizzato a vedere) si scarta silenziosamente.
		out := make([]searchHit, 0, len(hits))
		for _, h := range hits {
			t := typeByKey(h.Type)
			if t == nil {
				continue
			}
			status, body, err := pbCollectionGet(ctx, app, claims, t.Collection, h.ID, nil)
			if err != nil || status != 200 {
				continue
			}
			label := extractLabel(h.Type, body)
			out = append(out, searchHit{ID: h.ID, Type: h.Type, Label: label})
		}

		return jsonResult(map[string]any{
			"query":   q,
			"total":   len(out),
			"results": out,
		})
	}
}

// extractLabel ricava una label leggibile dal record PB serializzato,
// scegliendo per ogni tipo i campi piu` utili senza forzare il modello a
// ri-ispezionare strutture. Per i dettagli si fa get_<type>(id).
func extractLabel(typ string, body []byte) string {
	m := map[string]any{}
	if err := json.Unmarshal(body, &m); err != nil {
		return ""
	}
	get := func(k string) string {
		if v, ok := m[k].(string); ok {
			return v
		}
		return ""
	}
	switch typ {
	case "event":
		return joinNonEmpty(get("name"), get("when_start"))
	case "sig":
		return joinNonEmpty(get("name"), get("group_type"))
	case "deal":
		return joinNonEmpty(get("name"), get("commercial_sector"))
	case "document":
		return joinNonEmpty(get("name"), get("category"))
	case "member":
		return joinNonEmpty(get("name"), get("city"), get("state"))
	case "org_group":
		return get("title")
	case "org_role":
		return get("role")
	case "quid_article":
		return joinNonEmpty(get("title"), get("category_name"))
	case "quid_issue":
		return get("name")
	case "podcast":
		return get("title")
	case "podcast_episode":
		return joinNonEmpty(get("title"), get("published_at"))
	case "linktree_link":
		return joinNonEmpty(get("title"), get("url"))
	}
	return get("name")
}

func joinNonEmpty(parts ...string) string {
	out := []string{}
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " · ")
}

func jsonResult(v any) (*mcp.CallToolResult, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("json marshal: %w", err)
	}
	return mcp.NewToolResultText(string(b)), nil
}

