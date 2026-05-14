package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Apre list e view di quid_issues e quid_articles a chiunque: il contenuto
// e` pubblico sul web di Quid, l'app puo` leggere direttamente da PB senza
// dover passare da WordPress. Le scritture restano interne (solo backend Go
// via quidsync), quindi create/update/delete rules restano nil.
func init() {
	m.Register(func(app core.App) error {
		empty := ""
		for _, name := range []string{"quid_issues", "quid_articles"} {
			col, err := app.FindCollectionByNameOrId(name)
			if err != nil {
				return err
			}
			col.ListRule = &empty
			col.ViewRule = &empty
			if err := app.Save(col); err != nil {
				return err
			}
		}
		return nil
	}, func(app core.App) error {
		for _, name := range []string{"quid_issues", "quid_articles"} {
			col, err := app.FindCollectionByNameOrId(name)
			if err != nil {
				continue
			}
			col.ListRule = nil
			col.ViewRule = nil
			if err := app.Save(col); err != nil {
				return err
			}
		}
		return nil
	})
}
