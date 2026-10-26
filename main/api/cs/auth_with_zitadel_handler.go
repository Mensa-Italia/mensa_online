package cs

import (
	"errors"
	"log"
	"mensadb/area32"
	"mensadb/tools/zauth"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// AuthWithZitadelHandler autentica email/password contro Zitadel (Session API v2 + OIDC token exchange)
// e restituisce i token Zitadel (access + refresh + id) al client. Mantiene PocketBase users sincronizzato
// usando Area32 come fonte profilo, con la stessa logica di AuthWithAreaHandler.
//
// Motivo: PocketBase ammette una sola sessione attiva per utente; delegando a Zitadel il client
// puo' tenere N sessioni in parallelo (es. mobile + web).
func AuthWithZitadelHandler(e *core.RequestEvent) error {
	email := strings.ToLower(e.Request.FormValue("email"))
	password := e.Request.FormValue("password")
	if email == "" || password == "" {
		return apis.NewBadRequestError("email and password are required", nil)
	}

	scraperApi := area32.NewAPI()
	var areaUser *area32.Area32User

	tokens, zerr := zauth.LoginWithPassword(email, password)

	// Tre casi vanno tutti riparati nello stesso modo: l'utente non esiste su
	// Zitadel, esiste ma con password diversa, oppure esiste senza alcuna
	// credenziale password (utenti creati dal bulk import: Zitadel risponde
	// FailedPrecondition COMMAND-3nJ4t). In tutti e tre Area32 e` la fonte di
	// verita`: ci autentichiamo li`, poi allineiamo Zitadel e ritentiamo.
	if errors.Is(zerr, zauth.ErrUserNotFound) ||
		errors.Is(zerr, zauth.ErrInvalidPassword) ||
		errors.Is(zerr, zauth.ErrPasswordNotSet) {

		var aerr error
		areaUser, aerr = scraperApi.DoLoginAndRetrieveMain(email, password)
		if aerr != nil {
			if errors.Is(aerr, area32.ErrUnableToConnect) {
				return apis.NewApiError(http.StatusServiceUnavailable, "Unable to connect to area32", aerr)
			}
			return apis.NewApiError(http.StatusUnauthorized, "Invalid credentials", aerr)
		}

		metadata := map[string]string{"membership_id": areaUser.Id}

		// CreateUser fa upsert e ritorna l'id Zitadel in entrambi i rami.
		// Usiamo quell'id diretto invece di risalire dai metadata: la ricerca
		// per membership_id passa dalle proiezioni (eventually consistent) e
		// non trova gli utenti importati senza quel metadato.
		zitadelUserID := zauth.CreateUser(areaUser.Fullname, email, email, metadata)
		if zitadelUserID == "" {
			if existingID, ok := zauth.UserExists(email); ok {
				zitadelUserID = existingID
			}
		}
		if zitadelUserID == "" {
			log.Println("zitadel provisioning failed for", email)
			return apis.NewApiError(http.StatusInternalServerError, "Zitadel sync failed", nil)
		}

		// Ripara i vecchi utenti importati senza membership_id, cosi` le
		// lookup per metadata (e SetUserPassword) tornano a funzionare.
		if err := zauth.SetUserMetadataValues(zitadelUserID, metadata); err != nil {
			log.Println("zitadel metadata sync failed:", err)
		}

		if err := zauth.SetUserPasswordByUserID(zitadelUserID, password); err != nil {
			log.Println("zitadel password sync failed:", err)
			return apis.NewApiError(http.StatusInternalServerError, "Zitadel password sync failed", err)
		}

		tokens, zerr = zauth.LoginWithPassword(email, password)
		if zerr != nil {
			log.Println("zitadel login still failing after provisioning:", zerr)
			return apis.NewApiError(http.StatusInternalServerError, "Zitadel sync failed", zerr)
		}
	} else if zerr != nil {
		log.Println("zitadel login error:", zerr)
		return apis.NewApiError(http.StatusServiceUnavailable, "Unable to authenticate against zitadel", zerr)
	}

	if areaUser == nil {
		fresh, aerr := scraperApi.DoLoginAndRetrieveMain(email, password)
		if aerr == nil {
			areaUser = fresh
		} else {
			log.Println("area32 sync skipped:", aerr)
		}
	}

	var record *core.Record
	if areaUser != nil {
		r, err := upsertUserFromAreaUser(e.App, areaUser, email, password)
		if err != nil {
			log.Println("PB upsert failed:", err)
		} else {
			record = r
		}
	}
	if record == nil {
		if existing, ferr := e.App.FindFirstRecordByFilter("users", "email={:email}", dbx.Params{"email": email}); ferr == nil {
			record = existing
		}
	}

	resp := map[string]any{
		"access_token":  tokens.AccessToken,
		"refresh_token": tokens.RefreshToken,
		"id_token":      tokens.IDToken,
		"token_type":    tokens.TokenType,
		"expires_in":    tokens.ExpiresIn,
	}
	if record != nil {
		resp["record"] = record
	}
	return e.JSON(http.StatusOK, resp)
}
