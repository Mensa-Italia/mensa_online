package zitadelauth

import (
	"mensadb/tools/dbtools"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

const (
	// Dopo entrambi i loader di auth (Zitadel e quello nativo di PocketBase,
	// DefaultLoadAuthTokenMiddlewarePriority) perche' legge e.Auth; prima del
	// rate limit e del gate di integrita`.
	StaleArea32PasswordPriority = apis.DefaultSecurityHeadersMiddlewarePriority + 1
	StaleArea32PasswordID       = "rejectStaleArea32Password"
)

// RejectStaleArea32Password risponde 401 a ogni richiesta autenticata di un
// socio la cui password e` stata rifiutata da Area32
// (users.area32_password_rejected_at, vedi dbtools.MarkArea32PasswordRejected).
//
// Rifiutare il login non basta: l'app tiene viva la sessione rinnovando il
// token direttamente su Zitadel, che non sa niente di Area32, e il login
// rifatto a ogni avvio, se fallisce, viene ignorato. Il 401 invece l'app lo
// tratta come identita` non piu` valida: rinnova il token, riprova, e al
// secondo 401 su /api/cs/me esce cancellando dati e credenziali salvate.
// Al login successivo la password giusta passa da Area32 e toglie il segno.
//
// I superuser e i record di altre collection passano.
func RejectStaleArea32Password() *hook.Handler[*core.RequestEvent] {
	return &hook.Handler[*core.RequestEvent]{
		Id:       StaleArea32PasswordID,
		Priority: StaleArea32PasswordPriority,
		Func: func(e *core.RequestEvent) error {
			auth := e.Auth
			if auth == nil || auth.IsSuperuser() || auth.Collection().Name != "users" {
				return e.Next()
			}
			if dbtools.Area32PasswordRejected(auth) {
				return apis.NewUnauthorizedError("Area32 credentials changed: sign in again.", nil)
			}
			return e.Next()
		},
	}
}
