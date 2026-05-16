package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Aggiunge events.local_office come relazione opzionale verso local_offices.
// Popolata in automatico al create dell'evento (hook) e via cron di backfill
// "manual-only" per recuperare lo storico.
func init() {
	m.Register(func(app core.App) error {
		events, err := app.FindCollectionByNameOrId("events")
		if err != nil {
			return err
		}
		offices, err := app.FindCollectionByNameOrId("local_offices")
		if err != nil {
			return err
		}
		events.Fields.Add(&core.RelationField{
			Name:         "local_office",
			Required:     false,
			MaxSelect:    1,
			CollectionId: offices.Id,
		})
		events.AddIndex("idx_events_local_office", false, "local_office", "")
		return app.Save(events)
	}, func(app core.App) error {
		events, err := app.FindCollectionByNameOrId("events")
		if err != nil {
			return nil
		}
		events.Fields.RemoveByName("local_office")
		return app.Save(events)
	})
}
