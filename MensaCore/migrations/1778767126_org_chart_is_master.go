package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("org_chart_members")
		if err != nil {
			return err
		}
		col.Fields.Add(&core.BoolField{Name: "is_master"})
		return app.Save(col)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("org_chart_members")
		if err != nil {
			return nil
		}
		col.Fields.RemoveByName("is_master")
		return app.Save(col)
	})
}
