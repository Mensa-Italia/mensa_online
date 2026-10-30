package migrations

import (
	"mensadb/tools/leaderboardnotify"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Stato delle notifiche push della classifica (tools/leaderboardnotify):
// per ogni soggetto di una classifica l'ultima posizione vista e la data
// dell'ultimo invio per ogni soglia, piu' quella dell'ultimo messaggio
// quindicinale. Solo superuser. Idempotente: si crea solo se manca, e se
// manca si crea anche all'avvio del pacchetto.
func init() {
	m.Register(func(app core.App) error {
		return leaderboardnotify.EnsureStateCollection(app)
	}, func(app core.App) error {
		existing, err := app.FindCollectionByNameOrId(leaderboardnotify.StateTable)
		if err != nil {
			return nil
		}
		return app.Delete(existing)
	})
}
