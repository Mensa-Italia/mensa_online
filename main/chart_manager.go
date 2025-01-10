package main

import (
	"fmt"
	"github.com/pocketbase/pocketbase/core"
	"time"
)

func addToChart(key, data, chart string) {
	collection, _ := app.FindCollectionByNameOrId("chart")
	record := core.NewRecord(collection)
	record.Set("key", key)
	record.Set("data", data)
	record.Set("chart", chart)
	_ = app.Save(record)
}

func LogUserChart(e *core.RecordEvent) error {
	nowData := time.Now()
	key := fmt.Sprintf("%s_%s", e.Record.Id, nowData.Format("2006-01-02"))
	go addToChart(key, nowData.Format("2006-01-02"), "users_login")
	return nil
}
