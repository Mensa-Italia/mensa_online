package payment

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/stripe/stripe-go/v81/webhook"
	"io"
	"mensadb/tools/env"
	"net/http"
	"strings"
)

func webhookStripe(e *core.RequestEvent) error {
	const MaxBodyBytes = int64(65536)
	payloadToRead := http.MaxBytesReader(e.Response, e.Request.Body, MaxBodyBytes)
	payload, err := io.ReadAll(payloadToRead)
	if err != nil {
		return e.String(http.StatusBadRequest, "Invalid payload")
	}

	event, err := webhook.ConstructEvent(payload, e.Request.Header.Get("Stripe-Signature"), env.GetStripeWebhookSignature())
	if err != nil {
		return e.JSON(http.StatusBadRequest, err.Error())
	}

	if strings.Contains(string(event.Type), "payment_intent") {
		paymentIntent := event.Data.Object
		records, err := e.App.FindAllRecords("payments", dbx.NewExp("stripe_code = {:id}", dbx.Params{"id": paymentIntent["id"]}))
		if err != nil {
			return err
		}
		if len(records) == 0 {
			return e.String(http.StatusOK, "We have received the webhook but this service doesn't know this payment")
		}
		record := records[0]
		record.Set("status", paymentIntent["status"])
		err = e.App.Save(record)
		if err != nil {
			return e.String(http.StatusInternalServerError, "Error saving payment")
		}
	}
	return e.String(http.StatusOK, "OK")
}
