package main

import (
	"context"
	"crypto"
	"encoding/hex"
	"github.com/google/uuid"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
	"github.com/pocketbase/pocketbase/tools/security"
	"log"
	"mensadb/area32"
	"mensadb/importers"
	"mensadb/tools/env"
	"slices"
	"strings"
)

// Funzione principale per gestire l'autenticazione di un utente con Area32
func AuthWithAreaHandler(e *core.RequestEvent) error {
	// Recupera le credenziali dal form della richiesta HTTP
	email := e.Request.FormValue("email")
	password := e.Request.FormValue("password")

	// Inizializza l'API Area32 per autenticare l'utente e recuperare i suoi dati principali
	scraperApi := area32.NewAPI()
	areaUser, err := scraperApi.DoLoginAndRetrieveMain(email, password)
	if err != nil {
		// Restituisce un errore se le credenziali non sono valide
		return apis.NewBadRequestError("Invalid credentials", err)
	}

	// Cerca un record utente esistente nel database usando l'ID Area32
	byUser, err := app.FindRecordById("users", areaUser.Id)

	// Se non esiste un record utente, crea un nuovo utente
	if byUser == nil || err != nil {
		// Recupera la collezione "users" dal database
		collection, _ := app.FindCollectionByNameOrId("users")
		newUser := core.NewRecord(collection)

		// Popola il nuovo record con i dati recuperati da Area32
		newUser.Set("id", areaUser.Id)
		newUser.SetEmail(email)
		newUser.Set("username", suggestUniqueAuthRecordUsername("users", strings.Split(email, "@")[0])) // Suggerisce un username unico
		newUser.SetPassword(generatePassword(areaUser.Id))                                              // Genera una password sicura
		newUser.SetVerified(true)                                                                       // Segna l'utente come verificato
		newUser.Set("name", areaUser.Fullname)                                                          // Imposta il nome dell'utente
		newUser.Set("expire_membership", areaUser.ExpireDate)                                           // Data di scadenza della membership
		newUser.Set("is_membership_active", areaUser.IsMembershipActive)                                // Stato attivo della membership

		// Gestione dei permessi (powers) basata sui ruoli di Area32
		powerList := []string{}
		if areaUser.IsATestMaker {
			powerList = append(powerList, "testmakers") // Aggiunge il ruolo di testmaker
		}
		segretari := importers.RetrieveForwardedMail("segretari") // Recupera l'elenco dei segretari
		if slices.Contains(segretari, email) {
			powerList = append(powerList, "events") // Aggiunge il ruolo di gestione eventi
		}
		if len(powerList) > 0 {
			newUser.Set("powers", powerList)
		}

		// Scarica e associa l'immagine dell'utente come avatar
		log.Println(areaUser.ImageUrl)
		fileImage, err := filesystem.NewFileFromURL(context.Background(), areaUser.ImageUrl)
		if err == nil {
			newUser.Set("avatar", fileImage)
		}

		// Salva il nuovo utente nel database
		if err := app.Save(newUser); err != nil {
			log.Println("Invalid credentials on new save", err)
			return apis.NewBadRequestError("Invalid credentials", err)
		}

		// Crea un nuovo record per il link del calendario associato all'utente
		calendarLinkCollection, _ := app.FindCollectionByNameOrId("calendar_link")
		newCalendar := core.NewRecord(calendarLinkCollection)
		newCalendar.Set("user", areaUser.Id)
		newCalendar.Set("hash", randomHash()) // Genera un hash casuale per il calendario
		_ = app.Save(newCalendar)

		// Risponde con i dati di autenticazione dell'utente appena creato
		go func() {
			_, _ = getCustomerId(areaUser.Id)
		}()
		return apis.RecordAuthResponse(e, newUser, "password", nil)
	} else {
		// Se l'utente esiste, aggiorna i suoi dati
		byUser.SetEmail(email)
		byUser.SetVerified(true)
		byUser.Set("name", areaUser.Fullname)
		byUser.Set("expire_membership", areaUser.ExpireDate)
		byUser.Set("is_membership_active", areaUser.IsMembershipActive)

		// Gestisce i permessi esistenti
		powers := byUser.GetStringSlice("powers")
		if areaUser.IsATestMaker && !slices.Contains(powers, "testmakers") {
			powers = append(powers, "testmakers")
			byUser.Set("powers", powers)
		} else if !areaUser.IsATestMaker && slices.Contains(powers, "testmakers") {
			powers = removeFromSlice(powers, "testmakers") // Rimuove il permesso se non è più valido
			byUser.Set("powers", powers)
		}

		// Salva i dati aggiornati nel database
		if err := app.Save(byUser); err != nil {
			log.Println("Invalid credentials on update", err)
			return apis.NewBadRequestError("Invalid credentials", err)
		}

		// Ricarica l'utente dal database per confermare gli aggiornamenti
		byUser, err = app.FindRecordById("users", areaUser.Id)
		if err != nil || !byUser.ValidatePassword(generatePassword(areaUser.Id)) {
			data, _ := byUser.MarshalJSON()
			log.Println("Invalid credentials on reload", string(data))
			return apis.NewBadRequestError("Invalid credentials", err)
		}

		// Risponde con i dati di autenticazione dell'utente esistente
		go func() {
			_, _ = getCustomerId(areaUser.Id)
		}()
		return apis.RecordAuthResponse(e, byUser, "password", nil)
	}
}

// Genera una password sicura basata sull'ID utente e altri parametri
func generatePassword(id string) string {
	pass := crypto.SHA256.New()
	pass.Write([]byte(id + uuid.NewMD5(uuid.MustParse(env.GetPasswordUUID()), []byte(id)).String() + env.GetPasswordSalt()))
	return hex.EncodeToString(pass.Sum(nil))
}

// Rimuove un elemento specifico da una slice di stringhe
func removeFromSlice(slice []string, element string) []string {
	i := slices.Index(slice, element)
	slice[i] = slice[len(slice)-1] // Sostituisce l'elemento con l'ultimo
	return slice[:len(slice)-1]    // Trunca la slice
}

// Suggerisce un username unico per un record utente
func suggestUniqueAuthRecordUsername(
	collectionModelOrIdentifier string,
	baseUsername string,
) string {
	username := baseUsername
	for i := 0; i < 10; i++ { // Prova un massimo di 10 volte
		total, err := app.CountRecords(
			collectionModelOrIdentifier,
			dbx.NewExp("LOWER([[username]])={:username}", dbx.Params{"username": strings.ToLower(username)}),
		)
		if err == nil && total == 0 {
			break // Se unico, esce dal ciclo
		}
		// Genera un username incrementale
		username = baseUsername + security.RandomStringWithAlphabet(3+i, "123456789")
	}

	return username
}
