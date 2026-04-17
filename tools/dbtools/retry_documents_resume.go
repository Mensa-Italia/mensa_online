package dbtools

import (
	"log"
	"mensadb/tools/aitools"
	"sort"

	"github.com/pocketbase/pocketbase/core"
)

// RetryMissingDocumentsResume cerca i documenti senza riassunto e tenta di rigenerarlo.
// Non invia notifiche agli utenti. Pensata per essere triggerata manualmente da PocketBase.
func RetryMissingDocumentsResume(app core.App) {
	documents, err := app.FindAllRecords("documents")
	if err != nil {
		log.Println("[RetryResume] Errore nel recupero dei documenti:", err)
		return
	}

	// Ordina dal più recente al più vecchio — i documenti senza riassunto più recenti sono prioritari
	sort.Slice(documents, func(i, j int) bool {
		return documents[i].GetDateTime("created").Time().After(documents[j].GetDateTime("created").Time())
	})

	fsys, err := app.NewFilesystem()
	if err != nil {
		log.Println("[RetryResume] Errore nell'apertura del filesystem:", err)
		return
	}
	defer fsys.Close()

	found := 0
	success := 0
	failed := 0

	for _, doc := range documents {
		// Salta documenti che hanno già un elaborated
		if doc.GetString("elaborated") != "" {
			continue
		}

		found++
		docName := doc.GetString("name")
		docID := doc.Id

		fileField := doc.GetString("file")
		if fileField == "" {
			log.Printf("[RetryResume] Documento %s (%s): nessun file allegato, skip\n", docName, docID)
			failed++
			continue
		}

		key := doc.BaseFilesPath() + "/" + fileField
		file, err := fsys.GetReuploadableFile(key, true)
		if err != nil {
			log.Printf("[RetryResume] Documento %s (%s): errore lettura file: %v\n", docName, docID, err)
			failed++
			continue
		}

		log.Printf("[RetryResume] Documento %s (%s): avvio generazione riassunto...\n", docName, docID)
		resume := aitools.ResumeDocument(app, file)

		if resume == "" {
			log.Printf("[RetryResume] Documento %s (%s): riassunto vuoto (Gemini ha fallito o restituito stringa vuota)\n", docName, docID)
			failed++
			continue
		}

		resumeCollection, err := app.FindCollectionByNameOrId("documents_elaborated")
		if err != nil {
			log.Printf("[RetryResume] Documento %s (%s): collezione documents_elaborated non trovata: %v\n", docName, docID, err)
			failed++
			continue
		}

		newResume := core.NewRecord(resumeCollection)
		newResume.Set("document", docID)
		newResume.Set("ia_resume", resume)
		if err := app.Save(newResume); err != nil {
			log.Printf("[RetryResume] Documento %s (%s): errore salvataggio resume: %v\n", docName, docID, err)
			failed++
			continue
		}

		doc.Set("elaborated", newResume.Id)
		if err := app.Save(doc); err != nil {
			log.Printf("[RetryResume] Documento %s (%s): errore aggiornamento documento: %v\n", docName, docID, err)
			failed++
			continue
		}

		log.Printf("[RetryResume] Documento %s (%s): riassunto generato con successo\n", docName, docID)
		success++
	}

	log.Printf("[RetryResume] Completato — trovati: %d, successi: %d, falliti: %d\n", found, success, failed)
}
