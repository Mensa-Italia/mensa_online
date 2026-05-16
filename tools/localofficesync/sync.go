package localofficesync

import (
	"sort"
	"strings"
	"sync"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// fetchAliasConcurrency limita le chiamate concorrenti a cloud32 per estrarre
// l'email @mensa.it dalla scheda socio. cloud32 e` lento ma stabile; 4 in
// parallelo bilancia throughput e gentilezza.
const fetchAliasConcurrency = 4

// regionResult e` l'esito dello scraping di una singola regione.
type regionResult struct {
	code   string
	people []PersonRef
	err    error
}

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

	// Risolvi gli alias @mensa.it per TUTTE le regioni prima di iniziare il
	// processing: ci servono per calcolare l'identita` di ciascuna regione
	// (set di referenti) e detectare le fusioni di gruppi locali con stesso
	// staff.
	for i := range results {
		if results[i].err == nil {
			results[i].people = resolveAliases(results[i].people)
		}
	}

	// Mappa codice squadra → nome effettivo del gruppo locale, fondendo
	// regioni con lo stesso identico set di referenti (es. "Friuli Venezia
	// Giulia e Veneto" quando due regioni condividono segreteria).
	codeToName := mergeRegionsByStaff(results)

	totalLinked := 0
	for _, r := range results {
		if r.err != nil {
			app.Logger().Error("[localofficesync] fetch regione fallito", "squadra", r.code, "err", r.err)
			continue
		}
		regionName := codeToName[r.code]
		if regionName == "" {
			regionName = regionsByCode[r.code]
		}

		// Step 1: cerca un office gia` rinominato col nome merged.
		office := matchOffice(officesByRegion, regionName)

		// Step 2: se merged-name non esiste ma esiste un office matchato
		// sul nome originale di QUESTA regione, rinominalo al merged.
		// Cosi` consolidiamo offici creati pre-merge senza dover cancellare
		// manualmente (es. "Piemonte" diventa "Piemonte e Valle d'Aosta").
		if office == nil && regionName != regionsByCode[r.code] {
			if legacy := matchOffice(officesByRegion, regionsByCode[r.code]); legacy != nil {
				legacy.Set("name", regionName)
				legacy.Set("region", regionName)
				if err := app.Save(legacy); err == nil {
					app.Logger().Info("[localofficesync] consolidato local_office esistente",
						"from", regionsByCode[r.code], "to", regionName, "id", legacy.Id)
					// aggiorna in-memory index
					delete(officesByRegion, strings.ToLower(regionsByCode[r.code]))
					officesByRegion[strings.ToLower(regionName)] = legacy
					office = legacy
				} else {
					app.Logger().Error("[localofficesync] consolidamento fallito",
						"id", legacy.Id, "err", err)
				}
			}
		}

		if office == nil {
			// Auto-crea il local_office mancante. name = region: l'admin
			// potra` rinominarlo in seguito senza rompere il match.
			created, err := createOffice(app, regionName)
			if err != nil {
				app.Logger().Error("[localofficesync] creazione local_office fallita",
					"region", regionName, "err", err)
				continue
			}
			app.Logger().Info("[localofficesync] creato local_office mancante",
				"region", regionName, "id", created.Id)
			officesByRegion[strings.ToLower(regionName)] = created
			office = created
		}

		for _, p := range r.people {
			if p.MensaAlias == "" {
				continue
			}
			memberID := findMemberByAlias(app, p.MensaAlias)
			if memberID == "" {
				app.Logger().Warn("[localofficesync] nessun match alias_mail",
					"alias", p.MensaAlias, "region", regionName)
				continue
			}
			key := office.Id + "|" + memberID
			switch p.Role {
			case "segretario", "cosegretario":
				isOfficer := p.Role == "segretario"
				if err := upsertAdmin(app, adminsCol, office.Id, memberID, isOfficer); err != nil {
					app.Logger().Error("[localofficesync] upsert admin fallito",
						"office", office.Id, "member", memberID, "err", err)
					continue
				}
				seenAdmins[key] = struct{}{}
				totalLinked++
			case "assistente":
				if err := upsertAssistant(app, assistantsCol, office.Id, memberID); err != nil {
					app.Logger().Error("[localofficesync] upsert assistente fallito",
						"office", office.Id, "member", memberID, "err", err)
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

	// Cleanup: rimuovi local_office residui con nome di regione ora "assorbita"
	// in un cluster merged e senza nessun referente. Es: dopo aver consolidato
	// "Piemonte" -> "Piemonte e Valle d'Aosta", l'office "Valle d'Aosta" rimane
	// con 0 admins/0 assistants → orfano, da eliminare.
	absorbedRegions := map[string]struct{}{}
	for code, name := range codeToName {
		original := regionsByCode[code]
		if name != original {
			absorbedRegions[strings.ToLower(original)] = struct{}{}
		}
	}
	removedOffices := cleanupOrphanOffices(app, absorbedRegions)

	app.Logger().Info("[localofficesync] done",
		"linked", totalLinked,
		"removed_admins", removedAdmins,
		"removed_assistants", removedAssistants,
		"removed_offices", removedOffices,
	)
}

// cleanupOrphanOffices cancella i local_office con region/name che matchano
// uno degli `absorbed` (regioni che sono state fuse in un nome merged) E
// che non hanno admins ne` test_assistants attivi.
func cleanupOrphanOffices(app core.App, absorbed map[string]struct{}) int {
	if len(absorbed) == 0 {
		return 0
	}
	offices, err := app.FindAllRecords("local_offices")
	if err != nil {
		app.Logger().Error("[localofficesync] cleanup FindAll offices fallito", "err", err)
		return 0
	}
	removed := 0
	for _, o := range offices {
		region := strings.ToLower(strings.TrimSpace(o.GetString("region")))
		if _, ok := absorbed[region]; !ok {
			continue
		}
		// Verifica orfano: nessun admin, nessun assistant.
		admins, _ := app.FindRecordsByFilter("local_offices_admins",
			"local_office = '"+o.Id+"'", "", 1, 0, nil)
		if len(admins) > 0 {
			continue
		}
		assistants, _ := app.FindRecordsByFilter("local_offices_test_assistants",
			"local_office = '"+o.Id+"'", "", 1, 0, nil)
		if len(assistants) > 0 {
			continue
		}
		if err := app.Delete(o); err != nil {
			app.Logger().Error("[localofficesync] cleanup delete office fallito",
				"id", o.Id, "region", region, "err", err)
			continue
		}
		app.Logger().Info("[localofficesync] eliminato local_office orfano (assorbito da merge)",
			"id", o.Id, "region", region)
		removed++
	}
	return removed
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

// resolveAliases fetcha la scheda cloud32 di ogni persona in parallelo per
// estrarre l'email @mensa.it. Ritorna la stessa slice con MensaAlias
// valorizzato (o vuoto se non trovata).
func resolveAliases(people []PersonRef) []PersonRef {
	if len(people) == 0 {
		return people
	}
	out := make([]PersonRef, len(people))
	copy(out, people)
	var wg sync.WaitGroup
	sem := make(chan struct{}, fetchAliasConcurrency)
	for i := range out {
		i := i
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			alias, _ := FetchAlias(out[i].SocioCode, out[i].RoleCode, out[i].SquadraCode)
			out[i].MensaAlias = alias
		}()
	}
	wg.Wait()
	return out
}

// findMemberByAlias cerca in members_registry per alias_mail (con fallback
// su original_mail). Ritorna l'id del socio o "" se non trovato.
//
// Volutamente non richiede l'esistenza di un users record: dopo lo swap di
// FK in 1779001900, local_offices_admins.user / test_assistants.user
// puntano a members_registry, quindi possiamo linkare anche referenti che
// non hanno mai installato l'app.
func findMemberByAlias(app core.App, alias string) string {
	lower := strings.ToLower(strings.TrimSpace(alias))
	if lower == "" {
		return ""
	}
	// Primario: alias_mail (l'indirizzo @mensa.it ufficiale).
	rec, err := app.FindFirstRecordByFilter("members_registry",
		"alias_mail = {:m}",
		dbx.Params{"m": lower},
	)
	if err != nil || rec == nil {
		// Fallback: original_mail.
		rec, err = app.FindFirstRecordByFilter("members_registry",
			"original_mail = {:m}",
			dbx.Params{"m": lower},
		)
		if err != nil || rec == nil {
			return ""
		}
	}
	return rec.Id
}

func createOffice(app core.App, regionName string) (*core.Record, error) {
	col, err := app.FindCollectionByNameOrId("local_offices")
	if err != nil {
		return nil, err
	}
	rec := core.NewRecord(col)
	rec.Set("name", regionName)
	rec.Set("region", regionName)
	if err := app.Save(rec); err != nil {
		return nil, err
	}
	return rec, nil
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

// mergeRegionsByStaff raggruppa le regioni in base a un'unica chiave: il
// SocioCode del segretario. Stesso segretario → stesso gruppo locale.
//
// Robusto, deterministico, non dipende dalla seconda fetch cloud32 (gli alias
// @mensa.it possono mancare se la scheda socio fa timeout). Non dipende neanche
// dall'ordinamento degli assistenti sulla pagina (che la condividono o no).
//
// Ritorna una mappa code → nome effettivo del gruppo locale. Per le regioni
// con segretario condiviso, il nome diventa "Region1 e Region2" in ordine
// alfabetico stabile.
//
// Regioni senza segretario rilevato (errore di scrape, pagina vuota) restano
// col loro nome originale.
func mergeRegionsByStaff(results []regionResult) map[string]string {
	// 1. Per ogni regione individua il SocioCode del segretario (s_ruolo=001).
	segByCode := make(map[string]string, len(results)) // code -> segretario socio
	for _, r := range results {
		if r.err != nil {
			continue
		}
		for _, p := range r.people {
			if p.Role == "segretario" {
				segByCode[r.code] = p.SocioCode
				break
			}
		}
	}

	// 2. Raggruppa per segretario condiviso.
	groups := make(map[string][]string) // segretarioSocio -> []code
	for code, seg := range segByCode {
		if seg == "" {
			continue
		}
		groups[seg] = append(groups[seg], code)
	}

	// 3. Per ogni gruppo crea il nome finale (singolo o "X e Y").
	out := make(map[string]string, len(results))
	for _, r := range results {
		out[r.code] = regionsByCode[r.code] // default: nome originale
	}
	for _, codes := range groups {
		if len(codes) == 1 {
			continue // singleton: nome originale gia` valorizzato
		}
		sort.Strings(codes)
		names := make([]string, 0, len(codes))
		for _, c := range codes {
			names = append(names, regionsByCode[c])
		}
		sort.Strings(names)
		merged := strings.Join(names, " e ")
		for _, c := range codes {
			out[c] = merged
		}
	}
	return out
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

