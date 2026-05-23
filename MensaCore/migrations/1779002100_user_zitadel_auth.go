package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Tabella di mapping persistente Zitadel sub → users PB. Popolata lazy dal
// pacchetto mcp al primo MCP-call di un nuovo utente, dopo che la risoluzione
// (email da JWT o da /oidc/v1/userinfo) ha trovato l'users record.
//
// Letture/scritture solo dal backend: nessuna rule pubblica.
func init() {
	m.Register(func(app core.App) error {
		users, err := app.FindCollectionByNameOrId("users")
		if err != nil {
			return err
		}
		col := core.NewBaseCollection("user_zitadel_auth")

		col.Fields.Add(&core.RelationField{
			Name:          "user",
			Required:      true,
			MaxSelect:     1,
			CollectionId:  users.Id,
			CascadeDelete: true,
		})
		col.Fields.Add(&core.TextField{Name: "zitadel_sub", Required: true, Max: 100})
		col.Fields.Add(&core.TextField{Name: "email", Max: 320})
		col.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})
		col.Fields.Add(&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true})

		col.AddIndex("idx_user_zitadel_auth_sub", true, "zitadel_sub", "")
		col.AddIndex("idx_user_zitadel_auth_user", true, "user", "")

		return app.Save(col)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("user_zitadel_auth")
		if err != nil {
			return nil
		}
		return app.Delete(col)
	})
}
