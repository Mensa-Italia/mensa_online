package main

import (
	"encoding/json"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/stripe/stripe-go/v81"
	"io"
	"strings"
)

func webhookStripe(e *core.RequestEvent) error {
	event := stripe.Event{}
	payload, err := io.ReadAll(e.Request.Body)
	if err != nil {
		return e.String(400, "Invalid payload")
	}
	if err := json.Unmarshal(payload, &event); err != nil {
		return e.String(400, "Invalid payload format")
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
