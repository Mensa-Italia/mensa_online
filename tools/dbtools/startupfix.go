package dbtools

import (
	"github.com/pocketbase/pocketbase"
)

func StartupFix(app *pocketbase.PocketBase) {
	fixUsersEmails(app)
}

func fixUsersEmails(app *pocketbase.PocketBase) {
	userCollection, _ := app.FindCollectionByNameOrId("users")
	records, _ := app.FindAllRecords(userCollection)
	for _, record := range records {
		email := record.Email()
		if email != "" {
			record.SetEmail(email)
		}
		_ = app.Save(record)
	}
}
