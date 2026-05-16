package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// View pubblica che aggrega referenti dei gruppi locali (segretari +
// co-segretari + assistenti al test) in una singola tabella read-only.
// Evita di esporre rule complicate sulle tabelle interne e da` al client
// un endpoint unico /api/collections/view_local_office_referenti/records
// senza autenticazione.
//
// Una riga per (local_office, user, ruolo). Sorgenti:
//   - local_offices_admins -> segretario/cosegretario (is_the_officer)
//   - local_offices_test_assistants -> assistente
//
// I dati anagrafici (nome, immagine, email @mensa.it) vengono da
// members_registry quando disponibile.
func init() {
	m.Register(func(app core.App) error {
		col := core.NewViewCollection("view_local_office_referenti")
		col.ViewQuery = `SELECT
  loa.id AS id,
  lo.id AS local_office,
  lo.name AS local_office_name,
  lo.region AS region,
  u.id AS user,
  mr.name AS name,
  mr.image AS image,
  mr.alias_mail AS email,
  CASE WHEN loa.is_the_officer THEN 'segretario' ELSE 'cosegretario' END AS role
FROM local_offices_admins loa
JOIN local_offices lo ON lo.id = loa.local_office
JOIN users u ON u.id = loa.user
LEFT JOIN members_registry mr ON mr.id = u.id
WHERE mr.is_active IS NULL OR mr.is_active = 1

UNION ALL

SELECT
  lota.id AS id,
  lo.id AS local_office,
  lo.name AS local_office_name,
  lo.region AS region,
  u.id AS user,
  mr.name AS name,
  mr.image AS image,
  mr.alias_mail AS email,
  'assistente' AS role
FROM local_offices_test_assistants lota
JOIN local_offices lo ON lo.id = lota.local_office
JOIN users u ON u.id = lota.user
LEFT JOIN members_registry mr ON mr.id = u.id
WHERE mr.is_active IS NULL OR mr.is_active = 1`

		empty := ""
		col.ListRule = &empty
		col.ViewRule = &empty
		return app.Save(col)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("view_local_office_referenti")
		if err != nil {
			return nil
		}
		return app.Delete(col)
	})
}
