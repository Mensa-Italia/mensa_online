package dbtools

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// ResolveUserLocalOffice ritorna l'id del local_office a cui l'utente e`
// associato (segretario / co-segretario / assistente test), oppure "" se
// l'utente non e` linkato a nessun gruppo locale.
//
// Priorita`: admins (segretari/co-segretari) > test assistants. Se l'utente
// e` linkato a piu` di un local_office (caso raro: piu` cariche), torna il
// primo trovato — i casi reali sono mono-office.
func ResolveUserLocalOffice(app core.App, userID string) string {
	if userID == "" {
		return ""
	}
	if rec, err := app.FindFirstRecordByFilter("local_offices_admins",
		"user = {:u}", dbx.Params{"u": userID},
	); err == nil && rec != nil {
		return rec.GetString("local_office")
	}
	if rec, err := app.FindFirstRecordByFilter("local_offices_test_assistants",
		"user = {:u}", dbx.Params{"u": userID},
	); err == nil && rec != nil {
		return rec.GetString("local_office")
	}
	return ""
}
