package main

import (
	"fmt"
	"github.com/google/uuid"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
	"mensadb/area32"
	"mensadb/tools/env"
	"time"
)

func UpdateDocumentsFromArea32(forced ...bool) {
	// Recupera le credenziali dal form della richiesta HTTP
	email := env.GetArea32InternalEmail()
	password := env.GetArea32InternalPassword()

	// Inizializza l'API Area32 per autenticare l'utente e recuperare i suoi dati principali
	scraperApi := area32.NewAPI()
	_, err := scraperApi.DoLoginAndRetrieveMain(email, password)
	if err != nil {
		// Restituisce un errore se le credenziali non sono valide
		return
	}

	id, err := app.FindCollectionByNameOrId("documents")
	if err != nil {
		return
	}
	documentsInside, err := app.FindAllRecords(id)
	if err != nil {
		return
	}
	documentsUids := []string{}
	for _, document := range documentsInside {
		documentsUids = append(documentsUids, document.GetString("uid"))
	}
	newDocuments, _ := scraperApi.GetAllDocuments(documentsUids)
	lastUploadedDocumentId := ""
	for _, document := range newDocuments {
		lastUploadedDocumentId = UpdateDocuments(document)
	}

	if len(forced) > 0 && !forced[0] {
		if len(newDocuments) > 1 {
			go notifyAllUsers("Nuovi documenti disponibili!", fmt.Sprintf("Sono stati aggiunti %d nuovi documenti", len(newDocuments)),
				map[string]string{
					"type": "multiple_documents",
				})
		} else if len(newDocuments) == 1 {
			go notifyAllUsers("Nuovo documento disponibile!", newDocuments[0]["description"].(string),
				map[string]string{
					"type":        "single_document",
					"document_id": lastUploadedDocumentId,
				})
		}
	}

	app.Logger().Info(
		fmt.Sprintf("[CRON] Downloaded %d new documents from Area32", len(newDocuments)),
	)
}

func UpdateDocuments(document map[string]any) string {
	collection, err := app.FindCollectionByNameOrId("documents")
	if err != nil {
		return ""
	}
	uid := uuid.NewMD5(uuid.MustParse(env.GetDocsUUID()), []byte(document["link"].(string))).String()
	newDocument := core.NewRecord(collection)
	newDocument.Set("name", document["description"].(string))
	newDocument.Set("category", []string{getIconBasedOnCategory(document["image"].(string))})
	newDocument.Set("uid", uid)
	if document["date"] != nil {
		newDocument.Set("published", document["date"].(time.Time))
	}
	newDocument.Set("file", document["file"].(*filesystem.File))
	newDocument.Set("uploaded_by", "5366")
	_ = app.Save(newDocument)

	collectionElaborated, _ := app.FindCollectionByNameOrId("documents_elaborated")
	recordElaborated := core.NewRecord(collectionElaborated)
	recordElaborated.Set("document", newDocument.Id)
	recordElaborated.Set("ia_resume", document["resume"].(string))
	_ = app.Save(recordElaborated)

	newDocument.Set("elaborated", recordElaborated.Id)
	_ = app.Save(newDocument)
	return newDocument.Id
}

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
