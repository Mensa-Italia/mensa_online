package exapp

import (
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

type exportDecideBody struct {
	RequestID string `json:"request_id" form:"request_id"`
	Accepted  bool   `json:"accepted" form:"accepted"`
}

// exportDecideHandler: POST /api/cs/exapp/export/decide (utente autenticato).
//
// L'utente conferma o nega, dal foglio di conferma dell'app, una richiesta di
// export che lo riguarda. Va consentita anche agli utenti `blocked` (sono soci
// validi per le app esterne): sta nell'allowlist di tools/integrity. Dopo la
// decisione e` l'app a fare la POST di callback al callme_url del partner.
func exportDecideHandler(e *core.RequestEvent) error {
	var body exportDecideBody
	if err := e.BindBody(&body); err != nil {
		return exportError(e, &apiError{http.StatusBadRequest, "Body non valido."})
	}
	body.RequestID = strings.TrimSpace(body.RequestID)
	if body.RequestID == "" {
		return exportError(e, &apiError{http.StatusBadRequest, "request_id obbligatorio."})
	}

	status, apiErr := decideExportRequest(e.App, body.RequestID, e.Auth.Id, body.Accepted, nowFunc())
	if apiErr != nil {
		return exportError(e, apiErr)
	}
	return e.JSON(http.StatusOK, map[string]any{"status": status})
}
