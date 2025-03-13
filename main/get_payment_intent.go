package main

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/stripe/stripe-go/v81"
	"mensadb/tools/dbtools"
	"mensadb/tools/payment"
)

func getPaymentIntentHandler(e *core.RequestEvent) error {
	isLogged, authUser := dbtools.UserIsLoggedIn(e)
	if !isLogged {
		return e.String(401, "Unauthorized")
	}

	id := e.Request.PathValue("id")
	if id == "" {
		return e.String(400, "Invalid id")
	}
	collection, err := app.FindCollectionByNameOrId("payments")
	if err != nil {
		return err
	}

	record, err := app.FindRecordById(collection, id)
	if err != nil {
		return e.String(404, "Payment not found")
	}

	if record.Get("user") != authUser.Id {
		return e.String(401, "Unauthorized")
	}

	clientStripe := payment.GetClient()
	intent, err := clientStripe.PaymentIntents.Get(record.Get("stripe_code").(string), &stripe.PaymentIntentParams{
		Expand: stripe.StringSlice([]string{"payment_method"}),
	})
	if err != nil {
		return e.String(500, "Error getting payment")
	}
	return e.JSON(200, intent)
}
