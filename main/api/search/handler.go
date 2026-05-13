package search

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/pocketbase/pocketbase/core"
	"golang.org/x/sync/errgroup"
	"mensadb/tools/dbtools"
	"mensadb/tools/search"
)

var allTypes = []string{"event", "sig", "deal", "document", "user"}

// collectionFor maps a search type to its PocketBase collection name.
func collectionFor(typ string) string {
	switch typ {
	case "sig":
		return "sigs"
	case "document":
		return "documents"
	case "event":
		return "events"
	case "deal":
		return "deals"
	case "user":
		return "users"
	default:
		return typ
	}
}

type searchRequest struct {
	Q            string   `json:"q"`
	Types        []string `json:"types"`
	Region       string   `json:"region"`
	LimitPerType int      `json:"limit_per_type"`
	Hydrate      *bool    `json:"hydrate"`
}

type searchResponse struct {
	Query   string               `json:"query"`
	Total   int                  `json:"total"`
	Results map[string][]Item    `json:"results"`
}

func searchHandler(e *core.RequestEvent) error {
	isLogged, authUser := dbtools.UserIsLoggedIn(e)
	if !isLogged {
		return e.JSON(401, map[string]string{"error": "Unauthorized"})
	}

	if !allowReq(authUser.Id) {
		return e.JSON(429, map[string]string{"error": "Rate limit exceeded"})
	}

	// Parse body
	body, err := io.ReadAll(io.LimitReader(e.Request.Body, 64*1024))
	if err != nil {
		return e.JSON(400, map[string]string{"error": "cannot read body"})
	}
	defer func() { _ = e.Request.Body.Close() }()

	var req searchRequest
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			return e.JSON(400, map[string]string{"error": "invalid JSON"})
		}
	}

	// Validate / normalize
	req.Q = strings.TrimSpace(req.Q)
	if len(req.Q) > 256 {
		req.Q = req.Q[:256]
	}
	if req.Q == "" && req.Region == "" && len(req.Types) == 0 {
		return e.JSON(400, map[string]string{"error": "provide q or at least one filter"})
	}

	if len(req.Types) == 0 {
		req.Types = allTypes
	}

	if req.LimitPerType <= 0 {
		req.LimitPerType = 10
	}
	if req.LimitPerType > 50 {
		req.LimitPerType = 50
	}

	hydrateResults := true
	if req.Hydrate != nil {
		hydrateResults = *req.Hydrate
	}

	// Fetch auth user record for permission checks
	authUserRec, err := e.App.FindRecordById("users", authUser.Id)
	if err != nil {
		// Non-fatal: treat as nil (members-only content will be blocked)
		authUserRec = nil
	}

	// Query Bleve
	queryLimit := req.LimitPerType * max(len(req.Types), len(allTypes))
	hits, err := search.Query(req.Q, search.Filters{Types: req.Types, Region: req.Region}, queryLimit)
	if err != nil {
		return fmt.Errorf("search api: query: %w", err)
	}

	// Group by type, preserve score order
	grouped := make(map[string][]search.Result, len(req.Types))
	scoreMap := make(map[string]float64, len(hits))
	for _, h := range hits {
		grouped[h.Type] = append(grouped[h.Type], h)
		scoreMap[h.ID] = h.Score
	}

	// Fan-out: one goroutine per type
	var mu sync.Mutex
	results := make(map[string][]Item, len(req.Types))
	// Pre-populate all requested types with empty slices
	for _, typ := range req.Types {
		results[typ] = []Item{}
	}

	vis := typeVisibility

	g := new(errgroup.Group)
	for _, typ := range req.Types {
		typ := typ
		g.Go(func() error {
			typHits := grouped[typ]
			if len(typHits) == 0 {
				return nil
			}

			ids := make([]string, 0, len(typHits))
			for _, h := range typHits {
				ids = append(ids, h.ID)
			}

			recs, err := e.App.FindRecordsByIds(collectionFor(typ), ids)
			if err != nil {
				// Non-fatal: log and return empty for this type
				e.App.Logger().Warn("search api: FindRecordsByIds failed", "type", typ, "err", err)
				return nil
			}

			meta := vis[typ]
			var items []Item
			for _, rec := range recs {
				if !allow(authUserRec, meta.visibility, meta.requiredPower) {
					continue
				}
				sc := scoreMap[rec.Id]
				var item Item
				if hydrateResults {
					item = hydrateRecord(typ, rec, sc)
				} else {
					item = minimalItem(rec.Id, sc)
				}
				items = append(items, item)
				if len(items) >= req.LimitPerType {
					break
				}
			}

			if items == nil {
				items = []Item{}
			}

			mu.Lock()
			results[typ] = items
			mu.Unlock()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return fmt.Errorf("search api: fan-out: %w", err)
	}

	total := 0
	for _, items := range results {
		total += len(items)
	}

	return e.JSON(200, searchResponse{
		Query:   req.Q,
		Total:   total,
		Results: results,
	})
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
