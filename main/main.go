package main

import (
	"context"
	"fmt"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/cron"
	"github.com/pocketbase/pocketbase/tools/filesystem"
	"github.com/tidwall/gjson"
	"log"
	"mensadb/importers"
	_ "mensadb/migrations"
	"mensadb/tools/aipower"
	"mensadb/tools/signatures"
	"os"
	"strings"
	"time"
)

var app = pocketbase.New()

func main() {
	go importers.GetFullMailList()
	app.OnBootstrap().BindFunc(func(e *core.BootstrapEvent) error {
		scheduler := cron.New()

		// Update addons data every day at 3:01
		scheduler.MustAdd("updateAddonsData", "1 3 * * *", func() {
			go updateAddonsData()
			go func() {
				importers.GetFullMailList()
				updateStateManagers()
				app.Logger().Info(
					fmt.Sprintf("[CRON] Updated the powers of all the users based on the segretari list"),
				)
			}()
		})
		scheduler.MustAdd("updateDocumentsData", "0 8,11,14,17,20 * * *", func() {
			go UpdateDocumentsFromArea32()
		})
		scheduler.Start()
		app.Logger().Info(
			"[CRON] Scheduled all crons jobs",
		)

		if err := e.Next(); err != nil {
			return err
		}
		return nil
	})

	//migratecmd.MustRegister(app, app.RootCmd, migratecmd.Config{
	//	Automigrate: strings.HasPrefix(os.Args[0], os.TempDir()), // Automigrate only in tests
	//})

	app.OnServe().BindFunc(func(e *core.ServeEvent) error {
		e.Router.POST("/api/cs/auth-with-area", AuthWithAreaHandler)
		e.Router.POST("/api/cs/send-update-notify", SendUpdateNotifyHandler)
		e.Router.GET("/api/cs/sign-payload/{addon}", SignPayloadHandler)
		e.Router.GET("/api/cs/keys/{addon}", GetAddonPublicKeysHandler)
		e.Router.POST("/api/cs/verify-signature/{addon}", VerifySignatureHandler)
		e.Router.GET("/api/cs/force-update-addons", ForceUpdateAddonsHandler)
		e.Router.GET("/api/cs/force-document", forceUpdateDocumentHandler)
		e.Router.GET("/api/cs/force-update-state-managers", ForceUpdateStateManagersHandler)
		e.Router.GET("/ical/{hash}", RetrieveICAL)
		e.Router.GET("/api/position/state", GetStateHandler)
		e.Router.POST("/api/payment/method", PaymentMethodCreateHandler)
		e.Router.GET("/api/payment/method", GetPaymentMethodsHandler)
		e.Router.POST("/api/payment/default", setDefaultPaymentMethod)
		e.Router.GET("/api/payment/customer", getCustomerHandler)
		e.Router.POST("/api/payment/donate", donateHandler)
		e.Router.POST("/api/payment/webhook", webhookStripe)
		e.Router.GET("/api/payment/receipt/{id}", retrieveReceiptHandler)
		e.Router.GET("/api/payment/{id}", getPaymentIntentHandler)
		e.Router.POST("/api/telegram/check", checkTelegram)
		e.Router.POST("/api/payment/boutique", createBoutiquePaymentHandler)
		e.Router.GET("/static/{path...}", apis.Static(os.DirFS("./pb_public"), false))

		return e.Next()
	})
	app.OnRecordAfterUpdateSuccess("users").BindFunc(LogUserChart)
	app.OnRecordAfterCreateSuccess("addons").BindFunc(GeneratePublicPrivateKeys)
	app.OnRecordCreate("positions").BindFunc(PositionSetState)
	app.OnRecordCreate("ex_keys").BindFunc(OnKeyCreated)
	app.OnRecordAfterCreateSuccess("calendar_link").BindFunc(CalendarSetHash)
	app.OnRecordAfterCreateSuccess("events").BindFunc(EventsNotifyUsersAsync)

	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}

func GetAddonPublicKeysHandler(e *core.RequestEvent) error {
	addon := e.Request.PathValue("addon")
	record, err := app.FindRecordById("addons", addon)
	if err != nil {
		return apis.NewBadRequestError("Invalid addon", err)
	}

	return e.String(200, record.Get("public_key").(string))
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

func forceUpdateDocumentHandler(e *core.RequestEvent) error {
	go func() {
		collDocs, _ := app.FindCollectionByNameOrId("documents")
		record, _ := app.FindRecordsByFilter(
			collDocs,
			"file_data = ''",
			"-created",
			0,
			0,
		)

		for _, document := range record {
			if document.GetString("elaborated") != "" {
				continue
			}
			// construct the full file key by concatenating the record storage path with the specific filename
			fileKey := "https://svc.mensa.it/api/files/" + document.BaseFilesPath() + "/" + document.GetString("file")
			log.Println(fileKey)
			fsToUser, err := filesystem.NewFileFromURL(context.Background(), fileKey)
			if err != nil {
				log.Println(err)
				continue
			}
			// read the file data
			resume := aipower.AskResume(fsToUser)
			collectionElaborated, _ := app.FindCollectionByNameOrId("documents_elaborated")
			recordElaborated := core.NewRecord(collectionElaborated)
			recordElaborated.Set("document", document.Id)
			recordElaborated.Set("ia_resume", resume)
			_ = app.Save(recordElaborated)
			document.Set("elaborated", recordElaborated.Id)
			_ = app.Save(document)
			time.Sleep(5 * time.Second)
		}
	}()
	return e.String(200, "OK")
}

func checkTelegram(e *core.RequestEvent) error {
	authKey := e.Request.Header.Get("Authorization")
	if !CheckKey(authKey, "CHECK_USER_EXISTENCE") {
		return e.String(401, "Unauthorized")
	}
	userId := e.Request.FormValue("member_id")
	userEmail := e.Request.FormValue("email")

	user, err := app.FindRecordById("users", userId)
	if err != nil {
		return apis.NewBadRequestError("Invalid", err)
	}

	if strings.ToLower(user.GetString("email")) != strings.ToLower(userEmail) {
		return apis.NewBadRequestError("Invalid", nil)
	}

	return e.String(200, "OK")
}
