package migrations

import (
	"strings"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Livelli di accesso e chiavi App Attest.
//
//   - users.access_level (select: free | watched | blocked, nascosto):
//     "watched" obbliga ogni richiesta autenticata sotto /api a portare una
//     prova di integrita` dell'app (Play Integrity / App Attest), "blocked"
//     respinge ogni richiesta autenticata. Vuoto = free. E` `hidden` perche`
//     lo devono vedere e cambiare solo i superuser dalla dashboard: un campo
//     nascosto non esce nelle risposte API e, per i non superuser, viene
//     scartato anche dal body di create/update, quindi nemmeno un utente con
//     powers=super puo` toccarlo via API.
//   - app_attest_keys: le chiavi App Attest registrate dai dispositivi iOS,
//     una per (utente, dispositivo). Senza regole API = solo superuser, e il
//     server le legge e scrive via codice (tools/integrity).
//
// La migrazione e` idempotente: ogni pezzo si crea solo se manca, cosi` un DB
// dove il campo o la collection sono stati aggiunti a mano non si rompe.

const (
	accessLevelField   = "access_level"
	appAttestKeysTable = "app_attest_keys"
)

func init() {
	m.Register(upAccessLevelAndAppAttest, downAccessLevelAndAppAttest)
}

func upAccessLevelAndAppAttest(app core.App) error {
	users, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		return err
	}
	if users.Fields.GetByName(accessLevelField) == nil {
		users.Fields.Add(&core.SelectField{
			Name:      accessLevelField,
			Values:    []string{"free", "watched", "blocked"},
			MaxSelect: 1,
			Hidden:    true,
		})
		if err := app.Save(users); err != nil {
			return err
		}
	}

	if existing, _ := app.FindCollectionByNameOrId(appAttestKeysTable); existing != nil {
		return nil
	}

	keys := core.NewBaseCollection(appAttestKeysTable)
	// Nessuna regola API (nil = solo superuser): la collection contiene le
	// chiavi pubbliche e i contatori su cui si basa la verifica.
	keys.Fields.Add(
		&core.RelationField{
			Name:          "user",
			CollectionId:  users.Id,
			MaxSelect:     1,
			Required:      true,
			CascadeDelete: true,
		},
		// base64 standard del keyId (SHA-256 della chiave pubblica).
		&core.TextField{Name: "key_id", Required: true, Max: 128},
		// Chiave pubblica della foglia dell'attestazione: DER PKIX in base64.
		&core.TextField{Name: "public_key", Required: true, Max: 512},
		// Ultimo counter visto in un'asserzione: deve solo crescere.
		&core.NumberField{Name: "sign_counter", OnlyInt: true},
		&core.SelectField{
			Name:      "environment",
			Values:    []string{"production", "development"},
			MaxSelect: 1,
			Required:  true,
		},
		&core.AutodateField{Name: "created", OnCreate: true},
		&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
	)
	keys.AddIndex("idx_app_attest_keys_key_id", true, "key_id", "")
	keys.AddIndex("idx_app_attest_keys_user", false, "user", "")
	return app.Save(keys)
}

func downAccessLevelAndAppAttest(app core.App) error {
	if keys, _ := app.FindCollectionByNameOrId(appAttestKeysTable); keys != nil {
		if err := app.Delete(keys); err != nil {
			return err
		}
	}

	users, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		return nil // niente collection users: niente campo da togliere
	}
	if users.Fields.GetByName(accessLevelField) != nil && !readByAView(app, accessLevelField) {
		users.Fields.RemoveByName(accessLevelField)
		return app.Save(users)
	}
	return nil
}

// readByAView dice se una view legge la colonna: SQLite non lascia togliere
// una colonna che una view usa (view_leaderboard esclude gli utenti
// "blocked" leggendo users.access_level), e dal 30/10/2026 quella view sta
// nello schema di produzione, quindi la down lascia il campo dov'e`.
func readByAView(app core.App, column string) bool {
	views, err := app.FindAllCollections(core.CollectionTypeView)
	if err != nil {
		return true // nel dubbio non si tocca
	}
	for _, v := range views {
		if strings.Contains(v.ViewQuery, column) {
			return true
		}
	}
	return false
}
