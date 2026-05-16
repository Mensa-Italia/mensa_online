package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Allinea local_offices_test_dates.assistants a quello che abbiamo gia` su
// local_offices_admins.user e local_offices_test_assistants.user: target
// members_registry invece di users.
//
// Motivo: gli "assistenti" sono persone identificate dall'iscrizione Area32,
// non dall'avere un account app. Tenere il puntatore a users sarebbe
// limitato + inconsistente con le altre tabelle del cluster local_office.
//
// I valori esistenti restano validi: id socio = id users = id members_registry.
//
// Usa swapRelationTarget definito in 1778786000.
func init() {
	m.Register(func(app core.App) error {
		return swapRelationTarget(app, "local_offices_test_dates", "assistants", "members_registry")
	}, func(app core.App) error {
		_ = swapRelationTarget(app, "local_offices_test_dates", "assistants", "users")
		return nil
	})
}
