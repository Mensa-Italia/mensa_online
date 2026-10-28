package exapp

import (
	"sort"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// stampImageBase e` la base degli URL dei file: la stessa forma che usano le
// anteprime social dei link (main/links/stamp.go) e la ricerca
// (main/api/search/hydrate.go). `stamp` e` in FILE_LINK_PUBLIC_COLLECTIONS,
// quindi quell'URL e` scaricabile senza autenticazione (307 verso un link S3
// firmato): l'app esterna puo` usarlo cosi' com'e`.
const stampImageBase = "https://svc.mensa.it/api/files/"

// buildStampsExport ritorna i timbri dell'utente, dal piu' vecchio al piu'
// recente. obtained_at e` il momento in cui il timbro e` stato collezionato
// (created di stamp_users, UTC).
func buildStampsExport(app core.App, userID string, now time.Time) (map[string]any, error) {
	records, err := app.FindAllRecords("stamp_users", dbx.NewExp("user = {:user}", dbx.Params{"user": userID}))
	if err != nil {
		return nil, err
	}
	sort.SliceStable(records, func(i, j int) bool {
		return records[i].GetDateTime("created").Time().Before(records[j].GetDateTime("created").Time())
	})
	// Una sola query per tutti i timbri collegati. Gli errori di expand (un
	// timbro cancellato) non fermano l'export: quella riga esce senza dettagli.
	app.ExpandRecords(records, []string{"stamp"}, nil)

	stamps := make([]map[string]any, 0, len(records))
	for _, r := range records {
		item := map[string]any{
			"stamp_id":    r.GetString("stamp"),
			"description": "",
			"image_url":   "",
			"obtained_at": r.GetDateTime("created").Time().UTC().Format(time.RFC3339),
		}
		if stamp := r.ExpandedOne("stamp"); stamp != nil {
			item["description"] = stamp.GetString("description")
			if image := stamp.GetString("image"); image != "" {
				item["image_url"] = stampImageBase + stamp.BaseFilesPath() + "/" + image
			}
		}
		stamps = append(stamps, item)
	}

	return map[string]any{
		"member_id":   userID,
		"scope":       scopeStamps,
		"exported_at": now.UTC().Format(time.RFC3339),
		"stamps":      stamps,
	}, nil
}

// buildMedalsExport ritorna la riga di view_medals del socio. La view espone
// SOLO contatori (stamps_total, stamps_night, ...): le medaglie vere sono
// calcolate dal client (regole MedalRules in Kotlin) a partire da questi, e
// qui non sono riprodotte. Un socio senza riga ha un oggetto vuoto.
func buildMedalsExport(app core.App, userID string, now time.Time) (map[string]any, error) {
	medals := map[string]any{}
	rec, err := app.FindRecordById("view_medals", userID)
	if err == nil && rec != nil {
		medals = rec.PublicExport()
	}
	return map[string]any{
		"member_id":   userID,
		"scope":       scopeMedals,
		"exported_at": now.UTC().Format(time.RFC3339),
		"medals":      medals,
	}, nil
}
