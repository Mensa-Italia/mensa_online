package main

import (
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"mensadb/area32"
)

func SendUpdateNotifyHandler(e *core.RequestEvent) error {
	// Send update notify to all addons
	email := e.Request.FormValue("email")
	password := e.Request.FormValue("password")

	// Inizializza l'API Area32 per autenticare l'utente e recuperare i suoi dati principali
	scraperApi := area32.NewAPI()
	areaUser, err := scraperApi.DoLoginAndRetrieveMain(email, password)
	if err != nil {
		// Restituisce un errore se le credenziali non sono valide
		return apis.NewBadRequestError("Invalid credentials", err)
	}

	if areaUser.Id == "5366" {
		go notifyAllUsers("Nuovo aggiornamento disponibile!", "C'è un nuovo aggiornamento disponibile! Aggiorna ora per scoprire le nuove funzioni!")

		return e.JSON(200, areaUser)
	}
	return e.String(200, "OK")
}
