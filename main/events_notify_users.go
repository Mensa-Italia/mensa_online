package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"slices"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"google.golang.org/api/option"
	"mensadb/tools/env"
)

func EventsNotifyUsersAsync(e *core.RecordEvent) error {
	go func(e core.RecordEvent) {
		err := EventsNotifyUsers(&e)
		if err != nil {
			log.Printf("Errore nell'invio delle notifiche: %v", err)
		}
	}(*e)
	return e.Next()
}

func EventsNotifyUsers(e *core.RecordEvent) error {
	// Controllo se l'evento è nazionale
	if e.Record.Get("is_national") == true {
		return notifyAllUsers("Nuovo evento NAZIONALE!", e.Record.GetString("name"))
	}

	// Recupera la posizione dell'evento
	positionOfEvent, err := e.App.FindRecordById("positions", e.Record.GetString("position"))
	if err != nil {
		log.Printf("Errore nel recupero della posizione dell'evento: %v", err)
		return e.Next()
	}

	// Filtra gli utenti in base allo stato
	users, err := fetchUsersByState(positionOfEvent.GetString("state"))
	if err != nil {
		log.Printf("Errore nel recupero degli utenti: %v", err)
		return e.Next()
	}

	// Recupera i token dei dispositivi per gli utenti
	tokens, err := fetchDeviceTokens(users)
	if err != nil {
		log.Printf("Errore nel recupero dei token dei dispositivi: %v", err)
		return e.Next()
	}

	// Invia la notifica
	err = sendNotification(tokens, "Nuovo evento in "+positionOfEvent.GetString("state")+"!", e.Record.GetString("name"))
	if err != nil {
		log.Printf("Errore durante l'invio della notifica: %v", err)
	}

	return e.Next()
}

func notifyAllUsers(title, body string) error {
	// Recupera tutti i token dei dispositivi
	tokens, err := fetchAllDeviceTokens()
	if err != nil {
		return err
	}

	// Invia la notifica
	return sendNotification(tokens, title, body)
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

func fetchDeviceTokens(userIDs []string) ([]string, error) {
	var tokens []string
	for _, id := range userIDs {
		records, err := app.FindAllRecords("users_devices",
			dbx.NewExp(`firebase_id != {:id} AND user = {:user_ids}`, dbx.Params{"id": "NOTOKEN", "user_ids": id}),
		)
		if err != nil {
			log.Println(err)
			continue
		}

		for _, record := range records {
			tokens = append(tokens, record.GetString("firebase_id"))
		}
	}
	return tokens, nil
}

func fetchAllDeviceTokens() ([]string, error) {
	records, err := app.FindAllRecords("users_devices", dbx.NewExp(`firebase_id != {:id}`, dbx.Params{"id": "NOTOKEN"}))
	if err != nil {
		return nil, err
	}

	var tokens []string
	for _, record := range records {
		tokens = append(tokens, record.GetString("firebase_id"))
	}
	return tokens, nil
}

func sendNotification(tokens []string, title, body string) error {
	decodedKey, err := getDecodedFireBaseKey()
	if err != nil {
		return err
	}

	opts := []option.ClientOption{option.WithCredentialsJSON(decodedKey)}
	appFirebase, err := firebase.NewApp(context.Background(), nil, opts...)
	if err != nil {
		return err
	}

	fcmClient, err := appFirebase.Messaging(context.Background())
	if err != nil {
		return err
	}

	// Invio notifiche in batch da 400 token
	for i := 0; i < len(tokens); i += 400 {
		end := i + 400
		if end > len(tokens) {
			end = len(tokens)
		}

		response, err := fcmClient.SendEachForMulticast(context.Background(), &messaging.MulticastMessage{
			Notification: &messaging.Notification{Title: title, Body: body},
			Tokens:       tokens[i:end],
		})
		if err != nil {
			log.Printf("Errore nell'invio delle notifiche batch: %v", err)
		} else {
			log.Printf("Notifiche inviate correttamente: %d successi, %d fallimenti", response.SuccessCount, response.FailureCount)
		}
	}

	return nil
}

func getDecodedFireBaseKey() ([]byte, error) {
	fireBaseAuthKey := env.GetFireBaseAuthKey()
	if fireBaseAuthKey == "" {
		return nil, errors.New("Firebase Auth Key non configurata")
	}

	decodedKey, err := base64.StdEncoding.DecodeString(fireBaseAuthKey)
	if err != nil {
		return nil, err
	}

	return decodedKey, nil
}

func ShowEventsTest(e *core.RequestEvent) error {
	users, _ := fetchUsersByState("Piemonte")
	devices, _ := fetchDeviceTokens(users)
	sendNotification(devices, "Test", "Test")

	return e.JSON(200, devices)
}
