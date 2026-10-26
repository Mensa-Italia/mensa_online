package cs

import (
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
	"github.com/pocketbase/pocketbase/tools/types"
)

const passkeyChallengeCollection = "passkey_login_challenges"

// Finestra fra il begin e il finish del login con passkey: il tempo di un
// prompt biometrico, non di una sessione. Tenerla corta limita la finestra in
// cui un sessionToken Zitadel resta a riposo nel nostro database.
const passkeyChallengeTTL = 2 * time.Minute

var (
	errPasskeyChallengeUnknown = errors.New("unknown login_id")
	errPasskeyChallengeExpired = errors.New("passkey challenge expired")
)

// storePasskeyChallenge mette da parte la sessione Zitadel appena aperta e
// ritorna il riferimento opaco da dare al client.
//
// Il sessionToken non viaggia mai verso il client: e` un bearer valido verso le
// API Zitadel, quindi resta qui e il client maneggia solo login_id.
func storePasskeyChallenge(app core.App, sessionID, sessionToken string) (string, error) {
	collection, err := app.FindCollectionByNameOrId(passkeyChallengeCollection)
	if err != nil {
		return "", err
	}

	loginID := security.RandomString(32)

	record := core.NewRecord(collection)
	record.Set("login_id", loginID)
	record.Set("zitadel_session_id", sessionID)
	record.Set("zitadel_session_token", sessionToken)
	if err := app.Save(record); err != nil {
		return "", err
	}
	return loginID, nil
}

// consumePasskeyChallenge risolve loginID nella coppia di sessione Zitadel e
// cancella la riga.
//
// La challenge e` monouso: cancellare sempre, anche quando e` scaduta o quando
// il finish fallira` a valle, impedisce che un replay della stessa richiesta
// possa ritentare sulla stessa sessione.
func consumePasskeyChallenge(app core.App, loginID string) (sessionID, sessionToken string, err error) {
	if loginID == "" {
		return "", "", errPasskeyChallengeUnknown
	}

	record, err := app.FindFirstRecordByFilter(
		passkeyChallengeCollection, "login_id = {:l}", dbx.Params{"l": loginID},
	)
	if err != nil || record == nil {
		return "", "", errPasskeyChallengeUnknown
	}

	defer func() {
		if derr := app.Delete(record); derr != nil {
			app.Logger().Warn("[passkey] delete challenge fallita", "err", derr)
		}
	}()

	if time.Since(record.GetDateTime("created").Time()) > passkeyChallengeTTL {
		return "", "", errPasskeyChallengeExpired
	}

	return record.GetString("zitadel_session_id"), record.GetString("zitadel_session_token"), nil
}

// purgeExpiredPasskeyChallenges spazza le righe abbandonate, cioe` gli utenti
// che aprono il prompt biometrico e non lo completano.
//
// Gira inline sul begin invece che in cron: il volume e` minimo e cosi` la
// pulizia non dipende da uno scheduler. Gli errori sono solo loggati, perche`
// un fallimento della pulizia non deve impedire un login.
func purgeExpiredPasskeyChallenges(app core.App) {
	cutoff, err := types.ParseDateTime(time.Now().Add(-passkeyChallengeTTL))
	if err != nil {
		app.Logger().Warn("[passkey] cutoff non valido", "err", err)
		return
	}

	records, err := app.FindAllRecords(
		passkeyChallengeCollection,
		dbx.NewExp("created < {:cutoff}", dbx.Params{"cutoff": cutoff.String()}),
	)
	if err != nil {
		app.Logger().Warn("[passkey] purge challenge: query fallita", "err", err)
		return
	}

	for _, record := range records {
		if err := app.Delete(record); err != nil {
			app.Logger().Warn("[passkey] purge challenge: delete fallita", "id", record.Id, "err", err)
		}
	}
}
