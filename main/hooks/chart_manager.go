package hooks

import (
	"fmt"
	"github.com/pocketbase/pocketbase/core"
	"time"
)

func addToChart(app core.App, key, data, chart string) {
	collection, _ := app.FindCollectionByNameOrId("chart")
	record := core.NewRecord(collection)
	record.Set("key", key)
	record.Set("data", data)
	record.Set("chart", chart)
	_ = app.Save(record)
}

func LogUserChart(e *core.RecordEvent) error {
	nowData := time.Now()
	// key1 composed by user id and year-month-day
	key1 := fmt.Sprintf("%s_%s", e.Record.Id, nowData.Format("2006-01-02"))
	// key2 composed by user id and year-week
	year, week := nowData.ISOWeek()
	key2 := fmt.Sprintf("%s_%d-%d-week", e.Record.Id, year, week)
	// key3 composed by user id and year-month
	key3 := fmt.Sprintf("%s_%d-%d-month", e.Record.Id, nowData.Year(), nowData.Month())
	go addToChart(e.App, key1, nowData.Format("2006-01-02"), "users_login")
	go addToChart(e.App, key2, fmt.Sprintf("%d-%d", year, week), "users_login_week")
	go addToChart(e.App, key3, fmt.Sprintf("%d-%d", nowData.Year(), nowData.Month()), "users_login_month")
	return nil
}
