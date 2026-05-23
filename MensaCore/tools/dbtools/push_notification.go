package dbtools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"mensadb/tolgee"
	"mensadb/tools/env"
	"sync"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"google.golang.org/api/option"
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

	if len(store) == 0 || store[0] { // if store is not provided or is true
		translationTitle := tolgee.GetTranslation(notificationData.TrTag+".title", "it", notificationData.TrNamedParams)
		translationBody := tolgee.GetTranslation(notificationData.TrTag+".body", "it", notificationData.TrNamedParams)
		collectionNotifications, err := app.FindCollectionByNameOrId("user_notifications")
		if err != nil {
			app.Logger().Error("push: cannot find user_notifications collection", "err", err, "user", notificationData.UserId)
			return
		}
		notification := core.NewRecord(collectionNotifications)
		notification.Set("user", notificationData.UserId)
		notification.Set("data", notificationData.GetDataAsString())
		notification.Set("title", translationTitle)
		notification.Set("description", translationBody)
		notification.Set("tr", notificationData.TrTag)
		notification.Set("tr_named_params", notificationData.GetTrNamedParamsAsString())
		if err := app.Save(notification); err != nil {
			app.Logger().Error("push: save notification failed", "err", err, "user", notificationData.UserId)
			return
		}

		notificationData.Data["internal_id"] = notification.Id
		notification.Set("data", notificationData.GetDataAsString())
		if err := app.Save(notification); err != nil {
			app.Logger().Error("push: update notification with internal_id failed", "err", err, "user", notificationData.UserId)
		}
	}

	// Send push notification to device

	devicesCollection, err := app.FindCollectionByNameOrId("users_devices")
	if err != nil {
		app.Logger().Error("push: cannot find users_devices collection", "err", err, "user", notificationData.UserId)
		return
	}

	devicesOfUser, err := app.FindAllRecords(devicesCollection,
		dbx.NewExp(`user = {:user}`, dbx.Params{"user": notificationData.UserId}),
	)
	if err != nil {
		app.Logger().Error("push: load user devices failed", "err", err, "user", notificationData.UserId)
		return
	}

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
	userCollection, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		app.Logger().Error("push: cannot find users collection", "err", err)
		return
	}
	users, err := app.FindAllRecords(userCollection)
	if err != nil {
		app.Logger().Error("push: load users failed", "err", err)
		return
	}

	shouldStore := len(store) == 0 || store[0]

	// Pre-load all devices into map[userID][]*core.Record per evitare N+1.
	devicesByUser := map[string][]*core.Record{}
	devicesCollection, err := app.FindCollectionByNameOrId("users_devices")
	if err != nil {
		app.Logger().Error("push: cannot find users_devices collection", "err", err)
		return
	}
	allDevices, err := app.FindAllRecords(devicesCollection)
	if err != nil {
		app.Logger().Error("push: preload all devices failed, falling back to per-user query", "err", err)
		// non blocking: workers will read empty map and skip device delivery
	} else {
		for _, d := range allDevices {
			uid := d.GetString("user")
			devicesByUser[uid] = append(devicesByUser[uid], d)
		}
	}

	// Cache traduzioni Tolgee per la durata del batch.
	// Per `SendPushNotificationToAllUsers` TrTag e TrNamedParams sono costanti,
	// quindi basta indicizzare per lingua.
	titleByLang := map[string]string{}
	bodyByLang := map[string]string{}
	var transMu sync.Mutex
	getTrans := func(lang string, cache map[string]string, key string) string {
		transMu.Lock()
		defer transMu.Unlock()
		if v, ok := cache[lang]; ok {
			return v
		}
		v := tolgee.GetTranslation(key, lang, notificationData.TrNamedParams)
		cache[lang] = v
		return v
	}

	notificationsCollection, err := app.FindCollectionByNameOrId("user_notifications")
	if err != nil {
		app.Logger().Error("push: cannot find user_notifications collection", "err", err)
		return
	}

	const maxWorkers = 50
	sem := make(chan struct{}, maxWorkers)
	var wg sync.WaitGroup

	for _, user := range users {
		u := user
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			defer func() {
				if r := recover(); r != nil {
					app.Logger().Error("push: panic in worker", "user", u.Id, "panic", r)
				}
			}()

			// Build a per-user copy of notificationData (UserId + Data are user-scoped).
			perUser := notificationData
			perUser.UserId = u.Id
			// Copy Data map to avoid sharing internal_id mutation across workers.
			perUserData := make(map[string]string, len(notificationData.Data)+1)
			for k, v := range notificationData.Data {
				perUserData[k] = v
			}
			perUser.Data = perUserData

			if shouldStore {
				translationTitleIt := getTrans("it", titleByLang, perUser.TrTag+".title")
				translationBodyIt := getTrans("it", bodyByLang, perUser.TrTag+".body")
				notification := core.NewRecord(notificationsCollection)
				notification.Set("user", perUser.UserId)
				notification.Set("data", perUser.GetDataAsString())
				notification.Set("title", translationTitleIt)
				notification.Set("description", translationBodyIt)
				notification.Set("tr", perUser.TrTag)
				notification.Set("tr_named_params", perUser.GetTrNamedParamsAsString())
				if err := app.Save(notification); err != nil {
					app.Logger().Error("push: save notification failed", "err", err, "user", perUser.UserId)
					return
				}

				perUser.Data["internal_id"] = notification.Id
				notification.Set("data", perUser.GetDataAsString())
				if err := app.Save(notification); err != nil {
					app.Logger().Error("push: update notification with internal_id failed", "err", err, "user", perUser.UserId)
				}
			}

			devicesOfUser := devicesByUser[u.Id]
			if len(devicesOfUser) == 0 {
				return
			}

			tokensByLang := map[string][]string{}
			for _, device := range devicesOfUser {
				firebaseToken := device.GetString("firebase_id")
				deviceLanguage := device.GetString("language")
				if firebaseToken != "" {
					tokensByLang[deviceLanguage] = append(tokensByLang[deviceLanguage], firebaseToken)
				}
			}

			for language, tokens := range tokensByLang {
				translationTitle := getTrans(language, titleByLang, perUser.TrTag+".title")
				translationBody := getTrans(language, bodyByLang, perUser.TrTag+".body")
				if err := sendNotification(tokens, translationTitle, translationBody, perUser.Data); err != nil {
					app.Logger().Error("push: send FCM failed", "err", err, "user", perUser.UserId, "lang", language)
				}
			}
		}()
	}
	wg.Wait()
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
		return nil, errors.New("firebase auth key non configurata")
	}

	decodedKey, err := base64.StdEncoding.DecodeString(fireBaseAuthKey)
	if err != nil {
		return nil, err
	}

	return decodedKey, nil
}
