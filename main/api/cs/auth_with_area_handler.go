package cs

import (
	"errors"
	"mensadb/area32"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// Funzione principale per gestire l'autenticazione di un utente con Area32
func AuthWithAreaHandler(e *core.RequestEvent) error {
	email := strings.ToLower(e.Request.FormValue("email"))
	password := e.Request.FormValue("password")

	scraperApi := area32.NewAPI()
	areaUser, err := scraperApi.DoLoginAndRetrieveMain(email, password)
	if err != nil && errors.Is(err, area32.ErrUnableToConnect) {
		byUser, err := e.App.FindFirstRecordByFilter("users", "email={:email} ", dbx.Params{"email": email})
		if err != nil || byUser == nil || !byUser.ValidatePassword(password) {
			return apis.NewApiError(http.StatusServiceUnavailable, "Unable to connect to area32", err)
		}
		return apis.RecordAuthResponse(e, byUser, "password", nil)
	} else if err != nil {
		return apis.NewBadRequestError("Invalid credentials", err)
	}

	record, err := upsertUserFromAreaUser(e.App, areaUser, email, password)
	if err != nil {
		return apis.NewBadRequestError("Invalid credentials", err)
	}
	return apis.RecordAuthResponse(e, record, "password", nil)
}

// Suggerisce un username unico per un record utente
func suggestUniqueAuthRecordUsername(
	app core.App,
	collectionModelOrIdentifier string,
	baseUsername string,
) string {
	username := baseUsername
	for i := 0; i < 10; i++ {
		total, err := app.CountRecords(
			collectionModelOrIdentifier,
			dbx.NewExp("LOWER([[username]])={:username}", dbx.Params{"username": strings.ToLower(username)}),
		)
		if err == nil && total == 0 {
			break
		}
		username = baseUsername + security.RandomStringWithAlphabet(3+i, "123456789")
	}
	return username
}

// Rimuove un elemento specifico da una slice di stringhe (mantiene ordine non garantito).
func removeFromSlice(slice []string, element string) []string {
	i := -1
	for idx, v := range slice {
		if v == element {
			i = idx
			break
		}
	}
	if i < 0 {
		return slice
	}
	slice[i] = slice[len(slice)-1]
	return slice[:len(slice)-1]
}
