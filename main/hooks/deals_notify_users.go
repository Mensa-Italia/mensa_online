package hooks

import (
	"github.com/pocketbase/pocketbase/core"
	"mensadb/tools/dbtools"
)

func DealsNotifyUsersAsync(e *core.RecordEvent) error {
	go func(e *core.RecordEvent) {
		dealsNotifyUsers(e)
	}(e)

	return e.Next()
}

func dealsNotifyUsers(e *core.RecordEvent) {
	dbtools.SendPushNotificationToAllUsers(e.App, dbtools.PushNotification{
		TrTag: "push_notification.new_deal",
		TrNamedParams: map[string]string{
			"name": e.Record.GetString("name"),
		},
		Data: map[string]string{
			"type":    "deal",
			"deal_id": e.Record.Id,
		},
	}, false)
}
