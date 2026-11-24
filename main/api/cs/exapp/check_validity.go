package exapp

import (
	"database/sql"
	"errors"
	"mensadb/main/hooks"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

func checkValidity(e *core.RequestEvent) error {
	authKey := e.Request.Header.Get("Authorization")
	if !hooks.CheckKey(e.App, authKey, "CHECK_USER_EXISTENCE") {
		return e.String(401, "Unauthorized")
	}
	keyAppId, err := hooks.GetKeyAppId(e.App, authKey)
	if err != nil {
		return e.String(401, "Unauthorized")
	}

	// 400: richiesta malformata (parametro mancante o vuoto)
	userId := strings.TrimSpace(e.Request.FormValue("member_id"))
	if userId == "" {
		return e.String(400, "Missing member_id")
	}

	records, err := e.App.FindAllRecords("ex_granted_permissions",
		dbx.NewExp("user = {:user}", dbx.Params{"user": userId}),
		dbx.NewExp("ex_app = {:exapp}", dbx.Params{"exapp": keyAppId}),
	)
	if err != nil {
		return e.String(500, "Internal Server Error")
	}
	// Nessun permesso concesso per questo utente: rispondiamo 404
	// per non rivelare all'app esterna se l'utente esiste o meno.
	if len(records) == 0 {
		return e.String(404, "Not Found")
	}

	listOfGrantedPermissions := records[0].GetStringSlice("permissions")
	if !slices.Contains(listOfGrantedPermissions, "CHECK_USER_EXISTENCE") {
		return e.String(403, "Forbidden")
	}

	user, err := e.App.FindRecordById("users", userId)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return e.String(404, "Not Found")
		}
		return e.String(500, "Internal Server Error")
	}

	// Membership non attiva o scaduta: il membro non risulta valido
	if !user.GetBool("is_membership_active") {
		return e.String(404, "Not Found")
	}
	if user.GetDateTime("expire_membership").Before(types.NowDateTime()) {
		return e.String(404, "Not Found")
	}

	return e.JSON(200, map[string]interface{}{
		"username":             user.GetString("username"),
		"name":                 user.GetString("name"),
		"is_membership_active": user.GetBool("is_membership_active"),
	})
}
