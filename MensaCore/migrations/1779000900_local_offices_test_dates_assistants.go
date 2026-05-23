package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Aggiunge a local_offices_test_dates il campo `assistants`: lista di
// utenti (relation multi-select a _pb_users_auth_) che fanno da assistenti
// per la sessione di test. Tipicamente vanno scelti tra i record
// local_offices_test_assistants dello stesso local_office; questo controllo
// e` lasciato al client (segretari/co-segretari sono comunque gli unici a
// poter modificare la riga, vedi 1779000800).
func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("local_offices_test_dates")
		if err != nil {
			return err
		}
		col.Fields.Add(&core.RelationField{
			Name:         "assistants",
			Required:     false,
			MinSelect:    0,
			MaxSelect:    32, // hard cap di sicurezza, nessuna sessione ha 32 assistenti
			CollectionId: "_pb_users_auth_",
		})
		col.AddIndex("idx_local_offices_test_dates_assistants", false, "assistants", "")
		return app.Save(col)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("local_offices_test_dates")
		if err != nil {
			return nil
		}
		col.Fields.RemoveByName("assistants")
		return app.Save(col)
	})
}
