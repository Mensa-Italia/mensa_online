package dbtools

import (
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

// Area32PasswordRejectedField e` users.area32_password_rejected_at: quando
// Area32 ha rifiutato una password che Zitadel accettava ancora. Vedi la
// migrazione 1794100003_area32_password_rejected.
const Area32PasswordRejectedField = "area32_password_rejected_at"

// MarkArea32PasswordRejected segna il socio con questa email: Area32 ha
// rifiutato la sua password. Da qui ogni sua richiesta autenticata riceve 401
// finche' un login non riesce su Area32 (che lo azzera).
//
// Passa da app.Save, cioe` da PocketBase: mai scrivere data.db da fuori.
func MarkArea32PasswordRejected(app core.App, email string) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return
	}
	rec, err := app.FindFirstRecordByFilter("users", "email={:email}", dbx.Params{"email": email})
	if err != nil || rec == nil {
		return
	}
	rec.Set(Area32PasswordRejectedField, types.NowDateTime())
	if err := app.Save(rec); err != nil {
		app.Logger().Error("area32: segno di password rifiutata non salvato", "user", rec.Id, "err", err)
	}
}

// ClearArea32PasswordRejected toglie il segno dal record (da salvare a cura
// di chi chiama): Area32 ha appena accettato la password.
func ClearArea32PasswordRejected(rec *core.Record) {
	rec.Set(Area32PasswordRejectedField, "")
}

// Area32PasswordRejected dice se il socio e` segnato.
func Area32PasswordRejected(rec *core.Record) bool {
	return rec != nil && !rec.GetDateTime(Area32PasswordRejectedField).IsZero()
}
