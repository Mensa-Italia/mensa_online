package exapp

import (
	"net/http"
	"strings"
	"time"

	"mensadb/main/hooks"

	"github.com/pocketbase/pocketbase/core"
)

type exportFetchBody struct {
	RequestID string `json:"request_id" form:"request_id"`
}

// exportFetchHandler: POST /api/cs/exapp/export/fetch (chiave sk_).
//
// L'app esterna ritira i dati dopo che il socio ha approvato. Possono passare:
//
//	404  richiesta inesistente o di un altro ex_app (non si distingue)
//	401  la chiave non ha il permesso dello scope della richiesta
//	409  pending, denied o consumed
//	410  scaduta (anche una approvata non ritirata in tempo)
//	200  approved e valida: passa a consumed e restituisce i dati, UNA volta
func exportFetchHandler(e *core.RequestEvent) error {
	authKey := e.Request.Header.Get("Authorization")
	keyAppID, _ := hooks.GetKeyAppId(e.App, authKey)
	if keyAppID == "" {
		return e.String(http.StatusUnauthorized, "Unauthorized")
	}

	var body exportFetchBody
	if err := e.BindBody(&body); err != nil {
		return exportError(e, &apiError{http.StatusBadRequest, "Body non valido."})
	}
	requestID := strings.TrimSpace(body.RequestID)
	if requestID == "" {
		return exportError(e, &apiError{http.StatusBadRequest, "request_id obbligatorio."})
	}

	req, err := e.App.FindRecordById(exportRequestsCollection, requestID)
	if err != nil || req.GetString("ex_app") != keyAppID {
		return exportError(e, &apiError{http.StatusNotFound, "Richiesta non trovata."})
	}
	scope := req.GetString("scope")
	if !hooks.CheckKey(e.App, authKey, permissionForScope(scope)) {
		return e.String(http.StatusUnauthorized, "Unauthorized")
	}

	now := nowFunc()
	if apiErr := fetchStateError(e.App, req, now); apiErr != nil {
		return exportError(e, apiErr)
	}

	// I dati si leggono PRIMA di consumare: un errore di lettura non brucia il
	// consenso. Chi perde la corsa scarta quello che ha letto.
	userID := req.GetString("user")
	var payload map[string]any
	switch scope {
	case scopeStamps:
		payload, err = buildStampsExport(e.App, userID, now)
	case scopeMedals:
		payload, err = buildMedalsExport(e.App, userID, now)
	}
	if err != nil || payload == nil {
		e.App.Logger().Error("export: lettura dati fallita", "request", requestID, "scope", scope, "err", err)
		return exportError(e, &apiError{http.StatusInternalServerError, "Errore interno."})
	}

	won, err := consumeExportRequest(e.App, requestID, now)
	if err != nil {
		e.App.Logger().Error("export: consumo fallito", "request", requestID, "err", err)
		return exportError(e, &apiError{http.StatusInternalServerError, "Errore interno."})
	}
	if !won {
		// Un'altra chiamata ha consumato la richiesta (o e` scaduta) fra il
		// controllo e l'UPDATE: si ricalcola la risposta dallo stato attuale.
		current, findErr := e.App.FindRecordById(exportRequestsCollection, requestID)
		if findErr != nil {
			return exportError(e, &apiError{http.StatusNotFound, "Richiesta non trovata."})
		}
		if apiErr := fetchStateError(e.App, current, nowFunc()); apiErr != nil {
			return exportError(e, apiErr)
		}
		return exportError(e, &apiError{http.StatusConflict, "Richiesta gia` consumata."})
	}

	e.App.Logger().Info("export: dati ritirati", "request", requestID, "user", userID, "scope", scope, "ex_app", keyAppID)
	return e.JSON(http.StatusOK, payload)
}

// fetchStateError traduce lo stato della richiesta in errore HTTP; nil se
// e` `approved` e non scaduta (cioe` ritirabile). Applica la scadenza
// "pigra": una pending/approved oltre expires_at viene marcata `expired`.
func fetchStateError(app core.App, req *core.Record, now time.Time) *apiError {
	status := req.GetString("status")
	expired := !req.GetDateTime("expires_at").Time().After(now)

	if (status == statusPending || status == statusApproved) && expired {
		_ = markExpired(app, req.Id, now)
		status = statusExpired
	}
	switch status {
	case statusApproved:
		return nil
	case statusExpired:
		return &apiError{http.StatusGone, "Richiesta scaduta."}
	default: // pending, denied, consumed
		return &apiError{http.StatusConflict, "Richiesta non ritirabile (" + status + ")."}
	}
}
