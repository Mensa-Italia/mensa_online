package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Aggiorna view_local_office_linktree per includere parent + kind, cosi`
// il client puo` ricostruire l'albero (sezioni / sottosezioni / link).
// Niente filtro su kind: anche le sezioni "vuote" (senza link figli attivi)
// vengono esposte, sara` il client a decidere se ometterle.
func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("view_local_office_linktree")
		if err != nil {
			return err
		}
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
  ll.kind AS kind,
  ll.parent AS parent,
  ll.sort_order AS sort_order
FROM local_offices_links ll
JOIN local_offices lo ON lo.id = ll.local_office
WHERE ll.active = 1`
		return app.Save(col)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("view_local_office_linktree")
		if err != nil {
			return nil
		}
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
		return app.Save(col)
	})
}
