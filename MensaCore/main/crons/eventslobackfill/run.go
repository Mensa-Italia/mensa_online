package eventslobackfill

import (
	"github.com/pocketbase/pocketbase/core"

	"mensadb/tools/dbtools"
)

// Run scorre tutti gli eventi senza local_office assegnato e prova a
// risolverlo dal loro owner. Pensato per essere lanciato manualmente dal
// pannello PB ("Run" sul cron) dopo aver popolato la mappa local_offices,
// per recuperare lo storico precedente all'introduzione del campo.
//
// Idempotente: gli eventi che hanno gia` un local_office o quelli il cui
// owner non e` linkato a nessun gruppo vengono saltati.
func Run(app core.App) {
	app.Logger().Info("[CRON] events local_office backfill start")

	// Filtro sugli eventi che non hanno ancora local_office. Usiamo perPage
	// alto e teniamo il filtro sul DB: e` un'operazione "una tantum" e gli
	// eventi senza office sono tipicamente migliaia, non milioni.
	records, err := app.FindRecordsByFilter("events", "local_office = ''", "", -1, 0)
	if err != nil {
		app.Logger().Error("[CRON] backfill: FindRecordsByFilter fallito", "err", err)
		return
	}

	assigned := 0
	for _, rec := range records {
		ownerID := rec.GetString("owner")
		if ownerID == "" {
			continue
		}
		officeID := dbtools.ResolveUserLocalOffice(app, ownerID)
		if officeID == "" {
			continue
		}
		rec.Set("local_office", officeID)
		if err := app.Save(rec); err != nil {
			app.Logger().Error("[CRON] backfill: save fallito",
				"event", rec.Id, "owner", ownerID, "err", err)
			continue
		}
		assigned++
	}

	app.Logger().Info("[CRON] events local_office backfill done",
		"scanned", len(records), "assigned", assigned, "unmatched", len(records)-assigned)
}
