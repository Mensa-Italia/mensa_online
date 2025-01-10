package main

import (
	"github.com/pocketbase/pocketbase/core"
)

// Struttura per rappresentare i dati di autenticazione di un utente
type AuthData struct {
	Id      string `json:"id"`       // ID univoco dell'utente
	Email   string `json:"email"`    // Email dell'utente
	IsAdmin bool   `json:"is_admin"` // Indica se l'utente è un amministratore
}

// Funzione per verificare se un utente è autenticato
// Parametri:
// - e: Evento della richiesta HTTP gestito da PocketBase
// Restituisce:
// - bool: Indica se l'utente è autenticato
// - *AuthData: Dati dell'utente autenticato (se presente)
func isLoggedIn(e *core.RequestEvent) (bool, *AuthData) {
	// Recupera le informazioni sulla richiesta corrente
	info, err := e.RequestInfo()
	if err != nil {
		// Se si verifica un errore nel recupero delle informazioni, l'utente non è autenticato
		return false, nil
	}

	// Recupera i dati di autenticazione dal contesto della richiesta
	record := info.Auth
	if record != nil {
		// Se i dati di autenticazione esistono, restituisci true e un'istanza di AuthData
		return true, &AuthData{
			Email:   record.Email(), // Email dell'utente autenticato
			Id:      record.Id,      // ID dell'utente autenticato
			IsAdmin: false,          // Attualmente impostato su false, può essere esteso per verificare il ruolo dell'utente
		}
	}

	// Se non esiste un record di autenticazione, restituisci false e nil
	return false, nil
}
