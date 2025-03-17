package cs

import (
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"mensadb/area32"
	"mensadb/tools/dbtools"
	"time"
)

func SendUpdateNotifyHandler(e *core.RequestEvent) error {
	// Send update notify to all addons
	email := e.Request.FormValue("email")
	password := e.Request.FormValue("password")

	// Inizializza l'API Area32 per autenticare l'utente e recuperare i suoi dati principali
	scraperApi := area32.NewAPI()
	areaUser, err := scraperApi.DoLoginAndRetrieveMain(email, password)
	if err != nil {
		// Restituisce un errore se le credenziali non sono valide
		return apis.NewBadRequestError("Invalid credentials", err)
	}

	if areaUser.Id == "5366" {
		go func() {

			dbtools.SendPushNotificationToUser(e.App, dbtools.PushNotification{
				UserId: areaUser.Id,
				TrTag:  "push_notification.new_update_available",
			}, false)

			time.Sleep(30 * time.Second)

			dbtools.SendPushNotificationToAllUsers(e.App, dbtools.PushNotification{
				TrTag: "push_notification.new_update_available",
			}, false)
		}()
	}
	return e.String(200, "OK")
}
