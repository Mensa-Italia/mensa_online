package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Speculare a local_offices_admins ma per gli Assistenti al Test.
// Popolata dal cron localofficesync che scrappa /gruppi-locali-referenti/.
// list/view aperte ai soli utenti autenticati (allineato al pattern admins).
func init() {
	m.Register(func(app core.App) error {
		offices, err := app.FindCollectionByNameOrId("local_offices")
		if err != nil {
			return err
		}

		col := core.NewBaseCollection("local_offices_test_assistants")
		col.Fields.Add(&core.RelationField{
			Name:          "local_office",
			Required:      true,
			MaxSelect:     1,
			CollectionId:  offices.Id,
			CascadeDelete: true,
		})
		col.Fields.Add(&core.RelationField{
			Name:         "user",
			Required:     true,
			MaxSelect:    1,
			CollectionId: "_pb_users_auth_",
		})
		col.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})
		col.Fields.Add(&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true})

		col.AddIndex("idx_local_offices_test_assistants_unique", true, "local_office,user", "")
		col.AddIndex("idx_local_offices_test_assistants_user", false, "user", "")

		return app.Save(col)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("local_offices_test_assistants")
		if err != nil {
			return nil
		}
		return app.Delete(col)
	})
}
