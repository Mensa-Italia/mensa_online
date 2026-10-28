package mcp

import (
	"encoding/json"
	"net/http"

	"github.com/pocketbase/pocketbase/core"

	"mensadb/tools/integrity"
)

// newAccessGuard applica i livelli di accesso (users.access_level) alle
// richieste MCP. Va DOPO newAuthMiddleware: l'identita` qui arriva dal JWT
// Zitadel (claims nel context) e non da e.Auth, quindi il middleware globale
// di integrita` di PocketBase non e` una garanzia sufficiente per /mcp.
//
//   - blocked: 403 account_blocked, come su /api.
//   - watched: 403 integrity_failed. Un client MCP non e` l'app e non puo`
//     produrre una prova Play Integrity / App Attest, quindi per un utente
//     sotto osservazione non esiste un modo valido di usare /mcp. Non si
//     risponde 428 perche` suggerirebbe di riprovare con una prova.
//
// Se l'utente non si risolve (nessun account PB) la richiesta passa: i tool
// che servono un utente falliscono da soli con "MCP user non risolvibile", e
// chi non ha un account non ha un livello. I tool collection fanno comunque
// proxy via loopback su /api con il token dell'utente, e li' il middleware
// globale applica di nuovo i livelli.
func newAccessGuard(app core.App, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := ClaimsFromContext(r.Context())
		if ok {
			bearer, _ := BearerFromContext(r.Context())
			if user, err := resolveUserFromClaimsCtx(r.Context(), app, claims, bearer); err == nil && user != nil {
				var rej *integrity.Rejection
				switch integrity.LevelOf(user) {
				case integrity.LevelBlocked:
					rej = integrity.Blocked("utente con access_level=blocked")
				case integrity.LevelWatched:
					rej = integrity.Failed("/mcp non disponibile per utenti watched (nessuna prova possibile)")
				}
				if rej != nil {
					app.Logger().Warn("integrity: richiesta MCP rifiutata",
						"user", user.Id, "path", r.URL.Path, "code", rej.Code, "reason", rej.Reason)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(rej.Status)
					_ = json.NewEncoder(w).Encode(rej.Body())
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}
