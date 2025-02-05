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

func UpdateDocumentsFromArea32() {
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
	for _, document := range newDocuments {
		go UpdateDocuments(document)
	}

	go notifyAllUsers("Nuovi documenti disponibili!", fmt.Sprintf("Sono stati aggiunti %d nuovi documenti", len(newDocuments)))

}

func UpdateDocuments(document map[string]any) {
	collection, err := app.FindCollectionByNameOrId("documents")
	if err != nil {
		return
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
