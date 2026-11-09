package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// users.area32_password_rejected_at (data, nascosto): quando Area32 ha
// rifiutato una password che Zitadel invece accettava ancora.
//
// Succede a chi cambia la password direttamente sull'area soci (o viene
// tolto da li'): Zitadel conserva la vecchia, e prima il login la accettava
// lo stesso, saltando Area32. Finche' il campo e` valorizzato ogni richiesta
// autenticata di quel socio riceve 401 (vedi zitadelauth.RejectStaleArea32Password),
// cosi` l'app esce e chiede di nuovo la password. Lo azzera il primo login
// riuscito su Area32 (upsertUserFromAreaUser).
//
// Nascosto per lo stesso motivo di access_level: lo scrive solo il server, e
// un campo hidden non esce nelle risposte API ne' si puo` impostare via API
// da un non superuser. Idempotente.

const area32PasswordRejectedField = "area32_password_rejected_at"

func init() {
	m.Register(upArea32PasswordRejected, downArea32PasswordRejected)
}

func upArea32PasswordRejected(app core.App) error {
	users, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		return err
	}
	if users.Fields.GetByName(area32PasswordRejectedField) != nil {
		return nil
	}
	users.Fields.Add(&core.DateField{
		Name:   area32PasswordRejectedField,
		Hidden: true,
	})
	return app.Save(users)
}

func downArea32PasswordRejected(app core.App) error {
	users, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		return nil
	}
	if users.Fields.GetByName(area32PasswordRejectedField) == nil || readByAView(app, area32PasswordRejectedField) {
		return nil
	}
	users.Fields.RemoveByName(area32PasswordRejectedField)
	return app.Save(users)
}
