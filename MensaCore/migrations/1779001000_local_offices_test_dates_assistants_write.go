package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Estende le regole di scrittura di local_offices_test_dates anche agli
// assistenti al test del local_office (molti sono autonomi). Prima erano
// solo segretari + co-segretari (admins).
func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("local_offices_test_dates")
		if err != nil {
			return err
		}
		rule := "(@request.auth.id ?= local_office.local_offices_admins_via_local_office.user.id) || " +
			"(@request.auth.id ?= local_office.local_offices_test_assistants_via_local_office.user.id)"
		col.CreateRule = &rule
		col.UpdateRule = &rule
		col.DeleteRule = &rule
		return app.Save(col)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("local_offices_test_dates")
		if err != nil {
			return nil
		}
		rule := "@request.auth.id ?= local_office.local_offices_admins_via_local_office.user.id"
		col.CreateRule = &rule
		col.UpdateRule = &rule
		col.DeleteRule = &rule
		return app.Save(col)
	})
}
