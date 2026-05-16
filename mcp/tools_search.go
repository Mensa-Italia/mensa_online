package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/pocketbase/pocketbase/core"

	"mensadb/tools/search"
)

// searchResultItem e` la rappresentazione minimale di un hit per l'output
// MCP. Niente score Bleve grezzo: e` un dato interno di ranking che
// confonderebbe il modello chiamante. L'ordine della slice riflette gia` il
// ranking.
type searchResultItem struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Title    string `json:"title,omitempty"`
	Subtitle string `json:"subtitle,omitempty"`
	URL      string `json:"url,omitempty"`
}

// supportedSearchTypes elenca i tipi indicizzati attualmente. Tenuto in sync
// con main/api/search/handler.go::allTypes.
var supportedSearchTypes = []string{
	"event", "sig", "deal", "document", "member",
	"org_group", "org_role",
	"quid_issue", "quid_article",
	"podcast", "podcast_episode",
	"linktree_link",
}

func registerSearchTool(s *server.MCPServer, app core.App) {
	s.AddTool(
		mcp.NewTool("search",
			mcp.WithDescription(
				"Full-text search across Mensa Italia content: events, "+
					"groups (sigs / local / chat), deals, documents, members, "+
					"organigramma (groups + roles), Quid magazine (issues + "+
					"articles), podcasts (series + episodes), local-office "+
					"linktree links. Italian-aware analyzer with stemming, "+
					"plus fuzzy matching (1 Levenshtein edit) to catch "+
					"typos. Use the `types` filter to narrow to specific "+
					"kinds, `region` to scope to an Italian region "+
					"(e.g. \"Lombardia\").",
			),
			mcp.WithString("q",
				mcp.Required(),
				mcp.Description("Query string. Italian queries work best. Max 256 chars."),
			),
			mcp.WithString("types",
				mcp.Description(
					"Optional comma-separated list of types to restrict the search to. "+
						"Allowed: "+strings.Join(supportedSearchTypes, ", ")+
						". If empty, all types are searched.",
				),
			),
			mcp.WithString("region",
				mcp.Description(
					"Optional Italian region name (e.g. \"Lombardia\") to restrict the "+
						"search to records tagged with that region.",
				),
			),
			mcp.WithNumber("limit_per_type",
				mcp.Description("Max results per type. Default 5, max 25."),
			),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithIdempotentHintAnnotation(true),
			mcp.WithOpenWorldHintAnnotation(true),
		),
		makeSearchTool(app),
	)
}

func makeSearchTool(app core.App) server.ToolHandlerFunc {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		q := strings.TrimSpace(req.GetString("q", ""))
		if q == "" {
			return nil, fmt.Errorf("q is required")
		}
		if len(q) > 256 {
			q = q[:256]
		}

		typesRaw := req.GetString("types", "")
		region := strings.TrimSpace(req.GetString("region", ""))
		limitPerType := int(req.GetFloat("limit_per_type", 5))
		if limitPerType < 1 {
			limitPerType = 5
		}
		if limitPerType > 25 {
			limitPerType = 25
		}

		var types []string
		if typesRaw != "" {
			for _, t := range strings.Split(typesRaw, ",") {
				t = strings.TrimSpace(t)
				if t != "" {
					types = append(types, t)
				}
			}
		}
		if len(types) == 0 {
			types = supportedSearchTypes
		}

		hits, err := search.Query(q, search.Filters{Types: types, Region: region}, limitPerType*len(types))
		if err != nil {
			return nil, fmt.Errorf("search query: %w", err)
		}

		// Raggruppa per tipo preservando l'ordine di score, cap a limitPerType.
		perType := make(map[string][]search.Result, len(types))
		for _, h := range hits {
			if len(perType[h.Type]) >= limitPerType {
				continue
			}
			perType[h.Type] = append(perType[h.Type], h)
		}

		// Idrata in struttura piatta tipo->[]item.
		appURL := app.Settings().Meta.AppURL
		results := make(map[string][]searchResultItem, len(types))
		total := 0
		for _, typ := range types {
			tHits := perType[typ]
			if len(tHits) == 0 {
				results[typ] = []searchResultItem{}
				continue
			}
			items := make([]searchResultItem, 0, len(tHits))
			for _, h := range tHits {
				rec, err := app.FindRecordById(collectionForType(typ), h.ID)
				if err != nil || rec == nil {
					continue
				}
				items = append(items, hydrateForMCP(appURL, typ, rec))
			}
			results[typ] = items
			total += len(items)
		}

		return jsonResult(map[string]any{
			"query":   q,
			"total":   total,
			"results": results,
		})
	}
}

// collectionForType e` la versione MCP del mapping type->collection
// (duplicata da main/api/search/handler.go per non creare un import ciclico).
func collectionForType(t string) string {
	switch t {
	case "sig":
		return "sigs"
	case "document":
		return "documents"
	case "event":
		return "events"
	case "deal":
		return "deals"
	case "member":
		return "members_registry"
	case "org_group":
		return "org_chart_groups"
	case "org_role":
		return "org_chart_members"
	case "quid_article":
		return "quid_articles"
	case "quid_issue":
		return "quid_issues"
	case "podcast":
		return "podcasts"
	case "podcast_episode":
		return "podcast_episodes"
	case "linktree_link":
		return "local_offices_links"
	default:
		return t
	}
}

// hydrateForMCP costruisce un searchResultItem leggibile dal modello. Titolo
// + subtitle informativo + URL ufficiale se costruibile.
func hydrateForMCP(appURL, typ string, rec *core.Record) searchResultItem {
	item := searchResultItem{ID: rec.Id, Type: typ}
	switch typ {
	case "event":
		item.Title = rec.GetString("name")
		if t := rec.GetDateTime("when_start"); !t.IsZero() {
			item.Subtitle = t.Time().Format("02 Jan 2006")
		}
	case "sig":
		item.Title = rec.GetString("name")
		item.Subtitle = rec.GetString("group_type")
	case "deal":
		item.Title = rec.GetString("name")
		item.Subtitle = rec.GetString("commercial_sector")
	case "document":
		item.Title = rec.GetString("name")
		item.Subtitle = rec.GetString("category")
	case "member":
		item.Title = rec.GetString("name")
		city := rec.GetString("city")
		state := rec.GetString("state")
		switch {
		case city != "" && state != "":
			item.Subtitle = city + ", " + state
		case state != "":
			item.Subtitle = state
		default:
			item.Subtitle = city
		}
	case "org_group":
		item.Title = rec.GetString("title")
	case "org_role":
		item.Title = rec.GetString("role")
		item.Subtitle = rec.GetString("group")
	case "quid_article":
		item.Title = rec.GetString("title")
		item.Subtitle = rec.GetString("category_name")
		item.URL = rec.GetString("link")
	case "quid_issue":
		item.Title = rec.GetString("name")
		if c := rec.GetInt("articles_count"); c > 0 {
			item.Subtitle = fmt.Sprintf("%d articoli", c)
		}
	case "podcast":
		item.Title = rec.GetString("title")
	case "podcast_episode":
		item.Title = rec.GetString("title")
		if t := rec.GetDateTime("published_at"); !t.IsZero() {
			item.Subtitle = t.Time().Format("02 Jan 2006")
		}
	case "linktree_link":
		item.Title = rec.GetString("title")
		item.URL = rec.GetString("url")
	}
	if item.URL == "" {
		// Fallback: link al record PB via API (utile come "approfondisci")
		item.URL = fmt.Sprintf("%s/api/collections/%s/records/%s", strings.TrimRight(appURL, "/"), collectionForType(typ), rec.Id)
	}
	return item
}
