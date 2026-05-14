package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Cache locale dei numeri di Quid (categorie WP che matchano "quid-N-...").
// Popolata dal cron quidsync; usata per esporre il numero come risultato
// di global search distinto dai singoli articoli.
func init() {
	m.Register(func(app core.App) error {
		collection := core.NewBaseCollection("quid_issues")

		collection.Fields.Add(&core.TextField{Name: "category_id", Required: true, Max: 32})
		collection.Fields.Add(&core.NumberField{Name: "number", Required: true})
		collection.Fields.Add(&core.TextField{Name: "name", Max: 200})
		collection.Fields.Add(&core.TextField{Name: "slug", Max: 200})
		collection.Fields.Add(&core.NumberField{Name: "articles_count"})
		collection.Fields.Add(&core.TextField{Name: "image", Max: 500})
		collection.Fields.Add(&core.DateField{Name: "published_at"})
		collection.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})
		collection.Fields.Add(&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true})

		collection.AddIndex("idx_quid_issues_category", true, "category_id", "")
		collection.AddIndex("idx_quid_issues_number", true, "number", "")

		return app.Save(collection)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("quid_issues")
		if err != nil {
			return nil
		}
		return app.Delete(col)
	})
}
