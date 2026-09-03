package cs

import (
	"net/http"
	"strings"
	"time"

	"mensadb/tools/cdnfiles"
	"mensadb/tools/env"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/list"
)

// defaultThumbSizes replica la costante omonima non esportata di
// apis/file.go: PocketBase genera "100x100" per ogni campo file anche se non
// e` dichiarata fra le thumbs. Se un giorno cambia la` va cambiata anche qui.
var defaultThumbSizes = []string{"100x100"}

// FileLinkHandler risponde a GET /api/cs/file-link e restituisce un link S3
// firmato per un allegato.
//
// Esiste per i posti in cui un header non si puo` mandare: un PDF aperto in
// Safari o in un visualizzatore di sistema, un file passato a un foglio di
// condivisione, una pagina esterna. Li` l'app non puo` autenticare la
// richiesta del file, ma puo` autenticare QUESTA: manda l'header, e solo se
// l'header viene accettato riceve indietro il link firmato da consegnare a chi
// aprira` il file.
//
// Due controlli, in quest'ordine:
//
//  1. `e.Auth == nil` -> 401. L'identita` la mettono i middleware globali, sia
//     dal bearer OIDC di Zitadel sia da un token PocketBase nativo.
//  2. la view rule della collection, valutata con l'identita` del chiamante.
//     Senza, questo endpoint diventerebbe il modo piu` comodo per leggere gli
//     allegati di record che l'utente non puo` vedere: PocketBase carica il
//     record con FindRecordById, che le regole non le applica.
//
// Il secondo controllo e` piu` stretto di quello che PocketBase applica oggi
// al download diretto, dove la view rule si guarda solo per i campi marcati
// `protected` — cioe`, in questo schema, il solo `deals.attachment`.
//
// Parametri: `collection`, `record`, `file` e l'opzionale `thumb`. La chiave
// S3 la si ricostruisce come fa PocketBase, cosi` il link punta esattamente
// all'oggetto che servirebbe il download diretto.
func FileLinkHandler(e *core.RequestEvent) error {
	if e.Auth == nil {
		return apis.NewUnauthorizedError("not authenticated", nil)
	}

	collectionName := strings.TrimSpace(e.Request.URL.Query().Get("collection"))
	recordID := strings.TrimSpace(e.Request.URL.Query().Get("record"))
	filename := strings.TrimSpace(e.Request.URL.Query().Get("file"))
	if collectionName == "" || recordID == "" || filename == "" {
		return apis.NewBadRequestError("collection, record and file are required", nil)
	}

	collection, err := e.App.FindCachedCollectionByNameOrId(collectionName)
	if err != nil || collection == nil {
		return apis.NewNotFoundError("", nil)
	}

	record, err := e.App.FindRecordById(collection, recordID)
	if err != nil || record == nil {
		return apis.NewNotFoundError("", nil)
	}

	// `filename` arriva dal client e finisce dentro una chiave S3: senza questo
	// controllo un `../` la farebbe uscire dal prefisso del record. Il metodo
	// risolve il nome fra i valori realmente salvati nel record, quindi
	// qualunque cosa non sia un allegato di questo record non passa.
	fileField := record.FindFileFieldByFile(filename)
	if fileField == nil {
		return apis.NewNotFoundError("", nil)
	}

	requestInfo, err := e.RequestInfo()
	if err != nil {
		return apis.NewBadRequestError("failed to load request info", err)
	}
	if ok, _ := e.App.CanAccessRecord(record, requestInfo, record.Collection().ViewRule); !ok {
		// 404 e non 403, come fa PocketBase: dire "esiste ma non puoi" e` gia`
		// dire qualcosa.
		return apis.NewNotFoundError("", nil)
	}

	fileKey, err := resolveFileKey(e.App, record, fileField, filename, e.Request.URL.Query().Get("thumb"))
	if err != nil {
		return apis.NewNotFoundError("", err)
	}

	ttl := env.GetS3PresignTTL()
	url := cdnfiles.GetFilePresignedURLWithTTL(e.App, e.App.Settings().S3.Bucket, fileKey, ttl)
	if url == "" {
		// S3 spento (sviluppo locale) o firma fallita. Non e` un errore per il
		// client: il download diretto continua a funzionare, quindi gli si dice
		// dove andare invece di lasciarlo senza niente.
		return e.JSON(http.StatusOK, map[string]any{
			"url":       e.App.Settings().Meta.AppURL + "/api/files/" + collection.Name + "/" + record.Id + "/" + filename,
			"expiresAt": nil,
			"signed":    false,
		})
	}

	return e.JSON(http.StatusOK, map[string]any{
		"url":       url,
		"expiresAt": time.Now().Add(ttl).UTC().Format(time.RFC3339),
		"signed":    true,
	})
}

// resolveFileKey ricostruisce la chiave S3 dell'oggetto da firmare, con la
// stessa logica di apis/file.go: l'originale sta in
// `<collectionId>/<recordId>/<filename>`, la miniatura in
// `<collectionId>/<recordId>/thumbs_<filename>/<size>_<filename>`.
//
// Sulle miniature si ricade sempre sull'originale quando non si puo` fare di
// meglio — misura non dichiarata sul campo, oppure miniatura non ancora
// generata. PocketBase le genera su richiesta durante il download; qui non si
// serve niente, quindi firmare la chiave di una miniatura inesistente
// darebbe un link che risponde 404 da S3.
func resolveFileKey(app core.App, record *core.Record, fileField *core.FileField, filename, thumb string) (string, error) {
	base := record.BaseFilesPath()
	original := base + "/" + filename

	thumb = strings.TrimSpace(thumb)
	if thumb == "" {
		return original, nil
	}
	if !list.ExistInSlice(thumb, defaultThumbSizes) && !list.ExistInSlice(thumb, fileField.Thumbs) {
		return original, nil
	}

	fsys, err := app.NewFilesystem()
	if err != nil {
		return original, nil
	}
	defer func() {
		_ = fsys.Close()
	}()

	thumbKey := base + "/thumbs_" + filename + "/" + thumb + "_" + filename
	if exists, _ := fsys.Exists(thumbKey); exists {
		return thumbKey, nil
	}

	return original, nil
}
