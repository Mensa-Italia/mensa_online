package main

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/stripe/stripe-go/v81/webhook"
	"io"
	"os"
	"strings"
)

func webhookStripe(e *core.RequestEvent) error {
	payload, err := io.ReadAll(e.Request.Body)
	if err != nil {
		return e.String(400, "Invalid payload")
	}

	event, err := webhook.ConstructEvent(payload, e.Request.Header.Get("Stripe-Signature"), os.Getenv("STRIPE_WEBHOOK_SECRET"))
	if err != nil {
		return e.String(400, "Invalid signature")
	}

	if strings.Contains(string(event.Type), "payment_intent") {
		paymentIntent := event.Data.Object
		records, err := app.FindAllRecords("payments", dbx.NewExp("stripe_code = {:id}", dbx.Params{"id": paymentIntent["id"]}))
		if err != nil {
			return err
		}
		if len(records) == 0 {
			return e.String(404, "Payment not found")
		}
		record := records[0]
		record.Set("status", paymentIntent["status"])
		err = app.Save(record)
		if err != nil {
			return e.String(500, "Error saving payment")
		}
	}
	return e.String(200, "OK")
}
