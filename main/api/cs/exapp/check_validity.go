package exapp

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"mensadb/main/hooks"
	"slices"
)

func checkValidity(e *core.RequestEvent) error {
	authKey := e.Request.Header.Get("Authorization")
	if !hooks.CheckKey(e.App, authKey, "CHECK_USER_EXISTENCE") {
		return e.String(401, "Unauthorized")
	}
	keyAppId, _ := hooks.GetKeyAppId(e.App, authKey)
	userId := e.Request.FormValue("member_id")

	records, err := e.App.FindAllRecords("ex_granted_permissions",
		dbx.NewExp("user = {:user}", dbx.Params{"user": userId}),
		dbx.NewExp("ex_app = {:exapp}", dbx.Params{"exapp": keyAppId}),
	)
	if err != nil || len(records) == 0 {
		return e.String(400, "NOK")
	}

	listOfGrantedPermissions := records[0].GetStringSlice("permissions")
	if !slices.Contains(listOfGrantedPermissions, "CHECK_USER_EXISTENCE") {
		return e.String(200, "OK")
	}
	return e.String(400, "NOK")
}
