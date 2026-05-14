package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Aggiunge il flag is_public agli eventi: true = aperto a chiunque (anche
// senza account), false (default) = solo soci. Allinea listRule e viewRule
// in modo che un evento pubblico sia accessibile da utenti anonimi.
func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("events")
		if err != nil {
			return err
		}

		if col.Fields.GetByName("is_public") == nil {
			col.Fields.Add(&core.BoolField{Name: "is_public"})
		}

		open := "is_public = true || @request.auth.id != \"\" || " +
			"(@collection.ex_keys:keys.key ?= @request.headers.authorization && " +
			"@collection.ex_keys:keys.permissions:each ?= \"GET_EVENTS\")"
		col.ListRule = strPtr(open)
		col.ViewRule = strPtr(open)

		return app.Save(col)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("events")
		if err != nil {
			return nil
		}
		col.Fields.RemoveByName("is_public")
		listRule := "@request.auth.id != \"\" || (@collection.ex_keys:keys.key ?= @request.headers.authorization && @collection.ex_keys:keys.permissions:each ?= \"GET_EVENTS\") || true=true"
		viewRule := "@request.auth.id != \"\" || (@collection.ex_keys:keys.key ?= @request.headers.authorization && @collection.ex_keys:keys.permissions:each ?= \"GET_EVENTS\")"
		col.ListRule = &listRule
		col.ViewRule = &viewRule
		return app.Save(col)
	})
}

func strPtr(s string) *string { return &s }
