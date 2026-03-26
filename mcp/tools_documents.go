package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"mensadb/tools/cdnfiles"
)

// documentItem is the minimal representation returned by list_documents.
type documentItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Category    string `json:"category"`
	Published   string `json:"published"`
	Description string `json:"description"`
}

// documentItemWithSummary adds the AI-generated resume to documentItem.
type documentItemWithSummary struct {
	documentItem
	IaResume string `json:"ia_resume,omitempty"`
}

func registerDocumentTools(s *server.MCPServer, app core.App) {
	// ── 1. list_documents ────────────────────────────────────────────────────
	s.AddTool(
		mcp.NewTool("list_documents",
			mcp.WithDescription(
				"Returns a paginated list of Mensa documents (id, name, category, "+
					"published date, description). Use this as the first step to let "+
					"the model identify which document is relevant before requesting details.",
			),
			mcp.WithString("category",
				mcp.Description(
					"Optional filter. Allowed values: bilanci, elezioni, eventi_progetti, "+
						"materiale_comunicazione, modulistica_contratti, news_pubblicazioni, "+
						"normativa_interna, verbali_delibere, tesoreria_contabilita, document",
				),
			),
			mcp.WithNumber("page", mcp.DefaultNumber(1), mcp.Min(1),
				mcp.Description("Page number (1-based)"),
			),
			mcp.WithNumber("per_page", mcp.DefaultNumber(50), mcp.Min(1), mcp.Max(200),
				mcp.Description("Results per page (max 200)"),
			),
		),
		makeListDocumentsTool(app, false),
	)

	// ── 2. list_documents_with_summaries ─────────────────────────────────────
	s.AddTool(
		mcp.NewTool("list_documents_with_summaries",
			mcp.WithDescription(
				"Same as list_documents but includes the AI-generated summary (ia_resume) "+
					"for each document. Useful for deeper analysis when the title/description "+
					"alone is not enough to identify the right document.",
			),
			mcp.WithString("category",
				mcp.Description("Optional category filter (same values as list_documents)"),
			),
			mcp.WithNumber("page", mcp.DefaultNumber(1), mcp.Min(1),
				mcp.Description("Page number (1-based)"),
			),
			mcp.WithNumber("per_page", mcp.DefaultNumber(20), mcp.Min(1), mcp.Max(50),
				mcp.Description("Results per page (max 50 – responses are larger)"),
			),
		),
		makeListDocumentsTool(app, true),
	)

	// ── 3. search_documents ──────────────────────────────────────────────────
	s.AddTool(
		mcp.NewTool("search_documents",
			mcp.WithDescription(
				"Full-text search across document name, description and AI-generated "+
					"summaries. Returns matching documents with all details including ia_resume.",
			),
			mcp.WithString("query",
				mcp.Required(),
				mcp.Description("Search text (matched against name, description and ia_resume)"),
			),
			mcp.WithString("category",
				mcp.Description("Optional category filter (same values as list_documents)"),
			),
		),
		makeSearchDocumentsTool(app),
	)

	// ── 4. download_document ─────────────────────────────────────────────────
	s.AddTool(
		mcp.NewTool("download_document",
			mcp.WithDescription(
				"Returns a pre-signed download URL (valid for 1 hour) for the file "+
					"attached to a document. Pass the document id obtained from other tools.",
			),
			mcp.WithString("document_id",
				mcp.Required(),
				mcp.Description("Document ID (15-character alphanumeric string)"),
			),
		),
		makeDownloadDocumentTool(app),
	)
}

// ── helpers ───────────────────────────────────────────────────────────────────

// buildDocumentFilter constructs the base filter expression and params map.
// It always excludes hidden documents and optionally filters by category.
func buildDocumentFilter(category string) (string, dbx.Params) {
	filter := "hidden = false"
	params := dbx.Params{}
	if category != "" {
		filter += " && category = {:cat}"
		params["cat"] = category
	}
	return filter, params
}

// loadIaResumes queries documents_elaborated for the given document IDs and
// returns a map of documentID → ia_resume string.
func loadIaResumes(app core.App, docIDs []string) map[string]string {
	if len(docIDs) == 0 {
		return nil
	}
	ids := make([]any, len(docIDs))
	for i, id := range docIDs {
		ids[i] = id
	}
	elaborated, err := app.FindAllRecords("documents_elaborated",
		dbx.In("document", ids...),
	)
	if err != nil {
		return nil
	}
	result := make(map[string]string, len(elaborated))
	for _, e := range elaborated {
		result[e.GetString("document")] = e.GetString("ia_resume")
	}
	return result
}

func toDocumentItem(r *core.Record) documentItem {
	return documentItem{
		ID:          r.Id,
		Name:        r.GetString("name"),
		Category:    r.GetString("category"),
		Published:   r.GetString("published"),
		Description: r.GetString("description"),
	}
}

func jsonResult(v any) (*mcp.CallToolResult, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("json marshal: %w", err)
	}
	return mcp.NewToolResultText(string(b)), nil
}

// ── tool factories ────────────────────────────────────────────────────────────

func makeListDocumentsTool(app core.App, withSummaries bool) server.ToolHandlerFunc {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		category := req.GetString("category", "")
		page := req.GetInt("page", 1)
		perPage := req.GetInt("per_page", 50)
		if withSummaries && perPage > 50 {
			perPage = 50
		}
		if !withSummaries && perPage > 200 {
			perPage = 200
		}

		offset := (page - 1) * perPage
		filter, params := buildDocumentFilter(category)

		records, err := app.FindRecordsByFilter(
			"documents", filter, "-published", perPage, offset, params,
		)
		if err != nil {
			return nil, fmt.Errorf("query documents: %w", err)
		}

		if !withSummaries {
			items := make([]documentItem, len(records))
			for i, r := range records {
				items[i] = toDocumentItem(r)
			}
			return jsonResult(map[string]any{
				"page":           page,
				"per_page":       perPage,
				"returned_count": len(items),
				"documents":      items,
			})
		}

		// with summaries: collect IDs, load ia_resumes in one query
		docIDs := make([]string, len(records))
		for i, r := range records {
			docIDs[i] = r.Id
		}
		resumes := loadIaResumes(app, docIDs)

		items := make([]documentItemWithSummary, len(records))
		for i, r := range records {
			items[i] = documentItemWithSummary{
				documentItem: toDocumentItem(r),
				IaResume:     resumes[r.Id],
			}
		}
		return jsonResult(map[string]any{
			"page":           page,
			"per_page":       perPage,
			"returned_count": len(items),
			"documents":      items,
		})
	}
}

func makeSearchDocumentsTool(app core.App) server.ToolHandlerFunc {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query := req.GetString("query", "")
		if query == "" {
			return nil, fmt.Errorf("query is required")
		}
		category := req.GetString("category", "")

		// Search against name and description directly on documents.
		filter := "hidden = false && (name ~ {:q} || description ~ {:q} || elaborated.ia_resume ~ {:q})"
		params := dbx.Params{"q": query}
		if category != "" {
			filter += " && category = {:cat}"
			params["cat"] = category
		}

		records, err := app.FindRecordsByFilter(
			"documents", filter, "-published", 100, 0, params,
		)
		if err != nil {
			return nil, fmt.Errorf("search documents: %w", err)
		}

		docIDs := make([]string, len(records))
		for i, r := range records {
			docIDs[i] = r.Id
		}
		resumes := loadIaResumes(app, docIDs)

		items := make([]documentItemWithSummary, len(records))
		for i, r := range records {
			items[i] = documentItemWithSummary{
				documentItem: toDocumentItem(r),
				IaResume:     resumes[r.Id],
			}
		}
		return jsonResult(map[string]any{
			"query":          query,
			"returned_count": len(items),
			"documents":      items,
		})
	}
}

func makeDownloadDocumentTool(app core.App) server.ToolHandlerFunc {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		docID := req.GetString("document_id", "")
		if docID == "" {
			return nil, fmt.Errorf("document_id is required")
		}

		record, err := app.FindRecordById("documents", docID)
		if err != nil {
			return nil, fmt.Errorf("document not found: %s", docID)
		}

		if record.GetBool("hidden") {
			return nil, fmt.Errorf("document not found: %s", docID)
		}

		filename := record.GetString("file")
		if filename == "" {
			return nil, fmt.Errorf("document %s has no attached file", docID)
		}

		// S3 key: {collectionId}/{recordId}/{filename}
		s3Key := record.BaseFilesPath() + "/" + filename
		s3settings := app.Settings().S3
		url := cdnfiles.GetFilePresignedURL(app, s3settings.Bucket, s3Key)
		if url == "" {
			// Fall back to the PocketBase public file URL when S3 is not configured.
			appURL := app.Settings().Meta.AppURL
			url = fmt.Sprintf("%s/api/files/documents/%s/%s", appURL, docID, filename)
		}

		return jsonResult(map[string]any{
			"document_id": docID,
			"filename":    filename,
			"url":         url,
			"expires_in":  "1h",
		})
	}
}
