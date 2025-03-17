package exapp

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"mensadb/main/hooks"
	"mensadb/tools/dbtools"
	"strings"
)

func externalAppRequireConfirmation(e *core.RequestEvent) error {
	authKey := e.Request.Header.Get("Authorization")
	if !hooks.CheckKey(e.App, authKey, "CHECK_USER_EXISTENCE") {
		return e.String(401, "Unauthorized")
	}
	keyAppId, _ := hooks.GetKeyAppId(e.App, authKey)
	userId := e.Request.FormValue("member_id")
	userEmail := e.Request.FormValue("email")
	callmeURL := e.Request.FormValue("callme_url")

	exApp, _ := e.App.FindRecordById("ex_apps", keyAppId)

	user, err := e.App.FindRecordById("users", userId)
	if err != nil {
		return apis.NewBadRequestError("Invalid", err)
	}

	if strings.ToLower(user.GetString("email")) != strings.ToLower(userEmail) {
		return apis.NewBadRequestError("Invalid", nil)
	}

	exGrantedCollection, _ := e.App.FindCollectionByNameOrId("ex_granted_permissions")
	records, _ := e.App.FindAllRecords(exGrantedCollection,
		dbx.NewExp("user = {:user}", dbx.Params{"user": userId}),
		dbx.NewExp("ex_app = {:exapp}", dbx.Params{"exapp": keyAppId}),
	)
	if len(records) == 1 {
		_ = e.App.Delete(records[0])
	}

	dbtools.SendPushNotificationToUser(e.App, dbtools.PushNotification{
		UserId: user.Id,
		TrTag:  "push_notification.confirm_external_resource",
		TrNamedParams: map[string]string{
			"name": exApp.GetString("name"),
		},
		Data: map[string]string{
			"type":     "account_confirmation",
			"keyAppId": keyAppId,
			"url":      callmeURL,
		},
	})
	return e.String(200, "OK")
}
