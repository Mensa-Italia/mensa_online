package main

import (
	"encoding/json"
	"log"
	"mensadb/tools/dbtools"
	"slices"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func EventsNotifyUsersAsync(e *core.RecordEvent) error {
	go func(e *core.RecordEvent) {
		EventsNotifyUsers(e)
	}(e)
	return e.Next()
}

func EventsNotifyUsers(e *core.RecordEvent) {
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
		log.Printf("Errore nel recupero della posizione dell'evento: %v", err)
	}

	// Filtra gli utenti in base allo stato
	users, err := fetchUsersByState(positionOfEvent.GetString("state"))
	if err != nil {
		log.Printf("Errore nel recupero degli utenti: %v", err)
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

	dbtools.SendPushNotificationToUsers(e.App, pushNotifications)
}

func fetchUsersByState(state string) ([]string, error) {
	records, err := app.FindAllRecords("users_metadata",
		dbx.NewExp(`key = 'notify_me_events'`),
	)
	if err != nil {
		return nil, err
	}

	var userIDs []string
	for _, record := range records {
		var notifyMeEvents []string
		value := record.GetString("value")
		_ = json.Unmarshal([]byte(value), &notifyMeEvents)
		if slices.Contains(notifyMeEvents, state) {
			userIDs = append(userIDs, record.GetString("user"))
		}
	}
	return userIDs, nil
}
