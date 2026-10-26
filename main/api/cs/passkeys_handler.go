package cs

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"mensadb/tools/dbtools"
	"mensadb/tools/zauth"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// Limite sul nome della passkey: e` un'etichetta scelta dal client (di norma il
// modello del device) e finisce in una lista, non serve piu` di cosi`.
const passkeyNameMaxLen = 100

const passkeyDefaultName = "Passkey"

// zitadelUserIDForRecord risolve l'utente Zitadel a partire dal record PB
// autenticato — la direzione opposta a quella del middleware.
//
// Prima la cache locale user_zitadel_auth, poi il lookup autoritativo per
// metadato membership_id su Zitadel (che e` come popoliamo quella cache). Al
// successo per la via lenta la cache viene riempita, cosi` la chiamata dopo e`
// gia` veloce.
func zitadelUserIDForRecord(app core.App, record *core.Record) (string, error) {
	if record == nil {
		return "", errors.New("missing authenticated record")
	}

	if mapping, err := app.FindFirstRecordByFilter(
		"user_zitadel_auth", "user = {:u}", dbx.Params{"u": record.Id},
	); err == nil && mapping != nil {
		if sub := mapping.GetString("zitadel_sub"); sub != "" {
			return sub, nil
		}
	}

	zitadelUser, found := zauth.FindUserByMembershipID(record.Id)
	if !found || zitadelUser.GetUserId() == "" {
		return "", errors.New("no zitadel user for this account")
	}

	sub := zitadelUser.GetUserId()
	dbtools.UpsertUserZitadelAuth(app, sub, record.Id, record.Email())
	return sub, nil
}

// authenticatedZitadelUser e` il preambolo comune ai quattro endpoint di
// gestione: richiede un utente autenticato e ne risolve l'id Zitadel.
func authenticatedZitadelUser(e *core.RequestEvent) (string, error) {
	if e.Auth == nil {
		return "", apis.NewUnauthorizedError("not authenticated", nil)
	}
	userID, err := zitadelUserIDForRecord(e.App, e.Auth)
	if err != nil {
		return "", apis.NewApiError(http.StatusConflict, "no_zitadel_identity", nil)
	}
	return userID, nil
}

// PasskeyRegisterBeginHandler avvia la registrazione di una passkey per
// l'utente autenticato.
//
// POST /api/cs/passkeys/begin
//
// Le creation options includono gia` excludeCredentials, popolato da Zitadel con
// le passkey esistenti: e` l'authenticator del device a rifiutare il duplicato,
// e l'errore arriva al client, non a noi.
func PasskeyRegisterBeginHandler(e *core.RequestEvent) error {
	userID, err := authenticatedZitadelUser(e)
	if err != nil {
		return err
	}

	passkeyID, options, err := zauth.BeginPasskeyRegistration(userID)
	if err != nil {
		return apis.NewApiError(http.StatusServiceUnavailable, "Unable to start passkey registration", err)
	}

	return e.JSON(http.StatusOK, map[string]any{
		"passkey_id":                             passkeyID,
		"public_key_credential_creation_options": json.RawMessage(options),
	})
}

// PasskeyRegisterFinishHandler completa la registrazione.
//
// POST /api/cs/passkeys/finish — campi `passkey_id`, `credential`, `name`.
func PasskeyRegisterFinishHandler(e *core.RequestEvent) error {
	userID, err := authenticatedZitadelUser(e)
	if err != nil {
		return err
	}

	passkeyID := strings.TrimSpace(e.Request.FormValue("passkey_id"))
	credential := e.Request.FormValue("credential")
	if passkeyID == "" || credential == "" {
		return apis.NewBadRequestError("passkey_id and credential are required", nil)
	}

	name := strings.TrimSpace(e.Request.FormValue("name"))
	if name == "" {
		name = passkeyDefaultName
	}
	if len(name) > passkeyNameMaxLen {
		name = name[:passkeyNameMaxLen]
	}

	if err := zauth.FinishPasskeyRegistration(userID, passkeyID, name, []byte(credential)); err != nil {
		e.App.Logger().Debug("[passkey] registrazione rifiutata", "err", err)
		return apis.NewApiError(http.StatusBadRequest, "Invalid passkey credential", nil)
	}

	return e.JSON(http.StatusOK, map[string]any{"ok": true})
}

// PasskeyListHandler elenca le passkey dell'utente autenticato.
//
// GET /api/cs/passkeys — usato dalla schermata di gestione e dal gate che decide
// se proporre l'attivazione dopo un login con password.
func PasskeyListHandler(e *core.RequestEvent) error {
	userID, err := authenticatedZitadelUser(e)
	if err != nil {
		return err
	}

	passkeys, err := zauth.ListPasskeys(userID)
	if err != nil {
		return apis.NewApiError(http.StatusServiceUnavailable, "Unable to list passkeys", err)
	}

	return e.JSON(http.StatusOK, map[string]any{"passkeys": passkeys})
}

// PasskeyDeleteHandler revoca una passkey.
//
// DELETE /api/cs/passkeys/{id}
//
// Nessuna cautela particolare sull'ultima passkey rimasta: la password resta
// sempre un percorso valido, quindi non esiste il rischio di chiudersi fuori.
func PasskeyDeleteHandler(e *core.RequestEvent) error {
	userID, err := authenticatedZitadelUser(e)
	if err != nil {
		return err
	}

	passkeyID := e.Request.PathValue("id")
	if strings.TrimSpace(passkeyID) == "" {
		return apis.NewBadRequestError("passkey id is required", nil)
	}

	if err := zauth.RemovePasskey(userID, passkeyID); err != nil {
		return apis.NewApiError(http.StatusServiceUnavailable, "Unable to remove passkey", err)
	}

	return e.JSON(http.StatusOK, map[string]any{"ok": true})
}
