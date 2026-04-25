package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		collection := core.NewBaseCollection("processed_webhook_events")
		collection.Fields.Add(&core.TextField{
			Name:     "provider",
			Required: true,
			Max:      32,
		})
		collection.Fields.Add(&core.TextField{
			Name:     "event_id",
			Required: true,
			Max:      255,
		})
		collection.Fields.Add(&core.AutodateField{
			Name:     "created",
			OnCreate: true,
		})
		collection.AddIndex("idx_processed_webhook_unique", true,
			"provider, event_id", "")
		return app.Save(collection)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("processed_webhook_events")
		if err != nil {
			return nil
		}
		return app.Delete(col)
	})
}
