package main

import (
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/tidwall/gjson"
	"log"
	"mensadb/importers"
	"mensadb/main/api"
	"mensadb/main/hooks"
	_ "mensadb/migrations"
	"mensadb/tolgee"
	"mensadb/tools/dbtools"
	"mensadb/tools/env"
	"mensadb/tools/signatures"
	"os"
	"strings"
	"time"
)

var app = pocketbase.New()

func main() {
	tolgee.Load(env.GetTolgeeKey())
	go importers.GetFullMailList()

	app.OnServe().BindFunc(func(e *core.ServeEvent) error {
		dbtools.StartupFix(app)
		dbtools.CronTasks(app)
		return e.Next()
	})

	//migratecmd.MustRegister(app, app.RootCmd, migratecmd.Config{
	//	Automigrate: strings.HasPrefix(os.Args[0], os.TempDir()), // Automigrate only in tests
	//})

	app.OnServe().BindFunc(func(e *core.ServeEvent) error {

		api.Load(e.Router.Group("api/"))

		//e.Router.GET("/api/cs/keys/{addon}", keys.GetAddonPublicKeysHandler)
		//e.Router.GET("/api/position/state", position.GetStateHandler)

		e.Router.POST("/api/cs/auth-with-area", AuthWithAreaHandler)
		e.Router.POST("/api/cs/send-update-notify", SendUpdateNotifyHandler)
		e.Router.GET("/api/cs/sign-payload/{addon}", SignPayloadHandler)
		e.Router.POST("/api/cs/verify-signature/{addon}", VerifySignatureHandler)
		e.Router.GET("/api/cs/force-update-addons", ForceUpdateAddonsHandler)
		e.Router.GET("/api/cs/force-notification", forceNotification)
		e.Router.GET("/api/cs/force-update-state-managers", ForceUpdateStateManagersHandler)
		e.Router.GET("/ical/{hash}", RetrieveICAL)
		e.Router.POST("/api/payment/method", PaymentMethodCreateHandler)
		e.Router.GET("/api/payment/method", GetPaymentMethodsHandler)
		e.Router.POST("/api/payment/default", setDefaultPaymentMethod)
		e.Router.GET("/api/payment/customer", getCustomerHandler)
		e.Router.POST("/api/payment/donate", donateHandler)
		e.Router.POST("/api/payment/webhook", webhookStripe)
		e.Router.GET("/api/payment/receipt/{id}", retrieveReceiptHandler)
		e.Router.GET("/api/payment/{id}", getPaymentIntentHandler)
		e.Router.POST("/api/telegram/check", externalAppRequireConfirmation)
		e.Router.POST("/api/payment/boutique", createBoutiquePaymentHandler)
		e.Router.GET("/static/{path...}", apis.Static(os.DirFS("./pb_public"), false))

		return e.Next()
	})

	// Hooks to table events
	hooks.Load(app)

	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}

func VerifySignatureHandler(e *core.RequestEvent) error {
	addonId := e.Request.PathValue("addon")
	signature := e.Request.FormValue("signature")
	payload := e.Request.FormValue("payload")

	record, err := app.FindRecordById("addons", addonId)
	if err != nil {
		return apis.NewBadRequestError("Invalid addon", err)
	}

	isValid := signatures.ValidateSignature(payload, signature, record.Get("public_key").(string))

	payloadPure := payloadFromBase64(payload)

	if !gjson.ValidBytes([]byte(payloadPure)) {
		return apis.NewBadRequestError("Invalid payload", nil)
	}

	dataToUse := gjson.ParseBytes([]byte(payloadPure))

	if dataToUse.Get("expires_at").Time().After(time.Now()) &&
		dataToUse.Get("addon_id").String() == addonId &&
		isValid {
		return e.String(200, "OK")
	}
	return apis.NewBadRequestError("Invalid signature", nil)

}

func forceNotification(e *core.RequestEvent) error {
	user, _ := app.FindRecordById("users", "5366")

	dbtools.SendPushNotificationToUser(e.App, dbtools.PushNotification{
		UserId: user.Id,
		TrTag:  "push_notification.new_document_available",
		TrNamedParams: map[string]string{
			"name": "Delibera CDG 2025.2 Consiglio Vs Gabriel Garofalo",
		},
		Data: map[string]string{
			"type":        "single_document",
			"document_id": "5jsyp5i9cu9837v",
		},
	})
	return e.String(200, "OK")
}

func externalAppRequireConfirmation(e *core.RequestEvent) error {
	authKey := e.Request.Header.Get("Authorization")
	if !hooks.CheckKey(e.App, authKey, "CHECK_USER_EXISTENCE") {
		return e.String(401, "Unauthorized")
	}
	keyAppId, _ := hooks.GetKeyAppId(e.App, authKey)
	userId := e.Request.FormValue("member_id")
	userEmail := e.Request.FormValue("email")
	callmeURL := e.Request.FormValue("callme_url")

	exApp, _ := app.FindRecordById("ex_apps", keyAppId)

	user, err := app.FindRecordById("users", userId)
	if err != nil {
		return apis.NewBadRequestError("Invalid", err)
	}

	if strings.ToLower(user.GetString("email")) != strings.ToLower(userEmail) {
		return apis.NewBadRequestError("Invalid", nil)
	}

	dbtools.SendPushNotificationToUser(e.App, dbtools.PushNotification{
		UserId: user.Id,
		TrTag:  "push_notification.confirm_external_resource",
		TrNamedParams: map[string]string{
			"name": exApp.GetString("name"),
		},
		Data: map[string]string{
			"type":     "account_confirmation",
			"keyAppId": keyAppId,
			"url":      callmeURL,
		},
	})
	return e.String(200, "OK")
}
