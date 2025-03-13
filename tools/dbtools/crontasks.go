package dbtools

import (
	"fmt"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/tools/cron"
	"mensadb/importers"
	"mensadb/tolgee"
	"mensadb/tools/env"
)

func CronTasks(app *pocketbase.PocketBase) {
	scheduler := cron.New()

	scheduler.MustAdd("RemoteUpdateAddons", "1 3 * * *", func() {
		go RemoteUpdateAddons(app)
		go func() {
			// Ottengo dalla mail list tutti le mail alias del Mensa Italia. Le uso successivamente per assegnare i poteri
			importers.GetFullMailList()
			// Aggiorno i poteri di tutti gli utenti in base alla lista dei segretari
			RefreshUserStatesManagersPowers(app)
			app.Logger().Info(
				fmt.Sprintf("[CRON] Updated the powers of all the users based on the segretari list"),
			)
		}()
		go tolgee.Load(env.GetPasswordSalt())
	})
	scheduler.MustAdd("updateDocumentsData", "0 8,11,14,17,20 * * *", func() {
		go RemoteRetrieveDocumentsFromArea32(app)
	})
	scheduler.Start()
	app.Logger().Info(
		"[CRON] Scheduled all crons jobs",
	)
}
