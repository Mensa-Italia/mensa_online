package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// View pubblica che espone le sessioni di test programmate dai gruppi
// locali, arricchite con i metadati del gruppo (nome, regione). Read-only,
// senza autenticazione: le date dei test sono informazione pubblica e
// servono al sito/app per indirizzare i candidati alla sessione piu` vicina.
//
// La colonna `assistants` resta una relazione multi-select verso users:
// il client puo` fare ?expand=assistants per ottenere i nomi.
func init() {
	m.Register(func(app core.App) error {
		col := core.NewViewCollection("view_local_office_test_dates")
		col.ViewQuery = `SELECT
  lotd.id AS id,
  lotd.local_office AS local_office,
  lo.name AS local_office_name,
  lo.region AS region,
  lotd.date AS date,
  lotd.location AS location,
  lotd.notes AS notes,
  lotd.max_participants AS max_participants,
  lotd.assistants AS assistants,
  lotd.created AS created,
  lotd.updated AS updated
FROM local_offices_test_dates lotd
JOIN local_offices lo ON lo.id = lotd.local_office`

		empty := ""
		col.ListRule = &empty
		col.ViewRule = &empty
		return app.Save(col)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("view_local_office_test_dates")
		if err != nil {
			return nil
		}
		return app.Delete(col)
	})
}
