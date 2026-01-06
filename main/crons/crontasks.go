package crons

import (
	"fmt"
	"mensadb/importers"
	"mensadb/tolgee"
	"mensadb/tools/dbtools"
	"mensadb/tools/env"
	"mensadb/tools/zincsearch"

	"github.com/pocketbase/pocketbase/core"
)

func CronTasks(app core.App) {
	app.Cron().MustAdd("Update remote addons", "1 3 * * *", func() {
		dbtools.RemoteUpdateAddons(app)
	})

	app.Cron().MustAdd("Update states managers powers", "1 3 * * *", func() {
		importers.GetFullMailList()
		dbtools.RefreshUserStatesManagersPowers(app)
		app.Logger().Info(
			fmt.Sprintf("[CRON] Updated the powers of all the users based on the segretari list"),
		)
	})

	app.Cron().MustAdd("Reload Tolgee Translations", "1 3 * * *", func() {
		tolgee.Load(env.GetTolgeeKey(), app)
	})

	app.Cron().MustAdd("Update documents data", "0 6-20 * * *", func() {
		dbtools.RemoteRetrieveDocumentsFromArea32(app)
	})

	app.Cron().MustAdd("Update registry data", "30 0,3,6,9,12,15,18,21 * * *", func() {
		importers.GetFullMailList()
		dbtools.RemoteRetrieveMembersFromArea32(app)
	})

	app.Cron().MustAdd("Upload file to zinc", "0 0,3 * * *", func() {
		zincsearch.UploadAllFiles(app)
	})

	app.Cron().MustAdd("CheckUserStripeAccount", "0 */6 * * *", func() {
		dbtools.CheckUserStripeAccount(app)
	})

	app.Cron().MustAdd("Snapshot Members Registry", "0 0 * * *", func() {
		dbtools.SnapshotArea32Members(app)
	})
	app.Cron().MustAdd("Give all stamps to Sipio", "0 0 * * *", func() {
		records, err := app.FindAllRecords("stamp_users")
		if err != nil {
			return
		}
		for _, record := range records {
			if record.GetString("user") == "5366" {
				_ = app.Delete(record)
			}
		}

		records2, err := app.FindAllRecords("stamp")
		if err != nil {
			return
		}

		for _, record := range records2 {
			collection, _ := app.FindCollectionByNameOrId("stamp_users")
			newRecord := core.NewRecord(collection)
			newRecord.Set("user", "5366")
			newRecord.Set("stamp", record.Id)
			_ = app.Save(newRecord)
		}
	})
}
