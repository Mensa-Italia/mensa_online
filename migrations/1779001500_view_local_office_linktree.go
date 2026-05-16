package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// View pubblica che restituisce l'intera "pagina linktree" di un gruppo
// locale in una singola query: profilo (nome, region, slug, bio, image)
// piu` la lista dei link attivi ordinati. Una riga per link.
//
// Lettura no-auth. Il client risolve lo slug (es. "/lombardia"), filtra
// view_local_office_linktree?filter=(slug="lombardia") e renderizza.
func init() {
	m.Register(func(app core.App) error {
		col := core.NewViewCollection("view_local_office_linktree")
		col.ViewQuery = `SELECT
  ll.id AS id,
  lo.id AS local_office,
  lo.name AS local_office_name,
  lo.region AS region,
  lo.slug AS slug,
  lo.bio AS bio,
  lo.image AS image,
  ll.title AS title,
  ll.url AS url,
  ll.icon AS icon,
  ll.sort_order AS sort_order
FROM local_offices_links ll
JOIN local_offices lo ON lo.id = ll.local_office
WHERE ll.active = 1`

		empty := ""
		col.ListRule = &empty
		col.ViewRule = &empty
		return app.Save(col)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("view_local_office_linktree")
		if err != nil {
			return nil
		}
		return app.Delete(col)
	})
}
