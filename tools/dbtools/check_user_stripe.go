package dbtools

import (
	"github.com/pocketbase/pocketbase/core"
	"mensadb/tools/payment"
)

func CheckUserStripeAccount(app core.App) {
	records, err := app.FindAllRecords("users_secrets", nil)
	if err != nil {
		return
	}

	for _, record := range records {
		if record.GetString("key") == "stripe_customer_id" {
			user, _ := app.FindRecordById("users", record.GetString("user"), nil)
			customer, _ := payment.NewCustomerIfNotExists(
				user.Id,
				user.GetString("name"),
				user.GetString("email"),
			)
			if customer != nil && customer.ID != record.GetString("value") {
				record.Set("value", customer.ID)
				_ = app.Save(record)
			}
		}
	}
}
