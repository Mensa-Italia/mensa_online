package crons

import (
	"context"

	"mensadb/importers"
	"mensadb/main/cmd/searchcmd"
	"mensadb/main/crons/searchrec"
	"mensadb/tolgee"
	"mensadb/tools/dbtools"
	"mensadb/tools/env"

	"github.com/pocketbase/pocketbase/core"
)

func CronTasks(app core.App) {
	app.Cron().MustAdd("Update remote addons", "1 3 * * *", func() {
		dbtools.RemoteUpdateAddons(app)
	})

	app.Cron().MustAdd("Update states managers powers", "1 3 * * *", func() {
		importers.GetFullMailList()
		dbtools.RefreshUserStatesManagersPowers(app)
		app.Logger().Info("[CRON] Updated the powers of all the users based on the segretari list")
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

	app.Cron().MustAdd("Force zitadel sync", "0 3 * * *", func() {
		dbtools.UpdateZitadel(app)
	})

	app.Cron().MustAdd("CheckUserStripeAccount", "0 */6 * * *", func() {
		dbtools.CheckUserStripeAccount(app)
	})

	app.Cron().MustAdd("Retry missing documents resume", "0 3 1 * *", func() {
		dbtools.RetryMissingDocumentsResume(app)
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
				if err := app.Delete(record); err != nil {
					app.Logger().Error("delete record failed", "collection", record.Collection().Name, "id", record.Id, "err", err)
				}
			}
		}

		records2, err := app.FindAllRecords("stamp")
		if err != nil {
			return
		}

		for _, record := range records2 {
			collection, err := app.FindCollectionByNameOrId("stamp_users")
			if err != nil || collection == nil {
				app.Logger().Error("find collection stamp_users failed", "err", err)
				continue
			}
			newRecord := core.NewRecord(collection)
			newRecord.Set("user", "5366")
			newRecord.Set("stamp", record.Id)
			if err := app.Save(newRecord); err != nil {
				app.Logger().Error("save record failed", "collection", newRecord.Collection().Name, "stamp", record.Id, "err", err)
			}
		}
	})

	app.Cron().MustAdd("Search index reconciliation", "0 4 * * *", func() {
		searchrec.Run(app)
	})

	// Manual-only: schedule "Feb 31" never fires automatically. Trigger via the
	// PocketBase admin panel "Run" button on first deploy and for disaster recovery.
	app.Cron().MustAdd("Search index backfill (manual)", "0 0 31 2 *", func() {
		app.Logger().Info("[CRON] Search index backfill started")
		if err := searchcmd.Run(context.Background(), app, searchcmd.DefaultCollections, false); err != nil {
			app.Logger().Error("[CRON] Search index backfill failed", "err", err)
			return
		}
		app.Logger().Info("[CRON] Search index backfill completed")
	})
}
