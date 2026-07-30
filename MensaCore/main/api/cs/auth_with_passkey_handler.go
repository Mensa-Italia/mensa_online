package cs

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"mensadb/main/api/zitadelauth"
	"mensadb/tools/zauth"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// passkeyUnavailableMessage e` la risposta unica per "non si puo` entrare con
// una passkey", qualunque sia la ragione.
//
// Utente sconosciuto a Zitadel e utente senza passkey registrate devono essere
// indistinguibili: differenziarli renderebbe questo endpoint un oracolo con cui
// scoprire quali email sono socie e quali hanno gia` una passkey. Il client
// tratta comunque i due casi allo stesso modo — ricade sulla password — quindi
// non perde niente.
const passkeyUnavailableMessage = "passkey_unavailable"

// AuthWithPasskeyBeginHandler apre una sessione Zitadel con una challenge
// WebAuthn pendente e restituisce al client le request options piu` un
// riferimento opaco alla sessione.
//
// POST /api/cs/auth-with-passkey/begin — form-urlencoded, campo `email`.
func AuthWithPasskeyBeginHandler(e *core.RequestEvent) error {
	email := strings.ToLower(strings.TrimSpace(e.Request.FormValue("email")))
	if email == "" {
		return apis.NewBadRequestError("email is required", nil)
	}

	purgeExpiredPasskeyChallenges(e.App)

	sessionID, sessionToken, options, err := zauth.BeginPasskeyLogin(email)
	if err != nil {
		if errors.Is(err, zauth.ErrPasskeyUnavailable) {
			return apis.NewApiError(http.StatusConflict, passkeyUnavailableMessage, nil)
		}
		return apis.NewApiError(http.StatusServiceUnavailable, "Unable to start passkey login", err)
	}

	loginID, err := storePasskeyChallenge(e.App, sessionID, sessionToken)
	if err != nil {
		return apis.NewApiError(http.StatusInternalServerError, "Unable to store passkey challenge", err)
	}

	return e.JSON(http.StatusOK, map[string]any{
		"login_id":                              loginID,
		"public_key_credential_request_options": json.RawMessage(options),
	})
}

// AuthWithPasskeyFinishHandler verifica l'assertion e restituisce i token.
//
// POST /api/cs/auth-with-passkey/finish — form-urlencoded, campi `login_id` e
// `credential` (il PublicKeyCredential JSON prodotto dal device).
//
// La risposta ha volutamente la stessa forma di /auth-with-zitadel, cosi` il
// client riusa lo stesso modello e la stessa logica di adozione della sessione.
//
// A differenza del login con password, qui non si passa per Area32: senza la
// password non si puo` fare lo scraping del profilo. Il record PB viene risolto
// dal sub Zitadel e la sua freschezza e` garantita dal cron che reimporta il
// registro soci piu` volte al giorno.
func AuthWithPasskeyFinishHandler(e *core.RequestEvent) error {
	loginID := strings.TrimSpace(e.Request.FormValue("login_id"))
	credential := e.Request.FormValue("credential")
	if loginID == "" || credential == "" {
		return apis.NewBadRequestError("login_id and credential are required", nil)
	}

	sessionID, sessionToken, err := consumePasskeyChallenge(e.App, loginID)
	if err != nil {
		if errors.Is(err, errPasskeyChallengeExpired) {
			// 410: la challenge era valida ma e` scaduta. Il client puo`
			// ritentare il begin una volta prima di ricadere sulla password.
			return apis.NewApiError(http.StatusGone, "passkey_challenge_expired", nil)
		}
		return apis.NewApiError(http.StatusUnauthorized, "Invalid passkey login reference", nil)
	}

	tokens, err := zauth.FinishPasskeyLogin(sessionID, sessionToken, []byte(credential))
	if err != nil {
		e.App.Logger().Debug("[passkey] assertion rifiutata", "err", err)
		return apis.NewApiError(http.StatusUnauthorized, "Invalid passkey assertion", nil)
	}

	// Il sub e` la fonte autoritativa dell'identita`: lo prendiamo dai claim
	// verificati, non dall'email che il client ci ha passato al begin.
	claims, err := zauth.VerifyAccessToken(e.Request.Context(), tokens.AccessToken)
	if err != nil {
		return apis.NewApiError(http.StatusInternalServerError, "Unable to verify issued token", err)
	}
	claimEmail, _ := claims.Claims["email"].(string)

	record, err := zitadelauth.FindUserByZitadelSub(e.App, claims.Subject, claimEmail)
	if err != nil || record == nil {
		// L'utente ha una passkey valida ma non risolviamo il suo record PB.
		// Rispondiamo come "passkey non disponibile" invece di 500: cosi` il
		// client ricade sulla password, che passa per Area32 e riprovisiona il
		// record — cioe` il caso si auto-ripara invece di bloccare l'utente.
		e.App.Logger().Warn("[passkey] nessun record PB per il sub", "sub", claims.Subject, "err", err)
		return apis.NewApiError(http.StatusConflict, passkeyUnavailableMessage, nil)
	}

	return e.JSON(http.StatusOK, map[string]any{
		"access_token":  tokens.AccessToken,
		"refresh_token": tokens.RefreshToken,
		"id_token":      tokens.IDToken,
		"token_type":    tokens.TokenType,
		"expires_in":    tokens.ExpiresIn,
		"record":        record,
	})
}
