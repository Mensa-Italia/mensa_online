package dbtools

import (
	"fmt"
	"github.com/pocketbase/pocketbase"
	"mensadb/importers"
	"mensadb/tolgee"
	"mensadb/tools/env"
	"mensadb/tools/zincsearch"
)

func CronTasks(app *pocketbase.PocketBase) {
	app.Cron().MustAdd("Update remote addons", "1 3 * * *", func() {
		RemoteUpdateAddons(app)
	})

	app.Cron().MustAdd("Update states managers powers", "1 3 * * *", func() {
		importers.GetFullMailList()
		RefreshUserStatesManagersPowers(app)
		app.Logger().Info(
			fmt.Sprintf("[CRON] Updated the powers of all the users based on the segretari list"),
		)
	})

	app.Cron().MustAdd("Reload Tolgee Translations", "1 3 * * *", func() {
		tolgee.Load(env.GetTolgeeKey())
	})

	app.Cron().MustAdd("Update documents data", "0 8,11,14,17,20 * * *", func() {
		RemoteRetrieveDocumentsFromArea32(app)
	})

	app.Cron().MustAdd("Update registry data", "0 0,3,6,9,12,15,18,21 * * *", func() {
		RemoteRetrieveMembersFromArea32(app)
	})

	app.Cron().MustAdd("Upload file to zinc", "0 0,3 * * *", func() {
		zincsearch.UploadAllFiles(app)
	})

	app.Cron().MustAdd("CheckUserStripeAccount", "0 */6 * * *", func() {
		CheckUserStripeAccount(app)
	})
}
