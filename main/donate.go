package main

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/stripe/stripe-go/v81"
	"mensadb/tools/dbtools"
	"mensadb/tools/payment"
	"strconv"
)

func donateHandler(e *core.RequestEvent) error {
	isLogged, authUser := dbtools.UserIsLoggedIn(e)
	if !isLogged {
		return e.String(401, "Unauthorized")
	}

	amount := e.Request.FormValue("amount")
	if amount == "" {
		return e.String(400, "Invalid amount")
	}

	intAmount, err := strconv.ParseInt(amount, 10, 64)
	if err != nil {
		return e.String(400, "Invalid amount")
	}

	_, paymentIntent, err := createPayment(authUser.Id, int(intAmount))

	return e.JSON(200, paymentIntent)
}

func createPayment(userId string, amount int) (*core.Record, *stripe.PaymentIntent, error) {
	collection, err := app.FindCollectionByNameOrId("payments")
	if err != nil {
		return nil, nil, err
	}

	paymentIntent, err := stripeCreatePaymentIntent(userId, int64(amount))
	if err != nil {
		return nil, nil, err
	}
	record := core.NewRecord(collection)
	record.Set("amount", amount)
	record.Set("user", userId)
	record.Set("stripe_code", paymentIntent.ID)
	record.Set("status", string(paymentIntent.Status))
	err = app.Save(record)
	if err != nil {
		return nil, nil, err
	}
	return record, paymentIntent, nil
}

func stripeCreatePaymentIntent(userId string, amount int64) (*stripe.PaymentIntent, error) {
	customerId, err := getCustomerId(userId)
	if err != nil {
		return nil, err
	}
	customer, err := payment.GetClient().Customers.Get(customerId, nil)
	if err != nil {
		return nil, err
	}
	params := &stripe.PaymentIntentParams{
		Amount:      stripe.Int64(amount),
		Currency:    stripe.String(string(stripe.CurrencyEUR)),
		Customer:    stripe.String(customerId),
		Description: stripe.String("Donation"),
		AutomaticPaymentMethods: &stripe.PaymentIntentAutomaticPaymentMethodsParams{
			Enabled: stripe.Bool(true),
		},
		PaymentMethod: stripe.String(customer.InvoiceSettings.DefaultPaymentMethod.ID),
		Metadata: map[string]string{
			"userId":     userId,
			"created_by": "Mensa Online",
		},
	}
	clientStripe := payment.GetClient()
	intent, err := clientStripe.PaymentIntents.New(params)
	if err != nil {
		return nil, err
	}
	return intent, nil
}
