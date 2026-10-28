package exapp

import (
	"net/http"
	"net/url"
	"strings"

	"mensadb/main/hooks"
	"mensadb/tools/dbtools"

	"github.com/pocketbase/pocketbase/core"
)

// sendPush e` l'invio della push, sostituibile nei test.
var sendPush = func(app core.App, p dbtools.PushNotification) { dbtools.SendPushNotificationToUser(app, p) }

const maxCallmeURLLen = 2048

type exportRequestBody struct {
	MemberID  string `json:"member_id" form:"member_id"`
	Email     string `json:"email" form:"email"`
	CallmeURL string `json:"callme_url" form:"callme_url"`
	Scope     string `json:"scope" form:"scope"`
}

// exportRequestHandler: POST /api/cs/exapp/export/request (chiave sk_).
//
// L'app esterna chiede di esportare i dati di un socio (timbri o medaglie).
// Crea una richiesta `pending` di 10 minuti e manda al socio una push che
// apre il foglio di conferma. Risponde con l'id da usare poi in export/fetch.
func exportRequestHandler(e *core.RequestEvent) error {
	authKey := e.Request.Header.Get("Authorization")
	keyAppID, _ := hooks.GetKeyAppId(e.App, authKey)
	if keyAppID == "" {
		return e.String(http.StatusUnauthorized, "Unauthorized")
	}

	var body exportRequestBody
	if err := e.BindBody(&body); err != nil {
		return exportError(e, &apiError{http.StatusBadRequest, "Body non valido."})
	}
	body.Scope = strings.ToLower(strings.TrimSpace(body.Scope))
	perm := permissionForScope(body.Scope)
	if perm == "" {
		return exportError(e, &apiError{http.StatusBadRequest, "scope deve essere stamps o medals."})
	}
	// Il permesso e` per scope: una chiave EXPORT_STAMPS non chiede le medaglie.
	if !hooks.CheckKey(e.App, authKey, perm) {
		return e.String(http.StatusUnauthorized, "Unauthorized")
	}

	callme := strings.TrimSpace(body.CallmeURL)
	if !validCallmeURL(callme) {
		return exportError(e, &apiError{http.StatusBadRequest, "callme_url deve essere un URL http(s) assoluto."})
	}

	// Come /request: id e email devono combaciare, e la risposta e` la stessa
	// per "utente inesistente" ed "email sbagliata".
	user, err := e.App.FindRecordById("users", strings.TrimSpace(body.MemberID))
	if err != nil || !strings.EqualFold(user.GetString("email"), strings.TrimSpace(body.Email)) {
		return exportError(e, &apiError{http.StatusBadRequest, "Invalid"})
	}

	exApp, err := e.App.FindRecordById("ex_apps", keyAppID)
	if err != nil {
		return e.String(http.StatusUnauthorized, "Unauthorized")
	}

	now := nowFunc()
	req, err := createExportRequest(e.App, keyAppID, user.Id, body.Scope, callme, now)
	if err != nil {
		e.App.Logger().Error("export: creazione richiesta fallita", "user", user.Id, "err", err)
		return exportError(e, &apiError{http.StatusInternalServerError, "Errore interno."})
	}

	sendPush(e.App, dbtools.PushNotification{
		UserId: user.Id,
		TrTag:  "push_notification.confirm_data_export",
		TrNamedParams: map[string]string{
			"name":  exApp.GetString("name"),
			"scope": body.Scope,
		},
		Data: map[string]string{
			"type":      "data_export_confirmation",
			"requestId": req.Id,
			"keyAppId":  keyAppID,
			"scope":     body.Scope,
			"url":       callme,
		},
	})

	return e.JSON(http.StatusOK, map[string]any{
		"request_id": req.Id,
		"expires_in": int(exportTTL.Seconds()),
	})
}

// validCallmeURL: URL assoluto http o https con host.
func validCallmeURL(raw string) bool {
	if raw == "" || len(raw) > maxCallmeURLLen {
		return false
	}
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func exportError(e *core.RequestEvent, err *apiError) error {
	return e.JSON(err.Status, map[string]any{"status": err.Status, "message": err.Message})
}
