package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Aggiunge pdf_url a quid_issues: per i numeri storici (1-12) Quid non ha
// categoria WP ne` articoli, esiste solo come PDF allegato. quidsync scrappa
// /archivio-quid/ e popola questo campo.
func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("quid_issues")
		if err != nil {
			return err
		}
		col.Fields.Add(&core.TextField{Name: "pdf_url", Max: 500})
		return app.Save(col)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("quid_issues")
		if err != nil {
			return nil
		}
		col.Fields.RemoveByName("pdf_url")
		return app.Save(col)
	})
}
