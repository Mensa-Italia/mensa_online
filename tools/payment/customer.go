package payment

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func GetCustomerId(app core.App, userId string) (string, error) {
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
		customer, err := NewCustomer(userId, recordUser.Get("name").(string), recordUser.Get("email").(string))
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
