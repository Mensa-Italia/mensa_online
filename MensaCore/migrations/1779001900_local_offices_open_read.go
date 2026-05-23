package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Apre list/view di local_offices a qualsiasi socio autenticato.
// Prima erano ristrette ai soli admin del singolo gruppo: scelta troppo
// chiusa una volta che la collection alimenta pagine pubbliche dell'app
// (linktree, eventi, sigs territoriali, ecc.).
func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("local_offices")
		if err != nil {
			return err
		}
		rule := "@request.auth.id != \"\""
		col.ListRule = &rule
		col.ViewRule = &rule
		return app.Save(col)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("local_offices")
		if err != nil {
			return nil
		}
		listRule := "@request.auth.id ?= local_offices_admins_via_local_office.user.id"
		empty := ""
		col.ListRule = &listRule
		col.ViewRule = &empty
		return app.Save(col)
	})
}
