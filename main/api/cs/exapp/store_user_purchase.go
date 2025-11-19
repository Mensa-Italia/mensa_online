package exapp

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"mensadb/main/hooks"
)

func StoreUserPurchase(e *core.RequestEvent) error {
	authKey := e.Request.Header.Get("Authorization")
	if !hooks.CheckKey(e.App, authKey, "PUSH_PAYMENTS_DATA") {
		return e.String(401, "Unauthorized")
	}
	userId := e.Request.FormValue("user_id")
	var userRecord *core.Record
	var err error
	if userId == "" {
		userEmail := e.Request.FormValue("user_email")
		userRecord, err = e.App.FindFirstRecordByFilter("users", "email={:user_email}", dbx.Params{"user_email": userEmail})
		if err != nil || userRecord == nil {
			userRecord, err = e.App.FindFirstRecordByFilter("members_registry", "full_data.E-mail={:user_email}", dbx.Params{"user_email": userEmail})
			if err != nil || userRecord == nil {
				return e.InternalServerError("User not found", nil)
			}
		}
	} else {
		userRecord, err = e.App.FindRecordById("users", userId)
		if err != nil || userRecord == nil {
			return e.InternalServerError("User not found", nil)
		}
	}
	collection, _ := e.App.FindCollectionByNameOrId("payments")
	purchaseRecord := core.NewRecord(collection)
	purchaseRecord.Set("description", e.Request.FormValue("description"))
	purchaseRecord.Set("user", userRecord.Id)
	purchaseRecord.Set("stripe_code", e.Request.FormValue("stripe_code"))
	purchaseRecord.Set("status", e.Request.FormValue("status"))
	purchaseRecord.Set("amount", e.Request.FormValue("amount"))
	purchaseRecord.Set("link", e.Request.FormValue("link"))

	err = e.App.Save(purchaseRecord)
	if err != nil {
		return e.InternalServerError("Failed to save purchase record", err)
	}

	return e.String(200, "OK")

}
