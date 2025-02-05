package main

import (
	"encoding/json"
	"github.com/go-resty/resty/v2"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/cron"
	"github.com/tidwall/gjson"
	"log"
	"mensadb/importers"
	_ "mensadb/migrations"
	"mensadb/tools/env"
	"mensadb/tools/signatures"
	"os"
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
			}()
		})
		scheduler.MustAdd("updateDocumentsData", "0 9,15,18,21 * * *", func() {
			go UpdateDocumentsFromArea32()
		})

		scheduler.Start()

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
		e.Router.POST("/api/payment/boutique", createBoutiquePaymentHandler)
		e.Router.GET("/static/{path...}", apis.Static(os.DirFS("./pb_public"), false))

		return e.Next()
	})
	app.OnRecordAfterUpdateSuccess("users").BindFunc(LogUserChart)
	app.OnRecordAfterCreateSuccess("addons").BindFunc(GeneratePublicPrivateKeys)
	app.OnRecordCreate("positions").BindFunc(PositionSetState)
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

func updateDocDescription() {
	collection, _ := app.FindCollectionByNameOrId("documents")
	documentsInside, _ := app.FindAllRecords(collection.Id, dbx.NewExp(`file_data = ''`))
	for _, doc := range documentsInside {
		key := doc.BaseFilesPath() + "/" + doc.GetString("file")
		fsys, _ := app.NewFilesystem()
		defer fsys.Close()
		blob, _ := fsys.GetFile(key)
		defer blob.Close()

		post, err := resty.New().R().SetFileReader("file", doc.GetString("file"), blob).SetFormData(map[string]string{
			"token": env.GetDocsUUID(),
		}).Post("http://127.0.0.1:8000/convert")
		if err != nil {
			log.Println(err)
			continue
		}
		var mapPost map[string]interface{}
		_ = json.Unmarshal(post.Body(), &mapPost)
		doc.Set("file_data", mapPost["result"])
		err = app.Save(doc)
		if err != nil {
			log.Println(err)
			continue
		}
	}

}
