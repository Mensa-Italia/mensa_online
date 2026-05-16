package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Collection dei link "linktree" per ogni gruppo locale: titolo, URL,
// icona opzionale, ordine, attivo/disattivo.
//
// Permessi:
//   - lettura: pubblica (la pagina linktree deve essere consultabile senza auth)
//   - scrittura: stessa regola di local_offices_test_dates → admin del gruppo
//     (segretari + co-segretari) PLUS assistenti al test del gruppo. Tutti
//     possono aiutare a curare la pagina.
func init() {
	m.Register(func(app core.App) error {
		offices, err := app.FindCollectionByNameOrId("local_offices")
		if err != nil {
			return err
		}
		col := core.NewBaseCollection("local_offices_links")

		col.Fields.Add(&core.RelationField{
			Name:          "local_office",
			Required:      true,
			MaxSelect:     1,
			CollectionId:  offices.Id,
			CascadeDelete: true,
		})
		col.Fields.Add(&core.TextField{Name: "title", Required: true, Max: 100})
		col.Fields.Add(&core.URLField{Name: "url", Required: true})
		// icon: stringa libera (emoji "✨", nome icona "instagram", ecc.)
		col.Fields.Add(&core.TextField{Name: "icon", Max: 60})
		col.Fields.Add(&core.NumberField{Name: "sort_order"})
		col.Fields.Add(&core.BoolField{Name: "active"})
		col.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})
		col.Fields.Add(&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true})

		readRule := ""
		writeRule := "(@request.auth.id ?= local_office.local_offices_admins_via_local_office.user.id) || " +
			"(@request.auth.id ?= local_office.local_offices_test_assistants_via_local_office.user.id)"
		col.ListRule = &readRule
		col.ViewRule = &readRule
		col.CreateRule = &writeRule
		col.UpdateRule = &writeRule
		col.DeleteRule = &writeRule

		col.AddIndex("idx_local_offices_links_office", false, "local_office", "")
		col.AddIndex("idx_local_offices_links_order", false, "local_office, sort_order", "")

		return app.Save(col)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("local_offices_links")
		if err != nil {
			return nil
		}
		return app.Delete(col)
	})
}
