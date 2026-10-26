package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Cambia il target delle relation:
//   - local_offices_admins.user
//   - local_offices_test_assistants.user
// da "users" (auth, contiene solo chi si e` registrato in app) a
// "members_registry" (registro completo dei soci, sync Area32).
//
// Molti segretari/co-segretari/assistenti non hanno mai installato l'app:
// con il vecchio target restavano fuori dalla riconciliazione. Con
// members_registry possiamo linkarli tutti, anche senza utenza app.
//
// In-place: i valori esistenti restano validi perche` i record sono
// chiavati sull'id socio (uid Area32) che coincide tra users e
// members_registry.
//
// Usa swapRelationTarget definito in 1778786000.
func init() {
	m.Register(func(app core.App) error {
		if err := swapRelationTarget(app, "local_offices_admins", "user", "members_registry"); err != nil {
			return err
		}
		return swapRelationTarget(app, "local_offices_test_assistants", "user", "members_registry")
	}, func(app core.App) error {
		_ = swapRelationTarget(app, "local_offices_admins", "user", "users")
		_ = swapRelationTarget(app, "local_offices_test_assistants", "user", "users")
		return nil
	})
}

