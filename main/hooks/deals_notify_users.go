package hooks

import (
	"github.com/pocketbase/pocketbase/core"
	"mensadb/tools/dbtools"
)

func DealsNotifyUsersAsync(e *core.RecordEvent) error {
	if dbtools.GetInternalConfig(e.App, "notify_deals_new") != "true" {
		return e.Next()
	}
	go func(e *core.RecordEvent) {
		dealsNotifyUsers(e, "push_notification.new_deal")
	}(e)

	return e.Next()
}

func DealsUpdateNotifyUsersAsync(e *core.RecordEvent) error {
	if dbtools.GetInternalConfig(e.App, "notify_deals_update") != "true" {
		return e.Next()
	}
	go func(e *core.RecordEvent) {
		dealsNotifyUsers(e, "push_notification.update_deal")
	}(e)
	return e.Next()
}

func dealsNotifyUsers(e *core.RecordEvent, trTags string) {
	dbtools.SendPushNotificationToAllUsers(e.App, dbtools.PushNotification{
		TrTag: trTags,
		TrNamedParams: map[string]string{
			"name": e.Record.GetString("name"),
		},
		Data: map[string]string{
			"type":    "deal",
			"deal_id": e.Record.Id,
		},
	})
}
