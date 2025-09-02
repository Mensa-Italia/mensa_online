package dbtools

import "github.com/pocketbase/pocketbase/core"

func GetInternalConfig(app core.App, key string) string {
	collection, err := app.FindCollectionByNameOrId("configs")
	if err != nil {
		return ""
	}

	record, err := app.FindFirstRecordByData(collection.Id, "key", key)
	if err != nil || record == nil {
		return ""
	}

	return record.GetString("value")
}
