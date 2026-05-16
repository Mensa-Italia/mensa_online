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
		office := matchOffice(officesByRegion, regionName)
		if office == nil {
			// Auto-crea il local_office mancante. name = region: l'admin
			// potra` rinominarlo in seguito senza rompere il match (la chiave
			// di matching e` il campo region, non name).
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

// mergeRegionsByStaff analizza i risultati dello scraping e raggruppa le
// regioni che hanno esattamente lo stesso set di referenti (stesse persone
// con stessi ruoli). Per ogni gruppo di dimensione > 1 produce un nome
// combinato del tipo "Region1 e Region2" (e per N>2, "Region1 e Region2 e
// Region3"), assicurando lo stesso ordinamento alfabetico.
//
// Ritorna una mappa code (01..20) → nome effettivo del gruppo locale da
// usare per matching/creazione del relativo local_office.
//
// Le regioni con scrape fallito o senza referenti restano col loro nome
// originale (non vengono mai fuse).
func mergeRegionsByStaff(results []regionResult) map[string]string {
	out := make(map[string]string, len(results))
	identity := make(map[string]string, len(results)) // code -> hash
	groups := make(map[string][]string)               // hash -> []code

	for _, r := range results {
		if r.err != nil {
			out[r.code] = regionsByCode[r.code]
			continue
		}
		h := computeStaffIdentity(r.people)
		if h == "" {
			// regione vuota o senza alias risolti: tieni il nome originale,
			// non mergiamo a caso.
			out[r.code] = regionsByCode[r.code]
			continue
		}
		identity[r.code] = h
		groups[h] = append(groups[h], r.code)
	}

	for hash, codes := range groups {
		if len(codes) == 1 {
			out[codes[0]] = regionsByCode[codes[0]]
			continue
		}
		// Fusione: nome combinato in ordine alfabetico stabile.
		names := make([]string, 0, len(codes))
		for _, c := range codes {
			names = append(names, regionsByCode[c])
		}
		sort.Strings(names)
		merged := strings.Join(names, " e ")
		for _, c := range codes {
			out[c] = merged
		}
		_ = hash
	}
	return out
}

// computeStaffIdentity calcola un hash deterministico del set di referenti
// di una regione: chiave = sorted({alias|role}). Cosi` due regioni con
// gli stessi N segretari + assistenti producono lo stesso hash.
func computeStaffIdentity(people []PersonRef) string {
	if len(people) == 0 {
		return ""
	}
	keys := make([]string, 0, len(people))
	for _, p := range people {
		alias := strings.ToLower(strings.TrimSpace(p.MensaAlias))
		if alias == "" {
			continue
		}
		keys = append(keys, alias+"|"+p.Role)
	}
	if len(keys) == 0 {
		return ""
	}
	sort.Strings(keys)
	return strings.Join(keys, ";")
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

