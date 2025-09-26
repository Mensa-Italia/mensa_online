package dbtools

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"mensadb/tools/payment"
)

func CheckUserStripeAccount(app core.App) {

	users, _ := app.FindAllRecords("users", nil)

	for _, user := range users {
		filter, err := app.FindFirstRecordByFilter("users_secrets", "user = ? AND key = ?", dbx.Params{
			"user": user.Id,
			"key":  "stripe_customer_id",
		})
		if err != nil || filter == nil {
			customer, _ := payment.NewCustomerIfNotExists(
				user.Id,
				user.GetString("name"),
				user.GetString("email"),
			)
			if customer != nil {
				collection, _ := app.FindCollectionByNameOrId("users_secrets")
				record := core.NewRecord(collection)
				record.Set("user", user.Id)
				record.Set("key", "stripe_customer_id")
				record.Set("value", customer.ID)
				_ = app.Save(record)
			}
		}
	}

}
