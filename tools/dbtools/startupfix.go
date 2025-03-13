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
		email := record.GetString("email")
		if email != "" {
			record.Set("email", strings.ToLower(email))
		}
		_ = app.Save(record)
	}
}
