package dbtools

import (
	"context"
	"github.com/go-resty/resty/v2"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
	"github.com/tidwall/gjson"
	"sync"
	"time"
)

// Mutex per evitare esecuzioni simultanee della funzione
var lockAddonsData sync.Mutex

// RemoteUpdateAddons aggiorna i dati degli addons memorizzati nel database di PocketBase
func RemoteUpdateAddons(app core.App) {
	// Tenta di acquisire il lock per evitare esecuzioni parallele
	successLock := lockAddonsData.TryLock()
	if !successLock {
		// Se il lock non è disponibile, significa che il processo è già in esecuzione, quindi esce
		return
	}
	// Assicura il rilascio del lock al termine della funzione
	defer lockAddonsData.Unlock()

	// Log per indicare l'inizio dell'operazione con un ritardo di sicurezza
	app.Logger().Info("Updating addons data, this may take a while. Waiting 1 minute before starting for security reasons.")
	time.Sleep(1 * time.Minute)
	app.Logger().Info("Starting to update addons data.")

	// Recupera tutti i record degli addons dal database

	query := app.RecordQuery("addons")
	records := []*core.Record{}
	if err := query.All(&records); err != nil {
		// Se fallisce la query, termina la funzione
		return
	}

	// Itera su tutti i record degli addons
	for _, record := range records {
		// Costruisce l'URL da cui recuperare i dati dell'addon
		urlToCheck := record.Get("url").(string) + "/mensadata.json"

		// Effettua una richiesta HTTP GET per recuperare i dati dell'addon
		get, err := resty.New().R().Get(urlToCheck)
		if err == nil && get.StatusCode() == 200 && get.Body() != nil {
			// Parsea il JSON di risposta
			dataToUse := gjson.ParseBytes(get.Body())

			// Verifica che l'ID dell'addon ricevuto corrisponda a quello memorizzato nel database
			if dataToUse.Get("id").String() == record.GetString("id") {
				// Aggiorna i dati dell'addon con quelli ricevuti
				record.Set("name", dataToUse.Get("name").String())
				record.Set("description", dataToUse.Get("description").String())
				record.Set("version", dataToUse.Get("version").String())

				// Scarica e imposta l'icona dell'addon se disponibile
				fileImage, err := filesystem.NewFileFromURL(context.Background(), dataToUse.Get("icon").String())
				if err == nil {
					record.Set("icon", fileImage)
				}

				// Se tutti i campi essenziali sono stati aggiornati e valorizzati, imposta l'addon come valido, altrimenti come non valido
				if record.GetString("name") != "" && record.GetString("description") != "" && record.GetString("version") != "" && record.GetString("icon") != "" {
					record.Set("is_ready", true)
				} else {
					record.Set("is_ready", false)
				}

				// Salva il record aggiornato nel database
				err = app.Save(record)
			} else {
				// Se l'ID dell'addon non corrisponde, imposta l'addon come non valido
				record.Set("is_ready", false)
				_ = app.Save(record)
			}
		} else {
			// Se la richiesta HTTP fallisce, imposta l'addon come non valido
			record.Set("is_ready", false)
			_ = app.Save(record)
		}
	}
}
