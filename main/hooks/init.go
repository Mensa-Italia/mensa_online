package hooks

import (
	"log"
	"mensadb/tools/aitools"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
)

func Load(app core.App) {

	app.OnRecordAfterUpdateSuccess("users").BindFunc(LogUserChart)
	app.OnRecordAfterCreateSuccess("addons").BindFunc(GeneratePublicPrivateKeys)
	app.OnRecordCreate("positions").BindFunc(PositionSetState)
	app.OnRecordCreate("ex_keys").BindFunc(OnKeyCreated)
	app.OnRecordAfterCreateSuccess("calendar_link").BindFunc(CalendarSetHash)

	// Notify users when an event is created
	app.OnRecordAfterCreateSuccess("events").BindFunc(EventsNotifyUsersAsync)
	app.OnRecordAfterUpdateSuccess("events").BindFunc(EventsUpdateNotifyUsersAsync)

	// Notify users when a deal is created or updated
	app.OnRecordAfterCreateSuccess("deals").BindFunc(DealsNotifyUsersAsync)
	app.OnRecordAfterUpdateSuccess("deals").BindFunc(DealsUpdateNotifyUsersAsync)

	app.OnRecordAfterUpdateSuccess("stamp").BindFunc(StampUpdateImageAsync)

	// search index — live updates
	app.OnRecordAfterCreateSuccess("events").BindFunc(indexEventAsync)
	app.OnRecordAfterUpdateSuccess("events").BindFunc(indexEventAsync)
	app.OnRecordAfterDeleteSuccess("events").BindFunc(unindexAsync)

	app.OnRecordAfterCreateSuccess("sigs").BindFunc(indexSigAsync)
	app.OnRecordAfterUpdateSuccess("sigs").BindFunc(indexSigAsync)
	app.OnRecordAfterDeleteSuccess("sigs").BindFunc(unindexAsync)

	app.OnRecordAfterCreateSuccess("deals").BindFunc(indexDealAsync)
	app.OnRecordAfterUpdateSuccess("deals").BindFunc(indexDealAsync)
	app.OnRecordAfterDeleteSuccess("deals").BindFunc(unindexAsync)

	app.OnRecordAfterCreateSuccess("documents").BindFunc(indexDocumentAsync)
	app.OnRecordAfterUpdateSuccess("documents").BindFunc(indexDocumentAsync)
	app.OnRecordAfterDeleteSuccess("documents").BindFunc(unindexAsync)

	// Soci indicizzati: members_registry (sync Area32), NON users.
	// users e` la collection di auth PocketBase e contiene anche record
	// "ombra" / non-soci. Inoltre l'hook su members_registry esclude i
	// record con is_active=false dall'indice.
	app.OnRecordAfterCreateSuccess("members_registry").BindFunc(indexMemberAsync)
	app.OnRecordAfterUpdateSuccess("members_registry").BindFunc(indexMemberAsync)
	app.OnRecordAfterDeleteSuccess("members_registry").BindFunc(unindexAsync)

	// Org chart: indicizza ogni membro come "org_role" e ogni gruppo come
	// "org_group" in Bleve.
	app.OnRecordAfterCreateSuccess("org_chart_members").BindFunc(indexOrgRoleAsync)
	app.OnRecordAfterUpdateSuccess("org_chart_members").BindFunc(indexOrgRoleAsync)
	app.OnRecordAfterDeleteSuccess("org_chart_members").BindFunc(unindexOrgRoleAsync)

	app.OnRecordAfterCreateSuccess("org_chart_groups").BindFunc(indexOrgGroupAsync)
	app.OnRecordAfterUpdateSuccess("org_chart_groups").BindFunc(indexOrgGroupAsync)
	app.OnRecordAfterDeleteSuccess("org_chart_groups").BindFunc(unindexAsync)

	// quid_articles: cache locale degli articoli WordPress sincronizzati dal
	// cron quidsync. Hook li riflette nell'indice Bleve.
	app.OnRecordAfterCreateSuccess("quid_articles").BindFunc(indexQuidArticleAsync)
	app.OnRecordAfterUpdateSuccess("quid_articles").BindFunc(indexQuidArticleAsync)
	app.OnRecordAfterDeleteSuccess("quid_articles").BindFunc(unindexAsync)

	// quid_issues: cache locale dei numeri Quid (categorie WP). Indicizzati
	// come risultato di search distinto dai singoli articoli.
	app.OnRecordAfterCreateSuccess("quid_issues").BindFunc(indexQuidIssueAsync)
	app.OnRecordAfterUpdateSuccess("quid_issues").BindFunc(indexQuidIssueAsync)
	app.OnRecordAfterDeleteSuccess("quid_issues").BindFunc(unindexAsync)
}

func StampUpdateImageAsync(e *core.RecordEvent) error {
	record := e.Record

	go func(e *core.RecordEvent) {
		if strings.Contains(record.GetString("description"), "[UPDATE]") {
			descriptionToUse := strings.TrimSpace(strings.ReplaceAll(record.GetString("description"), "[UPDATE]", ""))
			makeItRed := false

			records, _ := e.App.FindRecordsByFilter("events", "name ~ {:name}", "-created", 1, 0, dbx.Params{"name": descriptionToUse})
			if len(records) > 0 {
				eventRecord := records[0]
				descriptionToUse = eventRecord.GetString("name") + "\n\n\n" + eventRecord.GetString("description")
				makeItRed = eventRecord.GetBool("is_national")
			}
			// Generazione dell'immagine del timbro
			geminiImage, err := aitools.GenerateStamp(descriptionToUse, makeItRed)
			if err != nil {
				// Log dell'errore nella generazione dello stamp
				log.Printf("Errore nella generazione dello stamp: %v", err)
				return
			}
			fileImage, err := filesystem.NewFileFromBytes(geminiImage, "stamp.png")
			if err != nil {
				log.Printf("Errore nella creazione del file immagine: %v", err)
				return
			}
			record.Set("image", fileImage)

			record.Set("description", strings.TrimSpace(strings.ReplaceAll(record.GetString("description"), "[UPDATE]", "")))

			if err := e.App.Save(record); err != nil {
				e.App.Logger().Error("save record failed", "collection", record.Collection().Name, "id", record.Id, "err", err)
			}

		}
	}(e)

	return e.Next()
}
