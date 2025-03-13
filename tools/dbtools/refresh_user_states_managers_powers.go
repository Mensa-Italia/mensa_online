package dbtools

import (
	"github.com/pocketbase/pocketbase/core"
	"mensadb/importers"
	"slices"
	"sync"
	"time"
)

// Mutex per evitare esecuzioni simultanee della funzione
var lockStateManagers sync.Mutex

// RefreshUserStatesManagersPowers aggiorna i permessi dei gestori di stato degli utenti
func RefreshUserStatesManagersPowers(app core.App) {
	// Tenta di acquisire il lock per evitare esecuzioni parallele
	successLock := lockStateManagers.TryLock()
	if !successLock {
		// Se il lock non è disponibile, significa che il processo è già in esecuzione, quindi esce
		return
	}
	// Assicura il rilascio del lock al termine della funzione
	defer lockStateManagers.Unlock()

	// Log per indicare l'inizio dell'operazione con un ritardo di sicurezza
	app.Logger().Info("Updating states managers permissions, this may take a while. Waiting 1 minute before starting for security reasons.")
	time.Sleep(1 * time.Minute)

	// Recupera tutti i record degli utenti con almeno un valore nel campo 'powers'
	records, err := app.FindRecordsByFilter("users", "powers:length > -1", "-created", -1, 0)
	if err != nil {
		// Se fallisce il recupero dei dati, termina la funzione
		return
	}

	// Recupera la lista delle email degli utenti con poteri di gestione eventi (segretari)
	segretari := importers.RetrieveForwardedMail("segretari")

	// Itera su tutti i record degli utenti
	for _, record := range records {
		// Ottiene la lista attuale dei poteri dell'utente
		powers := record.GetStringSlice("powers")
		newPowers := []string{}
		hadEventsPower := false

		// Verifica se l'utente ha il potere 'events'
		hasEventsPower := slices.Contains(segretari, record.GetString("email"))

		// Filtra i poteri attuali rimuovendo 'events' se presente
		for _, power := range powers {
			if power == "events" {
				hadEventsPower = true
				continue
			}
			newPowers = append(newPowers, power)
		}

		// Se l'utente dovrebbe avere il potere 'events', lo aggiunge
		if hasEventsPower {
			newPowers = append(newPowers, "events")
		}

		// Se lo stato del potere 'events' è cambiato, aggiorna il record
		if hasEventsPower != hadEventsPower {
			record.Set("powers", newPowers)
			_ = app.Save(record) // Salva le modifiche senza gestire esplicitamente l'errore
		}
	}
}
