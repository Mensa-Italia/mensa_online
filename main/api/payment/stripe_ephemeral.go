package payment

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/stripe/stripe-go/v81"
	"mensadb/tools/dbtools"
	"mensadb/tools/payment"
)

func PaymentMethodCreateHandler(e *core.RequestEvent) error {
	isLogged, authUser := dbtools.UserIsLoggedIn(e)
	if !isLogged {
		return e.String(401, "Unauthorized")
	}
	customerId, err := payment.GetCustomerId(e.App, authUser.Id)
	if err != nil {
		return e.String(500, "Internal server error")
	}
	seti, _ := payment.GetClient().SetupIntents.New(&stripe.SetupIntentParams{
		Customer: &customerId,
	})
	return e.JSON(200, seti)

}

func GetPaymentMethodsHandler(e *core.RequestEvent) error {
	isLogged, authUser := dbtools.UserIsLoggedIn(e)
	if !isLogged {
		return e.String(401, "Unauthorized ")
	}
	customerId, err := payment.GetCustomerId(e.App, authUser.Id)
	if err != nil {
		return e.String(500, "Internal server error")
	}
	pm := payment.GetClient().Customers.ListPaymentMethods(&stripe.CustomerListPaymentMethodsParams{
		Customer: &customerId,
	})
	return e.JSON(200, pm.PaymentMethodList().Data)
}

func setDefaultPaymentMethod(e *core.RequestEvent) error {
	isLogged, authUser := dbtools.UserIsLoggedIn(e)
	paymentMethodId := e.Request.FormValue("payment_method_id")
	if !isLogged {
		return e.String(401, "Unauthorized")
	}
	customerId, err := payment.GetCustomerId(e.App, authUser.Id)
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
	isLogged, authUser := dbtools.UserIsLoggedIn(e)
	if !isLogged {
		return e.String(401, "Unauthorized")
	}
	customerId, err := payment.GetCustomerId(e.App, authUser.Id)
	if err != nil {
		return e.String(500, "Internal server error")
	}
	customer, err := payment.GetClient().Customers.Get(customerId, nil)
	if err != nil {
		return e.String(500, "Internal server error")
	}
	return e.JSON(200, customer)
}
