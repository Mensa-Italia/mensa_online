package dbtools

import (
	"fmt"

	"github.com/pocketbase/pocketbase/core"
)

// SetInternalConfig esegue un upsert sulla collection `configs` per la chiave indicata.
// Usata per stato applicativo persistente (cursori cron, flag operativi, ecc.).
func SetInternalConfig(app core.App, key, value string) error {
	collection, err := app.FindCollectionByNameOrId("configs")
	if err != nil {
		return fmt.Errorf("find configs collection: %w", err)
	}

	record, err := app.FindFirstRecordByData(collection.Id, "key", key)
	if err != nil || record == nil {
		record = core.NewRecord(collection)
		record.Set("key", key)
	}
	record.Set("value", value)
	if err := app.Save(record); err != nil {
		return fmt.Errorf("save config %q: %w", key, err)
	}
	return nil
}
