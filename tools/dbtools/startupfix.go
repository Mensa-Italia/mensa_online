package dbtools

import (
	"github.com/pocketbase/pocketbase"
	"strings"
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
			record.SetEmail(strings.ToLower(email))
			record.Set("username", strings.ToLower(record.GetString("username")))
		}
		_ = app.Save(record)
	}
}
