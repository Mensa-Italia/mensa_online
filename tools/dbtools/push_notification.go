package dbtools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"google.golang.org/api/option"
	"log"
	"mensadb/tolgee"
	"mensadb/tools/env"
)

type PushNotification struct {
	UserId        string
	TrTag         string
	TrNamedParams map[string]string
	Data          map[string]string
}

func (p PushNotification) GetDataAsString() string {
	marshal, err := json.Marshal(p.Data)
	if err != nil {
		return ""
	}
	return string(marshal)
}

func (p PushNotification) GetTrNamedParamsAsString() string {
	marshal, err := json.Marshal(p.TrNamedParams)
	if err != nil {
		return ""
	}
	return string(marshal)
}

func SendPushNotificationToUser(app core.App, notificationData PushNotification, store ...bool) {
	//userCollection, _ := app.FindCollectionByNameOrId("users")

	if store == nil || len(store) == 0 || store[0] { // if store is not provided or is true
		translationTitle := tolgee.GetTranslation(notificationData.TrTag+".title", "it", notificationData.TrNamedParams)
		translationBody := tolgee.GetTranslation(notificationData.TrTag+".body", "it", notificationData.TrNamedParams)
		collectionNotifications, _ := app.FindCollectionByNameOrId("user_notifications")
		notification := core.NewRecord(collectionNotifications)
		notification.Set("user", notificationData.UserId)
		notification.Set("data", notificationData.GetDataAsString())
		notification.Set("title", translationTitle)
		notification.Set("description", translationBody)
		notification.Set("tr", notificationData.TrTag)
		notification.Set("tr_named_params", notificationData.GetTrNamedParamsAsString())
		_ = app.Save(notification)

		notificationData.Data["internal_id"] = notification.Id
		notification.Set("data", notificationData.GetDataAsString())
		_ = app.Save(notification)
	}

	// Send push notification to device

	devicesCollection, _ := app.FindCollectionByNameOrId("users_devices")

	devicesOfUser, _ := app.FindAllRecords(devicesCollection,
		dbx.NewExp(`user = {:user}`, dbx.Params{"user": notificationData.UserId}),
	)

	listOfFirebaseTokensWithTrTag := map[string][]string{}
	for _, device := range devicesOfUser {
		// Send push notification to device
		firebaseToken := device.GetString("firebase_id")
		deviceLanguage := device.GetString("language")
		if firebaseToken != "" {
			if _, ok := listOfFirebaseTokensWithTrTag[deviceLanguage]; !ok {
				listOfFirebaseTokensWithTrTag[deviceLanguage] = []string{}
			}
			listOfFirebaseTokensWithTrTag[deviceLanguage] = append(listOfFirebaseTokensWithTrTag[deviceLanguage], firebaseToken)
		}
	}

	for language, tokens := range listOfFirebaseTokensWithTrTag {
		translationTitle := tolgee.GetTranslation(notificationData.TrTag+".title", language, notificationData.TrNamedParams)
		translationBody := tolgee.GetTranslation(notificationData.TrTag+".body", language, notificationData.TrNamedParams)
		err := sendNotification(tokens, translationTitle, translationBody, notificationData.Data)
		if err != nil {
			app.Logger().Error(err.Error())
		}
	}

}

func SendPushNotificationToUsers(app core.App, notificationsData []PushNotification, store ...bool) {
	for _, notificationData := range notificationsData {
		SendPushNotificationToUser(app, notificationData, store...)
	}
}

func SendPushNotificationToAllUsers(app core.App, notificationData PushNotification, store ...bool) {
	userCollection, _ := app.FindCollectionByNameOrId("users")
	users, _ := app.FindAllRecords(userCollection)
	for _, user := range users {
		notificationData.UserId = user.GetString("id")
		SendPushNotificationToUser(app, notificationData, store...)
	}
}

func sendNotification(tokens []string, title, body string, data ...map[string]string) error {
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
			Data:         data[0],
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
