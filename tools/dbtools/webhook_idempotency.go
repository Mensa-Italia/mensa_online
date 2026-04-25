package dbtools

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const processedWebhookCollection = "processed_webhook_events"

// MarkWebhookEventProcessed ritorna true se l'evento e' NUOVO (e lo marca come processato),
// false se era gia' stato processato. In caso di errore DB, ritorna true (fail-open) e logga
// — preferiamo processare 2 volte un evento piuttosto che perderlo.
func MarkWebhookEventProcessed(app core.App, provider, eventID string) bool {
	if eventID == "" {
		return true
	}
	existing, _ := app.FindFirstRecordByFilter(
		processedWebhookCollection,
		"provider = {:p} && event_id = {:e}",
		dbx.Params{"p": provider, "e": eventID},
	)
	if existing != nil {
		return false
	}
	collection, err := app.FindCollectionByNameOrId(processedWebhookCollection)
	if err != nil {
		app.Logger().Error("webhook idempotency: collection not found", "err", err)
		return true
	}
	rec := core.NewRecord(collection)
	rec.Set("provider", provider)
	rec.Set("event_id", eventID)
	if err := app.Save(rec); err != nil {
		app.Logger().Error("webhook idempotency: save failed", "err", err)
		return true
	}
	return true
}
