package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// view_local_office: vista pubblica (no-auth) della collection local_offices.
// La tabella base e` aperta solo agli utenti autenticati; per le pagine
// pubbliche (sito web, condivisioni social, pagine di iscrizione di un
// gruppo locale) serve un endpoint readable senza token. Espone solo i
// campi safe (no metadati interni).
//
// Allineata di stile alle altre view "view_local_office_*" gia` esistenti
// (linktree, test_dates, referenti).
func init() {
	m.Register(func(app core.App) error {
		col := core.NewViewCollection("view_local_office")
		col.ViewQuery = `SELECT
  lo.id AS id,
  lo.name AS name,
  lo.region AS region,
  lo.slug AS slug,
  lo.bio AS bio,
  lo.image AS image,
  lo.created AS created,
  lo.updated AS updated
FROM local_offices lo`

		empty := ""
		col.ListRule = &empty
		col.ViewRule = &empty
		return app.Save(col)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("view_local_office")
		if err != nil {
			return nil
		}
		return app.Delete(col)
	})
}
