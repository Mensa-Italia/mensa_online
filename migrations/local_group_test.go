package migrations

import (
	"testing"
	"time"

	"mensadb/tools/memberchanges"
	"mensadb/tools/officestats"

	"github.com/pocketbase/pocketbase/core"
)

// Il 20 novembre 2026 a mezzogiorno: stagione 2026/27, mesi ottobre e
// novembre.
var localGroupNow = time.Date(2026, time.November, 20, 12, 0, 0, 0, time.UTC)

// at e' in season_test.go.

// Schema e regole: region_key riempita dalla migrazione, local_offices_admins
// chiusa in scrittura, le tre collection nuove con le loro regole.
func TestLocalGroupSchema(t *testing.T) {
	f := &medalFixture{t: t, app: newMigratedApp(t)}
	f.save("local_offices", "loPU", map[string]any{"name": "Puglia", "region": "Basilicata e Puglia", "slug": "puglia"})
	f.save("local_offices", "loLO", map[string]any{"name": "Lombardia", "region": "Lombardia", "slug": "lombardia"})

	// La seconda up riempie le sedi che ci sono e non rompe niente.
	if err := upLocalGroup(f.app); err != nil {
		t.Fatalf("seconda up: %v", err)
	}
	for id, want := range map[string]string{"loPU": "Puglia", "loLO": "Lombardia"} {
		r, err := f.app.FindRecordById("local_offices", id)
		if err != nil {
			t.Fatal(err)
		}
		if got := r.GetString("region_key"); got != want {
			t.Errorf("%s.region_key = %q, atteso %q", id, got, want)
		}
	}
	offices, _ := f.app.FindCollectionByNameOrId("local_offices")
	if offices.UpdateRule == nil || !containsText(*offices.UpdateRule, "@request.body.region_key:isset = false") {
		t.Errorf("local_offices.updateRule: %v", offices.UpdateRule)
	}

	admins, err := f.app.FindCollectionByNameOrId("local_offices_admins")
	if err != nil {
		t.Fatal(err)
	}
	if admins.CreateRule != nil || admins.DeleteRule != nil {
		t.Errorf("local_offices_admins: create=%v delete=%v, attese solo superuser", admins.CreateRule, admins.DeleteRule)
	}
	if admins.ListRule == nil || *admins.ListRule != `@request.auth.id != ""` {
		t.Errorf("local_offices_admins.listRule cambiata: %v", admins.ListRule)
	}

	for name, rule := range map[string]string{
		memberchanges.Table:      memberchanges.AdminReadRule,
		officestats.Table:        `@request.auth.id != ""`,
		officestats.MonthlyTable: memberchanges.AdminReadRule,
	} {
		c, err := f.app.FindCollectionByNameOrId(name)
		if err != nil {
			t.Fatal(err)
		}
		if c.ListRule == nil || *c.ListRule != rule || c.ViewRule == nil || *c.ViewRule != rule {
			t.Errorf("%s: regole di lettura %v %v", name, c.ListRule, c.ViewRule)
		}
		if c.CreateRule != nil || c.UpdateRule != nil || c.DeleteRule != nil {
			t.Errorf("%s: scrittura solo superuser", name)
		}
	}
	changes, _ := f.app.FindCollectionByNameOrId(memberchanges.Table)
	for _, name := range []string{"member", "name", "region_key", "local_office", "kind", "day", "created", "updated"} {
		if changes.Fields.GetByName(name) == nil {
			t.Errorf("%s.%s mancante", memberchanges.Table, name)
		}
	}
	if len(changes.Indexes) != 2 {
		t.Errorf("%s: indici %v", memberchanges.Table, changes.Indexes)
	}
}

func containsText(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestLocalOfficeStats(t *testing.T) {
	f := &medalFixture{t: t, app: newMigratedApp(t)}
	app := f.app

	f.save("local_offices", "loPU", map[string]any{"name": "Puglia", "region": "Basilicata e Puglia", "slug": "puglia"})
	f.save("local_offices", "loLA", map[string]any{"name": "Lazio", "region": "Lazio", "slug": "lazio"})
	f.save("positions", "pPU", map[string]any{"state": "Puglia"})
	f.save("positions", "pBA", map[string]any{"state": "Basilicata"})
	f.save("positions", "pLA", map[string]any{"state": "Lazio"})

	// Regione Puglia: tre attivi (uno in Basilicata, accorpata), uno no.
	f.member("4001", "Basilicata", true)
	f.member("4005", "Puglia", true)
	f.member("4006", "Puglia", true)
	f.member("4002", "Puglia", false)
	f.member("5001", "Lazio", true)

	// Evento in regione per indirizzo, nella stagione: due timbratori validi,
	// piu' l'organizzatore e 5366 che non contano.
	f.event("e1", "9999", "pBA", false, at(2026, time.November, 5))
	f.stamp("e1", "4001")
	f.stamp("e1", "5001")
	f.stamp("e1", "9999")
	f.stamp("e1", "5366")
	// Evento futuro della sede, senza indirizzo.
	f.save("events", "e2", map[string]any{"name": "Evento e2", "owner": "9999", "local_office": "loPU", "when_start": at(2026, time.December, 10)})
	// Evento della stagione scorsa e uno in un'altra regione: fuori.
	f.event("e3", "9999", "pPU", false, at(2026, time.September, 15))
	f.event("e4", "9999", "pLA", false, at(2026, time.November, 6))
	f.stamp("e4", "5001")

	// Convenzioni: una attiva in regione, una scaduta, una non locale.
	f.save("deals", "d1", map[string]any{"name": "D1", "position": "pPU", "is_local": true, "is_active": true})
	f.save("deals", "d2", map[string]any{"name": "D2", "position": "pPU", "is_local": true, "is_active": true, "ending": at(2026, time.November, 1)})
	f.save("deals", "d3", map[string]any{"name": "D3", "position": "pPU", "is_local": false, "is_active": true})
	f.save("deals", "d4", map[string]any{"name": "D4", "position": "pBA", "is_local": true, "is_active": true, "ending": at(2027, time.January, 1)})

	// Movimenti della sede.
	record := func(member, kind string, d time.Time) {
		t.Helper()
		if err := memberchanges.Record(app, []memberchanges.Change{{Member: member, Name: "Socio " + member, Kind: kind, RegionKey: "Puglia"}}, d); err != nil {
			t.Fatal(err)
		}
	}
	record("4006", memberchanges.Joined, time.Date(2026, time.November, 18, 9, 0, 0, 0, time.UTC))
	record("4005", memberchanges.MovedIn, time.Date(2026, time.November, 1, 9, 0, 0, 0, time.UTC))
	record("4002", memberchanges.Left, time.Date(2026, time.October, 25, 9, 0, 0, 0, time.UTC))
	record("4007", memberchanges.MovedOut, time.Date(2026, time.October, 5, 9, 0, 0, 0, time.UTC))

	if err := officestats.RefreshAt(app, localGroupNow); err != nil {
		t.Fatal(err)
	}

	stats, err := app.FindRecordById(officestats.Table, "loPU")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{
		"members_active":    3,
		"members_new_7d":    1,
		"members_new_30d":   2,
		"members_left_30d":  1,
		"events_season":     1,
		"events_upcoming":   1,
		"attendance_season": 2,
		"deals_active":      2,
	}
	for field, n := range want {
		if got := stats.GetInt(field); got != n {
			t.Errorf("%s = %d, atteso %d", field, got, n)
		}
	}
	if stats.GetString("season") != "2026/27" || stats.GetString("region_key") != "Puglia" || stats.GetString("local_office") != "loPU" {
		t.Errorf("riga: %v", stats.PublicExport())
	}
	if stats.GetDateTime("computed_at").IsZero() {
		t.Error("computed_at vuoto")
	}

	monthly, err := app.FindRecordsByFilter(officestats.MonthlyTable, "local_office = 'loPU'", "month", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(monthly) != 2 || monthly[0].Id != "loPU_2026-10" || monthly[1].Id != "loPU_2026-11" {
		t.Fatalf("mesi: %v", ids(monthly))
	}
	check := func(r *core.Record, values map[string]int) {
		t.Helper()
		for field, n := range values {
			if got := r.GetInt(field); got != n {
				t.Errorf("%s.%s = %d, atteso %d", r.Id, field, got, n)
			}
		}
	}
	check(monthly[0], map[string]int{"events": 0, "attendance": 0, "distinct_attendees": 0, "members_new": 0, "members_left": 2, "members_active": 1})
	check(monthly[1], map[string]int{"events": 1, "attendance": 2, "distinct_attendees": 2, "members_new": 2, "members_left": 0, "members_active": 3})

	// Il Lazio: un evento, un timbratore, nessun movimento.
	lazio, err := app.FindRecordById(officestats.Table, "loLA")
	if err != nil {
		t.Fatal(err)
	}
	if lazio.GetInt("events_season") != 1 || lazio.GetInt("attendance_season") != 1 || lazio.GetInt("members_active") != 1 {
		t.Errorf("Lazio: %v", lazio.PublicExport())
	}

	// Nella stagione dopo i mesi vecchi spariscono.
	if err := officestats.RefreshAt(app, time.Date(2027, time.October, 2, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	monthly, _ = app.FindRecordsByFilter(officestats.MonthlyTable, "local_office = 'loPU'", "month", 0, 0)
	if len(monthly) != 1 || monthly[0].Id != "loPU_2027-10" {
		t.Errorf("mesi della stagione nuova: %v", ids(monthly))
	}
}

func ids(records []*core.Record) []string {
	out := make([]string, len(records))
	for i, r := range records {
		out[i] = r.Id
	}
	return out
}
