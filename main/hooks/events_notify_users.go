package hooks

import (
	"bytes"
	"fmt"
	"github.com/google/uuid"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
	"github.com/pocketbase/pocketbase/tools/mailer"
	"github.com/yeqown/go-qrcode/v2"
	"github.com/yeqown/go-qrcode/writer/standard"
	"io"
	"log"
	"mensadb/tools/aipower"
	"mensadb/tools/dbtools"
	"net/mail"
)

// nopCloser è una struttura che implementa l'interfaccia io.Writer
// e fornisce un metodo Close() che non fa nulla, utile per scrivere
// dati in un buffer senza dover gestire la chiusura esplicita.
type nopCloser struct {
	io.Writer
}

func (nopCloser) Close() error { return nil }

// EventsNotifyUsersAsync avvia due operazioni asincrone:
// 1. Notifica gli utenti riguardo all'evento.
// 2. Crea e salva un timbro per l'evento.
// Restituisce il controllo immediatamente al chiamante.
func EventsNotifyUsersAsync(e *core.RecordEvent) error {

	if dbtools.GetInternalConfig(e.App, "notify_events_new") != "true" {
		return e.Next()
	}

	go func(e *core.RecordEvent) {
		eventsNotifyUsers(e)
	}(e)

	if !e.Record.GetBool("is_spot") {
		go func(e *core.RecordEvent) {
			createEventStamp(e.App, e.Record)
		}(e)
	}

	return e.Next()
}

// EventsUpdateNotifyUsersAsync:
// Notifica gli utenti riguardo all'evento aggiornato.
func EventsUpdateNotifyUsersAsync(e *core.RecordEvent) error {
	if dbtools.GetInternalConfig(e.App, "notify_events_update") != "true" {
		return e.Next()
	}
	go func(e *core.RecordEvent) {
		EventsUpdateNotifyUsers(e)
	}(e)
	return e.Next()
}

// createEventStamp gestisce il processo di creazione di un timbro per l'evento.
// 1. Genera un'immagine del timbro utilizzando il nome dell'evento.
// 2. Salva il record del timbro nel database.
// 3. Genera un QR code associato al timbro e al suo codice segreto.
// 4. Invia un'email all'utente con il timbro allegato.
func createEventStamp(app core.App, record *core.Record) []byte {

	userRecord, err := dbtools.GetUserById(app, record.GetString("owner"))
	if err != nil {
		return nil
	}

	stampCollection, _ := app.FindCollectionByNameOrId("stamp")
	newRecord := core.NewRecord(stampCollection)

	// Generazione dell'immagine del timbro
	geminiImage, err := aipower.GenerateStamp(record.GetString("name")+"\n"+record.GetString("description"), record.GetBool("is_national"))
	if err != nil {
		// Log dell'errore nella generazione dello stamp
		log.Printf("Errore nella generazione dello stamp: %v", err)
		return nil
	}
	fileImage, err := filesystem.NewFileFromBytes(geminiImage, "stamp.png")
	if err != nil {
		log.Printf("Errore nella creazione del file immagine: %v", err)
		return nil
	}
	newRecord.Set("image", fileImage)
	newRecord.Set("description", record.GetString("name"))

	// Salvataggio del record del timbro
	err = app.Save(newRecord)
	if err != nil {
		log.Printf("Errore nel salvataggio del record del timbro: %v", err)
		return nil
	}

	stampSecretCollection, _ := app.FindCollectionByNameOrId("stamp_secret")
	newRecordSecret := core.NewRecord(stampSecretCollection)
	newRecordSecret.Set("stamp", newRecord.Id)
	newRecordSecret.Set("code", uuid.New().String())
	err = app.Save(newRecordSecret)
	if err != nil {
		log.Printf("Errore nel salvataggio del record del timbro segreto: %v", err)
		return nil
	}

	// Generazione del QR code
	qrc, err := qrcode.New(fmt.Sprintf("%s:::%s", newRecord.Id, newRecordSecret.GetString("code")))
	if err != nil {
		// Log dell'errore nella generazione del QRCode
		log.Printf("Errore nella generazione del QRCode: %v", err)
		return nil
	}
	options := []standard.ImageOption{
		standard.WithBgColorRGBHex("#ffffff"),
		standard.WithFgColorRGBHex("#000000"),
	}

	// Creazione dell'immagine del timbro da inviare via email
	stampImage := bytes.NewBuffer(nil)
	wr := nopCloser{Writer: stampImage}
	w2 := standard.NewWithWriter(wr, options...)
	if err = qrc.Save(w2); err != nil {
		log.Printf("Errore nel salvataggio del QRCode: %v", err)
		return nil
	}

	key := record.BaseFilesPath() + "/" + record.GetString("image")

	fsys, _ := app.NewFilesystem()
	defer fsys.Close()

	blob, _ := fsys.GetReader(key)
	defer blob.Close()

	// Preparazione del messaggio email da inviare
	message := &mailer.Message{
		From: mail.Address{
			Address: app.Settings().Meta.SenderAddress,
			Name:    app.Settings().Meta.SenderName,
		},
		To: []mail.Address{{
			Address: userRecord.Email(),
		}},
		Subject: "Ciao creatore di eventi!", Attachments: map[string]io.Reader{
			"stamp_qr.png":  stampImage,
			"stamp.png":     bytes.NewReader(geminiImage),
			"copertina.png": blob,
		},
		HTML: fmt.Sprintf(`<p>Ciao creatore di eventi!</p><br><p>Trovi allegato il tuo timbro personale per l'evento %s e la copertina!</p>`, record.GetString("name")),
		Text: fmt.Sprintf("Ciao creatore di eventi!\n\nTrovi allegato il tuo timbro personale per l'evento %s e la copertina! ", record.GetString("name")),
	}

	// Invio dell'email con il timbro allegato
	err = app.NewMailClient().Send(message)
	if err != nil {
		log.Printf("Errore nell'invio dell'email: %v", err)
		return nil
	}

	log.Printf("Timbro creato e email inviata con successo per l'evento: %s", record.GetString("name"))
	return geminiImage

}

// eventsNotifyUsers gestisce l'invio di notifiche push agli utenti.
// Se l'evento è nazionale, invia una notifica a tutti gli utenti.
// Se l'evento è locale, recupera gli utenti in base alla posizione
// dell'evento e invia notifiche individuali.
func eventsNotifyUsers(e *core.RecordEvent) {
	// Controllo se l'evento è nazionale
	if e.Record.Get("is_national") == true {
		dbtools.SendPushNotificationToAllUsers(e.App, dbtools.PushNotification{
			TrTag: "push_notification.new_national_event",
			TrNamedParams: map[string]string{
				"name": e.Record.GetString("name"),
			},
			Data: map[string]string{
				"type":     "event",
				"event_id": e.Record.GetString("id"),
			},
		})
		return
	}

	// Recupera la posizione dell'evento
	positionOfEvent, err := e.App.FindRecordById("positions", e.Record.GetString("position"))
	if err != nil {
		// Log dell'errore nel recupero della posizione dell'evento
		log.Printf("Errore nel recupero della posizione dell'evento: %v", err)
		return
	}

	// Filtra gli utenti in base allo stato
	users, err := dbtools.GetUsersByState(e.App, positionOfEvent.GetString("state"))
	if err != nil {
		// Log dell'errore nel recupero degli utenti
		log.Printf("Errore nel recupero degli utenti: %v", err)
		return
	}

	pushNotifications := []dbtools.PushNotification{}
	for _, user := range users {
		pushNotifications = append(pushNotifications, dbtools.PushNotification{
			UserId: user,
			TrTag:  "push_notification.new_event",
			TrNamedParams: map[string]string{
				"name":  e.Record.GetString("name"),
				"state": positionOfEvent.GetString("state"),
			},
			Data: map[string]string{
				"type":     "event",
				"event_id": e.Record.GetString("id"),
			},
		})
	}

	// Invia le notifiche push agli utenti filtrati
	dbtools.SendPushNotificationToUsers(e.App, pushNotifications)
}

// EventsUpdateNotifyUsers gestisce l'invio di notifiche push agli utenti
// quando un evento viene aggiornato.
// Se l'evento è nazionale, invia una notifica a tutti gli utenti.
// Se l'evento è locale, recupera gli utenti in base alla posizione
// dell'evento e invia notifiche individuali.
func EventsUpdateNotifyUsers(e *core.RecordEvent) {
	// Controllo se l'evento è nazionale
	if e.Record.Get("is_national") == true {
		dbtools.SendPushNotificationToAllUsers(e.App, dbtools.PushNotification{
			TrTag: "push_notification.update_national_event",
			TrNamedParams: map[string]string{
				"name": e.Record.GetString("name"),
			},
			Data: map[string]string{
				"type":     "event",
				"event_id": e.Record.GetString("id"),
			},
		})
		return
	}

	// Recupera la posizione dell'evento
	positionOfEvent, err := e.App.FindRecordById("positions", e.Record.GetString("position"))
	if err != nil {
		log.Printf("Errore nel recupero della posizione dell'evento: %v", err)
		return
	}

	users, err := dbtools.GetUsersByState(e.App, positionOfEvent.GetString("state"))
	if err != nil {
		log.Printf("Errore nel recupero degli utenti: %v", err)
		return
	}

	pushNotifications := []dbtools.PushNotification{}
	for _, user := range users {
		pushNotifications = append(pushNotifications, dbtools.PushNotification{
			UserId: user,
			TrTag:  "push_notification.update_event",
			TrNamedParams: map[string]string{
				"name":  e.Record.GetString("name"),
				"state": positionOfEvent.GetString("state"),
			},
			Data: map[string]string{
				"type":     "event",
				"event_id": e.Record.GetString("id"),
			},
		})
	}

	dbtools.SendPushNotificationToUsers(e.App, pushNotifications)
}
