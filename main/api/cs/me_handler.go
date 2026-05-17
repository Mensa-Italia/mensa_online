package cs

import (
	"net/http"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// MeHandler ritorna il record users dell'utente autenticato dal middleware
// (sia esso bearer Zitadel o token PB nativo). Endpoint ergonomico per il
// client che vuole "dammi me stesso" senza dover ricordare l'id PB.
func MeHandler(e *core.RequestEvent) error {
	if e.Auth == nil {
		return apis.NewUnauthorizedError("not authenticated", nil)
	}
	return e.JSON(http.StatusOK, e.Auth)
}
