package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Cache locale degli articoli pubblicati su quid.mensa.it (WordPress).
// Popolata dal cron quidsync; serve solo a fornire l'idratazione per la
// global search (Bleve). L'app continua a parlare direttamente con WP.
// Nessuna regola di accesso: la collection e` interna, scrivibile solo dal
// backend Go.
func init() {
	m.Register(func(app core.App) error {
		collection := core.NewBaseCollection("quid_articles")

		collection.Fields.Add(&core.TextField{Name: "wp_id", Required: true, Max: 32})
		collection.Fields.Add(&core.TextField{Name: "title", Max: 500})
		collection.Fields.Add(&core.TextField{Name: "excerpt", Max: 5000})
		collection.Fields.Add(&core.TextField{Name: "body", Max: 65535})
		collection.Fields.Add(&core.TextField{Name: "link", Max: 500})
		collection.Fields.Add(&core.TextField{Name: "image", Max: 500})
		collection.Fields.Add(&core.TextField{Name: "category_id", Max: 32})
		collection.Fields.Add(&core.TextField{Name: "category_name", Max: 200})
		collection.Fields.Add(&core.DateField{Name: "published_at"})
		collection.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})
		collection.Fields.Add(&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true})

		collection.AddIndex("idx_quid_articles_wp_id", true, "wp_id", "")
		collection.AddIndex("idx_quid_articles_category", false, "category_id", "")

		return app.Save(collection)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("quid_articles")
		if err != nil {
			return nil
		}
		return app.Delete(col)
	})
}
