package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Storage a vita breve per le challenge di login con passkey.
//
// Esiste per una ragione precisa: fra il begin e il finish del login serve
// ricordare la sessione Zitadel appena aperta, ma il suo sessionToken e` un
// bearer verso le API Zitadel e non puo` essere consegnato al client. Il client
// riceve quindi solo login_id, un riferimento opaco, e la coppia
// (session_id, session_token) resta qui.
//
// Le righe vivono pochi secondi — il tempo del prompt biometrico — e vengono
// spazzate dal handler di begin. Letture/scritture solo dal backend: nessuna
// rule pubblica.
func init() {
	m.Register(func(app core.App) error {
		col := core.NewBaseCollection("passkey_login_challenges")

		col.Fields.Add(&core.TextField{Name: "login_id", Required: true, Max: 100})
		col.Fields.Add(&core.TextField{Name: "zitadel_session_id", Required: true, Max: 100})
		col.Fields.Add(&core.TextField{Name: "zitadel_session_token", Required: true, Max: 2000})
		col.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})

		col.AddIndex("idx_passkey_login_challenges_login_id", true, "login_id", "")
		// Non unico: serve solo a rendere la spazzata per scadenza una range scan.
		col.AddIndex("idx_passkey_login_challenges_created", false, "created", "")

		return app.Save(col)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("passkey_login_challenges")
		if err != nil {
			return nil
		}
		return app.Delete(col)
	})
}
