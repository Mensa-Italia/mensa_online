package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Estende le rule di update/delete sugli eventi: oltre all'owner e ai
// "super", possono modificare/cancellare anche gli admin (segretari +
// co-segretari) del local_office a cui l'evento e` associato. Utile quando
// un segretario vuole correggere o cancellare eventi creati da soci del
// suo gruppo (es. data sbagliata, evento duplicato).
func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("events")
		if err != nil {
			return err
		}
		rule := "(@request.auth.id = owner) || " +
			"(@request.auth.powers:each ?= \"super\") || " +
			"(@request.auth.id ?= local_office.local_offices_admins_via_local_office.user.id)"
		col.UpdateRule = &rule
		col.DeleteRule = &rule
		return app.Save(col)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("events")
		if err != nil {
			return nil
		}
		rule := "(@request.auth.id = owner) || (@request.auth.powers:each ?= \"super\")"
		col.UpdateRule = &rule
		col.DeleteRule = &rule
		return app.Save(col)
	})
}
