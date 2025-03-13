package main

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/stripe/stripe-go/v81"
	"mensadb/tools/dbtools"
	"mensadb/tools/payment"
)

func retrieveReceiptHandler(e *core.RequestEvent) error {
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
	itr := clientStripe.Charges.List(&stripe.ChargeListParams{
		PaymentIntent: stripe.String(record.Get("stripe_code").(string)),
	})
	if itr.Err() != nil {
		return e.String(500, "Error getting payment")
	}
	get := itr.ChargeList()
	return e.JSON(200, map[string]interface{}{
		"url": get.Data[0].ReceiptURL,
	})

}
