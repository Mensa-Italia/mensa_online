package dbtools

import (
	"fmt"
	"github.com/google/uuid"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
	"mensadb/area32"
	"mensadb/tools/env"
	"time"
)

// RemoteRetrieveDocumentsFromArea32 recupera documenti da Area32 e li aggiorna nel database
func RemoteRetrieveDocumentsFromArea32(app core.App) {
	// Recupera le credenziali dall'ambiente
	email := env.GetArea32InternalEmail()
	password := env.GetArea32InternalPassword()

	// Inizializza l'API Area32 per autenticare l'utente e ottenere i dati principali
	scraperApi := area32.NewAPI()
	_, err := scraperApi.DoLoginAndRetrieveMain(email, password)
	if err != nil {
		// Se l'autenticazione fallisce, termina la funzione
		return
	}

	// Recupera la collezione "documents" dal database
	id, err := app.FindCollectionByNameOrId("documents")
	if err != nil {
		return
	}

	// Recupera tutti i documenti già presenti nel database
	documentsInside, err := app.FindAllRecords(id)
	if err != nil {
		return
	}

	// Costruisce un elenco degli UID dei documenti esistenti
	documentsUids := []string{}
	for _, document := range documentsInside {
		documentsUids = append(documentsUids, document.GetString("uid"))
	}

	// Recupera i nuovi documenti da Area32 che non sono già nel database
	newDocuments, _ := scraperApi.GetAllDocuments(app, documentsUids)
	idOfDocument := ""
	savedCount := 0
	savedIdx := 0
	for i, document := range newDocuments {
		if id := UpdateDocuments(app, document); id != "" {
			idOfDocument = id
			savedIdx = i
			savedCount++
		}
	}

	// Notifica gli utenti solo per i documenti effettivamente salvati nel DB
	if GetInternalConfig(app, "notify_documents_new") == "true" {
		if savedCount > 1 {
			SendPushNotificationToAllUsers(app, PushNotification{
				TrTag: "push_notification.new_documents_available",
				TrNamedParams: map[string]string{
					"count": fmt.Sprintf("%d", savedCount),
				},
				Data: map[string]string{
					"type": "multiple_documents",
				},
			})
		} else if savedCount == 1 {
			SendPushNotificationToAllUsers(app, PushNotification{
				TrTag: "push_notification.new_document_available",
				TrNamedParams: map[string]string{
					"name": newDocuments[savedIdx]["description"].(string),
				},
				Data: map[string]string{
					"type":        "single_document",
					"document_id": idOfDocument,
				},
			})
		}
	}

	// Log dell'operazione completata
	app.Logger().Info(
		fmt.Sprintf("[CRON] Found %d new documents from Area32, saved %d", len(newDocuments), savedCount),
	)
}

// UpdateDocuments aggiorna il database con un nuovo documento recuperato da Area32.
// Ritorna l'ID del record salvato, o "" se il salvataggio fallisce.
func UpdateDocuments(app core.App, document map[string]any) string {
	collection, err := app.FindCollectionByNameOrId("documents")
	if err != nil {
		return ""
	}

	// Genera un UID univoco per il documento basato sul link
	uid := uuid.NewMD5(uuid.MustParse(env.GetDocsUUID()), []byte(document["link"].(string))).String()

	// Evita duplicati: se esiste già un documento con questo UID, non crearne un altro
	existing, _ := app.FindFirstRecordByData("documents", "uid", uid)
	if existing != nil {
		app.Logger().Warn(fmt.Sprintf("[UpdateDocuments] documento con uid %s già presente (id=%s), skip", uid, existing.Id))
		return ""
	}

	// Crea un nuovo record nella collezione "documents"
	newDocument := core.NewRecord(collection)
	newDocument.Set("name", document["description"].(string))
	newDocument.Set("category", []string{getIconBasedOnCategory(document["image"].(string))})
	newDocument.Set("uid", uid)

	// Se disponibile, imposta la data di pubblicazione
	if document["date"] != nil {
		newDocument.Set("published", document["date"].(time.Time))
	}

	// Imposta il file e i relativi dati
	newDocument.Set("file", document["file"].(*filesystem.File))
	newDocument.Set("uploaded_by", "5031")

	// Salva il nuovo documento nel database
	if err := app.Save(newDocument); err != nil {
		app.Logger().Error(fmt.Sprintf("[UpdateDocuments] errore salvataggio documento uid=%s nome=%q: %v", uid, document["description"], err))
		return ""
	}

	if document["resume"] != nil && document["resume"].(string) != "" {
		resumeCollection, _ := app.FindCollectionByNameOrId("documents_elaborated")
		newResume := core.NewRecord(resumeCollection)
		newResume.Set("document", newDocument.Id)
		newResume.Set("ia_resume", document["resume"].(string))
		if err := app.Save(newResume); err != nil {
			app.Logger().Error(fmt.Sprintf("[UpdateDocuments] errore salvataggio resume per documento %s: %v", newDocument.Id, err))
		} else {
			newDocument.Set("elaborated", newResume.Id)
			_ = app.Save(newDocument)
		}
	}

	return newDocument.Id
}

// getIconBasedOnCategory assegna un'icona in base alla categoria del documento
func getIconBasedOnCategory(category string) string {
	switch category {
	case "https://www.cloud32.it/Associazioni2/Documenti/170734/TipoDoc/004.jpg":
		return "bilanci"
	case "https://www.cloud32.it/Associazioni2/Documenti/170734/TipoDoc/011.jpg":
		return "elezioni"
	case "https://www.cloud32.it/Associazioni2/Documenti/170734/TipoDoc/006.jpg":
		return "eventi_progetti"
	case "https://www.cloud32.it/Associazioni2/Documenti/170734/TipoDoc/007.jpg":
		return "materiale_comunicazione"
	case "https://www.cloud32.it/Associazioni2/Documenti/170734/TipoDoc/002.jpg":
		return "modulistica_contratti"
	case "https://www.cloud32.it/Associazioni2/Documenti/170734/TipoDoc/005.jpg":
		return "news_pubblicazioni"
	case "https://www.cloud32.it/Associazioni2/Documenti/170734/TipoDoc/001.jpg":
		return "normativa_interna"
	case "https://www.cloud32.it/Associazioni2/Documenti/170734/TipoDoc/003.jpg":
		return "verbali_delibere"
	case "https://www.cloud32.it/Associazioni2/Documenti/170734/TipoDoc/012.jpg":
		return "tesoreria_contabilita"
	default:
		return "document"
	}
}
