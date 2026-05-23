package dbtools

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"mensadb/tools/payment"
)

func CheckUserStripeAccount(app core.App) {

	users, _ := app.FindAllRecords("users", nil)

	for _, user := range users {
		filter, err := app.FindFirstRecordByFilter("users_secrets", "user = {:user} AND key = {:key}", dbx.Params{
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
				collection, err := app.FindCollectionByNameOrId("users_secrets")
				if err != nil || collection == nil {
					app.Logger().Error("find collection users_secrets failed", "err", err)
					continue
				}
				record := core.NewRecord(collection)
				record.Set("user", user.Id)
				record.Set("key", "stripe_customer_id")
				record.Set("value", customer.ID)
				if err := app.Save(record); err != nil {
					app.Logger().Error("save record failed", "collection", record.Collection().Name, "user", user.Id, "err", err)
				}
			}
		}
	}

}
