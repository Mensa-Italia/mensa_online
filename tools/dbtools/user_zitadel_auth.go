package dbtools

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// UpsertUserZitadelAuth aggiorna o crea il mapping (zitadel_sub → users PB)
// nella tabella user_zitadel_auth. Usata sia dal cron Zitadel (popolazione
// bulk dopo il sync) sia dal flusso MCP (popolazione lazy al primo login).
//
// Idempotente. Errori loggati ma non propagati: il chiamante non deve
// fallire per un problema di cache.
func UpsertUserZitadelAuth(app core.App, zitadelSub, userPBID, email string) {
	if zitadelSub == "" || userPBID == "" {
		return
	}
	col, err := app.FindCollectionByNameOrId("user_zitadel_auth")
	if err != nil {
		app.Logger().Warn("[user_zitadel_auth] collection mancante", "err", err)
		return
	}
	existing, _ := app.FindFirstRecordByFilter(col,
		"zitadel_sub = {:s}", dbx.Params{"s": zitadelSub})
	rec := existing
	if rec == nil {
		rec = core.NewRecord(col)
		rec.Set("zitadel_sub", zitadelSub)
	}
	rec.Set("user", userPBID)
	if email != "" {
		rec.Set("email", email)
	}
	if err := app.Save(rec); err != nil {
		app.Logger().Warn("[user_zitadel_auth] save fallito",
			"sub", zitadelSub, "user", userPBID, "err", err)
	}
}
