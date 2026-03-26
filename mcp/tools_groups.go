package mcp

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/pocketbase/pocketbase/core"
)

// groupItem is the representation of a row from the sigs table.
type groupItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	GroupType   string `json:"group_type"`
	Link        string `json:"link,omitempty"`
	ImageURL    string `json:"image_url,omitempty"`
}

func registerGroupTools(s *server.MCPServer, app core.App) {
	s.AddTool(
		mcp.NewTool("list_groups",
			mcp.WithDescription(
				"Returns the full list of Mensa groups. The table includes several "+
					"kinds of groups that have been added over time: special interest "+
					"groups (SIG), local groups, Facebook groups, WhatsApp chats, "+
					"Telegram chats and generic chats. Use the group_type filter to "+
					"narrow down by kind. Each entry includes name, description, "+
					"external link and a direct URL to the group image.",
			),
			mcp.WithString("group_type",
				mcp.Description(
					"Optional filter. Allowed values: sig, sig_facebook, local, "+
						"chat_whatsapp, chat_telegram, chat",
				),
			),
		),
		makeListGroupsTool(app),
	)
}

func makeListGroupsTool(app core.App) server.ToolHandlerFunc {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		groupType := req.GetString("group_type", "")

		filter := "id != \"\""
		params := map[string]any{}
		if groupType != "" {
			filter = "group_type = {:gt}"
			params["gt"] = groupType
		}

		records, err := app.FindRecordsByFilter(
			"sigs", filter, "name", 0, 0, params,
		)
		if err != nil {
			return nil, fmt.Errorf("query groups: %w", err)
		}

		appURL := app.Settings().Meta.AppURL
		items := make([]groupItem, len(records))
		for i, r := range records {
			item := groupItem{
				ID:          r.Id,
				Name:        r.GetString("name"),
				Description: r.GetString("description"),
				GroupType:   r.GetString("group_type"),
				Link:        r.GetString("link"),
			}
			if img := r.GetString("image"); img != "" {
				item.ImageURL = fmt.Sprintf("%s/api/files/sigs/%s/%s", appURL, r.Id, img)
			}
			items[i] = item
		}

		return jsonResult(map[string]any{
			"total":  len(items),
			"groups": items,
		})
	}
}
