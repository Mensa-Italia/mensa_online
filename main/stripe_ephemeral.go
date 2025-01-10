package main

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/stripe/stripe-go/v81"
	"mensadb/tools/payment"
)

func PaymentMethodCreateHandler(e *core.RequestEvent) error {
	isLogged, authUser := isLoggedIn(e)
	if !isLogged {
		return e.String(401, "Unauthorized")
	}
	customerId, err := getCustomerId(authUser.Id)
	if err != nil {
		return e.String(500, "Internal server error")
	}
	seti, _ := payment.GetClient().SetupIntents.New(&stripe.SetupIntentParams{
		Customer: &customerId,
	})
	return e.JSON(200, seti)

}

func GetPaymentMethodsHandler(e *core.RequestEvent) error {
	isLogged, authUser := isLoggedIn(e) //
	if !isLogged {
		return e.String(401, "Unauthorized ")
	}
	customerId, err := getCustomerId(authUser.Id)
	if err != nil {
		return e.String(500, "Internal server error")
	}
	pm := payment.GetClient().Customers.ListPaymentMethods(&stripe.CustomerListPaymentMethodsParams{
		Customer: &customerId,
	})
	return e.JSON(200, pm.PaymentMethodList().Data)
}

func setDefaultPaymentMethod(e *core.RequestEvent) error {
	isLogged, authUser := isLoggedIn(e)
	paymentMethodId := e.Request.FormValue("payment_method_id")
	if !isLogged {
		return e.String(401, "Unauthorized")
	}
	customerId, err := getCustomerId(authUser.Id)
	if err != nil {
		return e.String(500, "Internal server error")
	}
	pm, err := payment.GetClient().Customers.Update(customerId, &stripe.CustomerParams{
		InvoiceSettings: &stripe.CustomerInvoiceSettingsParams{
			DefaultPaymentMethod: &paymentMethodId,
		},
	})
	if err != nil {
		return e.String(500, "Internal server error")
	}
	return e.JSON(200, pm)
}

func getCustomerHandler(e *core.RequestEvent) error {
	isLogged, authUser := isLoggedIn(e)
	if !isLogged {
		return e.String(401, "Unauthorized")
	}
	customerId, err := getCustomerId(authUser.Id)
	if err != nil {
		return e.String(500, "Internal server error")
	}
	customer, err := payment.GetClient().Customers.Get(customerId, nil)
	if err != nil {
		return e.String(500, "Internal server error")
	}
	return e.JSON(200, customer)
}

func getCustomerId(userId string) (string, error) {
	recordUser, err := app.FindRecordById("users", userId)
	if err != nil {
		return "", err
	}

	records, err := app.FindAllRecords("users_secrets", dbx.NewExp("user = {:id} AND key = {:key}",
		dbx.Params{
			"id":  userId,
			"key": "stripe_customer_id",
		}))
	var record *core.Record
	if err != nil || len(records) == 0 {
		collection, _ := app.FindCollectionByNameOrId("users_secrets")
		record = core.NewRecord(collection)
		record.Set("user", userId)
		record.Set("key", "stripe_customer_id")
		customer, err := payment.NewCustomer(userId, recordUser.Get("name").(string), recordUser.Get("email").(string))
		if err != nil {
			return "", err
		}
		record.Set("value", customer.ID)
		err = app.Save(record)
		if err != nil {
			return "", err
		}
	} else {
		record = records[0]
	}
	if record.Get("value") == nil {
		return "", nil
	}
	return record.Get("value").(string), nil
}
