package localofficesync

import (
	"strings"
	"sync"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Run scrappa /gruppi-locali-referenti/ per ogni regione e riconcilia le
// tabelle local_offices_admins (segretari + co-segretari) e
// local_offices_test_assistants (assistenti al test).
//
// Pattern:
//   1. carica i local_offices e indicizza per nome regione (lowercase)
//   2. per ogni regione 01..20, scrappa la pagina e ricava PersonRef
//   3. per ogni PersonRef, verifica che il corrispondente users record
//      esista (matching su id == cloud32 uid) e fa upsert nel link giusto
//   4. dopo aver visto tutte le pagine, cancella i record che non sono
//      piu` presenti sulla sorgente (slincing)
//
// Idempotente, sicuro da rilanciare. Se il sito e` giu` o una regione fallisce,
// le altre proseguono.
func Run(app core.App) {
	app.Logger().Info("[localofficesync] start")

	officesByRegion, err := loadOfficesByRegion(app)
	if err != nil {
		app.Logger().Error("[localofficesync] caricamento local_offices fallito", "err", err)
		return
	}

	adminsCol, err := app.FindCollectionByNameOrId("local_offices_admins")
	if err != nil {
		app.Logger().Error("[localofficesync] collection local_offices_admins non trovata", "err", err)
		return
	}
	assistantsCol, err := app.FindCollectionByNameOrId("local_offices_test_assistants")
	if err != nil {
		app.Logger().Error("[localofficesync] collection local_offices_test_assistants non trovata", "err", err)
		return
	}

	// Set degli id (office+user) visti durante questa esecuzione, usati per
	// la fase di reconcile / unlink. Chiave: "{officeId}|{userId}".
	seenAdmins := map[string]struct{}{}
	seenAssistants := map[string]struct{}{}

	// Scrape in parallelo: le 20 regioni sono indipendenti, ognuna e` una
	// chiamata HTTP. mensaitalia.it regge tranquillamente, riduciamo wallclock.
	type regionResult struct {
		code   string
		people []PersonRef
		err    error
	}
	var wg sync.WaitGroup
	results := make([]regionResult, 0, 20)
	var mu sync.Mutex
	for _, code := range allSquadraCodes() {
		code := code
		wg.Add(1)
		go func() {
			defer wg.Done()
			people, err := FetchRegion(code)
			mu.Lock()
			results = append(results, regionResult{code: code, people: people, err: err})
			mu.Unlock()
		}()
	}
	wg.Wait()

	totalLinked := 0
	for _, r := range results {
		if r.err != nil {
			app.Logger().Error("[localofficesync] fetch regione fallito", "squadra", r.code, "err", r.err)
			continue
		}
		regionName := regionsByCode[r.code]
		office := matchOffice(officesByRegion, regionName)
		if office == nil {
			app.Logger().Warn("[localofficesync] nessun local_office per regione", "region", regionName)
			continue
		}
		for _, p := range r.people {
			// Verifica che la persona sia registrata come user (auth) — se
			// non lo e`, non possiamo linkare (FK su _pb_users_auth_).
			if _, err := app.FindRecordById("users", p.UserID); err != nil {
				continue
			}
			key := office.Id + "|" + p.UserID
			switch p.Role {
			case "segretario", "cosegretario":
				isOfficer := p.Role == "segretario"
				if err := upsertAdmin(app, adminsCol, office.Id, p.UserID, isOfficer); err != nil {
					app.Logger().Error("[localofficesync] upsert admin fallito",
						"office", office.Id, "user", p.UserID, "err", err)
					continue
				}
				seenAdmins[key] = struct{}{}
				totalLinked++
			case "assistente":
				if err := upsertAssistant(app, assistantsCol, office.Id, p.UserID); err != nil {
					app.Logger().Error("[localofficesync] upsert assistente fallito",
						"office", office.Id, "user", p.UserID, "err", err)
					continue
				}
				seenAssistants[key] = struct{}{}
				totalLinked++
			}
		}
	}

	// Reconcile: cancella i record che non sono piu` sulla sorgente.
	removedAdmins := reconcile(app, adminsCol, seenAdmins)
	removedAssistants := reconcile(app, assistantsCol, seenAssistants)

	app.Logger().Info("[localofficesync] done",
		"linked", totalLinked,
		"removed_admins", removedAdmins,
		"removed_assistants", removedAssistants,
	)
}

func loadOfficesByRegion(app core.App) (map[string]*core.Record, error) {
	recs, err := app.FindAllRecords("local_offices")
	if err != nil {
		return nil, err
	}
	out := make(map[string]*core.Record, len(recs))
	for _, r := range recs {
		key := strings.ToLower(strings.TrimSpace(r.GetString("region")))
		if key != "" {
			out[key] = r
		}
	}
	return out, nil
}

// matchOffice trova il local_office che corrisponde a una regione. Prima
// prova match esatto (lowercase), poi tenta varianti senza apostrofi/spazi
// per essere robusto a piccole discrepanze di formato.
func matchOffice(byRegion map[string]*core.Record, regionName string) *core.Record {
	if regionName == "" {
		return nil
	}
	key := strings.ToLower(regionName)
	if r, ok := byRegion[key]; ok {
		return r
	}
	// Variante "Val d'Aosta" -> "val daosta" / "valle d'aosta" / "valle daosta"
	normalize := func(s string) string {
		s = strings.ToLower(s)
		s = strings.NewReplacer("'", "", "`", "", "  ", " ").Replace(s)
		return strings.TrimSpace(s)
	}
	target := normalize(regionName)
	// "Val d'Aosta" è il nome del sito, ma in db potrebbe essere "Valle d'Aosta".
	candidates := []string{target}
	if strings.HasPrefix(target, "val ") {
		candidates = append(candidates, "valle "+target[4:])
	}
	for k, r := range byRegion {
		nk := normalize(k)
		for _, c := range candidates {
			if nk == c {
				return r
			}
		}
	}
	return nil
}

func upsertAdmin(app core.App, col *core.Collection, officeID, userID string, isOfficer bool) error {
	existing, err := app.FindFirstRecordByFilter(col,
		"local_office = {:o} && user = {:u}",
		dbx.Params{"o": officeID, "u": userID},
	)
	rec := existing
	if err != nil || rec == nil {
		rec = core.NewRecord(col)
		rec.Set("local_office", officeID)
		rec.Set("user", userID)
	}
	// is_the_officer puo` cambiare nel tempo (un cosegretario che diventa
	// segretario o viceversa): aggiorniamo sempre.
	if rec.GetBool("is_the_officer") != isOfficer || existing == nil {
		rec.Set("is_the_officer", isOfficer)
		return app.Save(rec)
	}
	return nil
}

func upsertAssistant(app core.App, col *core.Collection, officeID, userID string) error {
	existing, err := app.FindFirstRecordByFilter(col,
		"local_office = {:o} && user = {:u}",
		dbx.Params{"o": officeID, "u": userID},
	)
	if err == nil && existing != nil {
		return nil // gia` linkato, niente da fare
	}
	rec := core.NewRecord(col)
	rec.Set("local_office", officeID)
	rec.Set("user", userID)
	return app.Save(rec)
}

// reconcile elimina dalla tabella i record (local_office, user) che non
// compaiono nel set "seen" raccolto durante lo scrape. Ritorna il numero
// di record cancellati.
func reconcile(app core.App, col *core.Collection, seen map[string]struct{}) int {
	all, err := app.FindAllRecords(col)
	if err != nil {
		app.Logger().Error("[localofficesync] reconcile FindAll fallito",
			"collection", col.Name, "err", err)
		return 0
	}
	removed := 0
	for _, rec := range all {
		key := rec.GetString("local_office") + "|" + rec.GetString("user")
		if _, ok := seen[key]; ok {
			continue
		}
		if err := app.Delete(rec); err != nil {
			app.Logger().Error("[localofficesync] reconcile delete fallito",
				"collection", col.Name, "id", rec.Id, "err", err)
			continue
		}
		removed++
	}
	if removed > 0 {
		app.Logger().Info("[localofficesync] reconcile",
			"collection", col.Name, "removed", removed)
	}
	return removed
}

