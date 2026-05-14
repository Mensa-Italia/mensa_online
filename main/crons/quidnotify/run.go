package quidnotify

import (
	"strconv"

	"github.com/pocketbase/pocketbase/core"

	"mensadb/tools/dbtools"
	"mensadb/tools/quidsync"
)

const configKey = "quid_last_issue_number"

// Run controlla se su quid.mensa.it e` uscito un nuovo numero e in tal caso:
//   1. sincronizza gli articoli del numero corrente nella collection
//      `quid_articles` (l'hook li riflette in Bleve);
//   2. manda una push a tutti i soci se il numero e` maggiore di quello
//      memorizzato in `configs[quid_last_issue_number]`.
//
// Al primo run (config vuoto) e` un bootstrap silenzioso: sincronizza ma NON
// invia notifiche, e salva il numero corrente per i confronti futuri.
func Run(app core.App) {
	issues, err := quidsync.FetchIssueCategories()
	if err != nil {
		app.Logger().Error("[CRON] quidnotify: fetch categories", "err", err)
		return
	}

	// Prima categoria con almeno un post pubblicato = numero corrente.
	var latest *quidsync.IssueCategory
	for i := range issues {
		if issues[i].Count > 0 {
			latest = &issues[i]
			break
		}
	}
	if latest == nil {
		app.Logger().Info("[CRON] quidnotify: nessun numero con post pubblicati, skip")
		return
	}

	// Sync articoli del numero corrente: copre articoli aggiunti in ritardo
	// e edit del contenuto. Lo storico (numeri vecchi) si gestisce con la CLI
	// quid-backfill: si presume immutabile.
	count, err := quidsync.SyncIssue(app, latest.ID, latest.Name)
	if err != nil {
		app.Logger().Error("[CRON] quidnotify: sync issue corrente fallito",
			"issue", latest.Number, "err", err)
		// proseguo: la notifica e` indipendente dall'esito del sync
	} else {
		app.Logger().Info("[CRON] quidnotify: sync issue corrente ok",
			"issue", latest.Number, "articles", count)
	}

	stored := dbtools.GetInternalConfig(app, configKey)
	if stored == "" {
		if err := dbtools.SetInternalConfig(app, configKey, strconv.Itoa(latest.Number)); err != nil {
			app.Logger().Error("[CRON] quidnotify: bootstrap save fallito", "err", err)
			return
		}
		app.Logger().Info("[CRON] quidnotify: bootstrap completato, nessuna notifica inviata",
			"issue", latest.Number, "name", latest.Name)
		return
	}

	storedNum, err := strconv.Atoi(stored)
	if err != nil {
		app.Logger().Error("[CRON] quidnotify: config corrotto, ignoro", "value", stored, "err", err)
		return
	}

	if latest.Number <= storedNum {
		app.Logger().Info("[CRON] quidnotify: nessun nuovo numero", "latest", latest.Number, "stored", storedNum)
		return
	}

	app.Logger().Info("[CRON] quidnotify: nuovo numero rilevato, invio notifiche",
		"issue", latest.Number, "name", latest.Name, "id", latest.ID)

	dbtools.SendPushNotificationToAllUsers(app, dbtools.PushNotification{
		TrTag: "push_notification.new_quid_issue",
		TrNamedParams: map[string]string{
			"name": latest.Name,
		},
		Data: map[string]string{
			"type":    "quid",
			"quid_id": strconv.Itoa(latest.ID),
		},
	})

	if err := dbtools.SetInternalConfig(app, configKey, strconv.Itoa(latest.Number)); err != nil {
		app.Logger().Error("[CRON] quidnotify: aggiornamento config fallito (notifiche gia` inviate)", "err", err)
	}
}
