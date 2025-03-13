package cs

import (
	"github.com/pocketbase/pocketbase/core"
	"mensadb/tools/dbtools"
)

func forceNotification(e *core.RequestEvent) error {
	user, _ := e.App.FindRecordById("users", "5366")

	dbtools.SendPushNotificationToUser(e.App, dbtools.PushNotification{
		UserId: user.Id,
		TrTag:  "push_notification.new_document_available",
		TrNamedParams: map[string]string{
			"name": "Delibera CDG 2025.2 Consiglio Vs Gabriel Garofalo",
		},
		Data: map[string]string{
			"type":        "single_document",
			"document_id": "5jsyp5i9cu9837v",
		},
	})
	return e.String(200, "OK")
}
