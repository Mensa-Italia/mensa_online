package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Aggiunge area / state / city alla view view_local_office_assistants.
// Prima esponeva solo region del local_office: utile per capire "dove ha
// sede l'ufficio", ma non "dove opera l'assistente" (in alcuni casi un
// assistente di un gruppo X copre eventi anche fuori regione).
//
// PB non lascia editare una view in-place quando cambiano le colonne:
// drop + recreate. La down ripristina la versione senza geo.
func init() {
	withGeo := `SELECT
  lota.id AS id,
  lo.id AS local_office,
  lo.name AS local_office_name,
  lo.region AS region,
  mr.id AS user,
  mr.name AS name,
  mr.image AS image,
  mr.alias_mail AS email,
  mr.area AS area,
  mr.state AS state,
  mr.city AS city
FROM local_offices_test_assistants lota
JOIN local_offices lo ON lo.id = lota.local_office
JOIN members_registry mr ON mr.id = lota.user
WHERE mr.is_active = 1`

	withoutGeo := `SELECT
  lota.id AS id,
  lo.id AS local_office,
  lo.name AS local_office_name,
  lo.region AS region,
  mr.id AS user,
  mr.name AS name,
  mr.image AS image,
  mr.alias_mail AS email
FROM local_offices_test_assistants lota
JOIN local_offices lo ON lo.id = lota.local_office
JOIN members_registry mr ON mr.id = lota.user
WHERE mr.is_active = 1`

	recreate := func(app core.App, query string) error {
		if col, err := app.FindCollectionByNameOrId("view_local_office_assistants"); err == nil {
			if err := app.Delete(col); err != nil {
				return err
			}
		}
		empty := ""
		col := core.NewViewCollection("view_local_office_assistants")
		col.ViewQuery = query
		col.ListRule = &empty
		col.ViewRule = &empty
		return app.Save(col)
	}

	m.Register(func(app core.App) error {
		return recreate(app, withGeo)
	}, func(app core.App) error {
		return recreate(app, withoutGeo)
	})
}
