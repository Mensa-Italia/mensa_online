package integrity

import (
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"

	tools "mensadb/tools/integrity"
)

const (
	// MiddlewareID / MiddlewarePriority: il gate gira DOPO i due loader di
	// auth (Zitadel a -1021, quello nativo di PocketBase a -1020), che sono
	// quelli che popolano e.Auth, e subito DOPO il rate limiter (-1000), cosi`
	// chi martella con prove false viene limitato prima di costarci una
	// chiamata a Google.
	MiddlewareID       = "integrityGate"
	MiddlewarePriority = apis.DefaultRateLimitMiddlewarePriority + 1

	// requestVerifiedKey segna sul RequestEvent che la richiesta ha portato una
	// prova valida (serve all'hook realtime).
	requestVerifiedKey = "integrityProofVerified"
	// clientVerifiedKey segna sul client realtime che ha sottoscritto con una
	// prova valida.
	clientVerifiedKey = "integrityProofVerified"
)

// Middleware ritorna il middleware globale di enforcement. Per ogni richiesta
// autenticata legge users.access_level (dal record caricato dai middleware di
// auth, quindi fresco di DB) e applica Gate.Authorize:
//
//   - free: passa senza altro lavoro;
//   - blocked: 403 account_blocked, su qualsiasi path tranne l'allowlist minima
//     del flusso delle app esterne (tools.blockedAllowlist);
//   - watched: prova per-richiesta su tutto /api (tranne le esenzioni), /mcp
//     rifiutato.
//
// I superuser (dashboard, backup, script) e le richieste senza auth passano:
// per le seconde il livello non e` noto e valgono le rule di sempre.
//
// Essendo un middleware GLOBALE sul router, vede ogni route /api, comprese
// quelle native di PocketBase (/api/collections/..., /api/batch, il refresh
// dell'auth, /api/realtime). Le sotto-richieste di /api/batch NON passano da
// qui, ma vivono dentro la richiesta /api/batch, che e` gia` stata verificata.
func Middleware(gate *tools.Gate) *hook.Handler[*core.RequestEvent] {
	return &hook.Handler[*core.RequestEvent]{
		Id:       MiddlewareID,
		Priority: MiddlewarePriority,
		Func: func(e *core.RequestEvent) error {
			if e.Auth == nil || e.Auth.IsSuperuser() {
				return e.Next()
			}
			level := tools.LevelOf(e.Auth)
			if level == tools.LevelFree {
				return e.Next()
			}

			rej := gate.Authorize(e.Request.Context(),
				tools.Subject{UserID: e.Auth.Id, Level: level},
				tools.Request{Method: e.Request.Method, Path: e.Request.URL.Path, Query: e.Request.URL.Query(), Header: e.Request.Header},
			)
			if rej != nil {
				return reject(e, rej)
			}
			if level == tools.LevelWatched && tools.RequiresProof(e.Request.Method, e.Request.URL.Path) {
				e.Set(requestVerifiedKey, true)
			}
			return e.Next()
		},
	}
}

// reject logga il motivo preciso e risponde con il JSON del contratto.
func reject(e *core.RequestEvent, rej *tools.Rejection) error {
	userID := ""
	if e.Auth != nil {
		userID = e.Auth.Id
	}
	e.App.Logger().Warn("integrity: richiesta rifiutata",
		"user", userID,
		"method", e.Request.Method,
		"path", e.Request.URL.Path,
		"code", rej.Code,
		"reason", rej.Reason,
	)
	return e.JSON(rej.Status, rej.Body())
}

// BindRealtime aggancia gli hook realtime che completano il gate.
//
// La sottoscrizione (POST /api/realtime) e` gia` protetta dal middleware. Gli
// eventi invece partono molto dopo, e il livello dell'utente puo` essere
// cambiato nel frattempo (PocketBase tiene aggiornato il record di auth dei
// client connessi): qui si chiude quel buco. Un client il cui utente e`
// "blocked", o "watched" senza una sottoscrizione fatta con prova valida,
// non riceve piu` eventi finche' non risottoscrive.
func BindRealtime(app core.App) {
	app.OnRealtimeSubscribeRequest().BindFunc(func(e *core.RealtimeSubscribeRequestEvent) error {
		verified, _ := e.RequestEvent.Get(requestVerifiedKey).(bool)
		e.Client.Set(clientVerifiedKey, verified)
		return e.Next()
	})

	app.OnRealtimeMessageSend().BindFunc(func(e *core.RealtimeMessageEvent) error {
		auth, _ := e.Client.Get(apis.RealtimeClientAuthKey).(*core.Record)
		if auth == nil || auth.IsSuperuser() {
			return e.Next()
		}
		switch tools.LevelOf(auth) {
		case tools.LevelBlocked:
			return nil
		case tools.LevelWatched:
			if verified, _ := e.Client.Get(clientVerifiedKey).(bool); !verified {
				return nil
			}
		}
		return e.Next()
	})
}
