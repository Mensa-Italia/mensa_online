package migrations

import (
	"slices"
	"strings"
	"testing"
	"time"

	"mensadb/tools/leaderboard"
	"mensadb/tools/medals"
	"mensadb/tools/regionmedals"
	"mensadb/tools/rollover"
	"mensadb/tools/season"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

// Stagioni dei test. Gli eventi stanno nel passato di qualunque giorno in
// cui giri il test (la query delle regioni scarta gli eventi nel futuro
// dell'orologio vero): la stagione "viva" e' la 2025, da ottobre 2025 a
// settembre 2026, e la precedente la 2024.
const (
	liveSeason = 2025
	oldSeason  = 2024
)

var (
	inLive = time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC) // dentro la 2025
	inNext = time.Date(2026, time.October, 1, 10, 0, 0, 0, time.UTC)
)

func init() {
	if season.Of(inLive) != liveSeason || season.Of(inNext) != liveSeason+1 || season.Of(at(2025, time.February, 10).Time()) != oldSeason {
		panic("le date dei test non sono nelle stagioni dichiarate")
	}
}

func at(y int, m time.Month, d int) types.DateTime {
	dt, _ := types.ParseDateTime(time.Date(y, m, d, 10, 0, 0, 0, time.UTC))
	return dt
}

// seasonFixture e' una piccola Italia con due stagioni di timbri: la regione
// A (sei soci attivi nel registro), due eventi, uno della 2024 e uno della
// 2025, e questi soci:
//
//	1001  timbro 2025, due dispositivi e iscritto da anni (medaglie permanenti)
//	1002  timbro 2025, niente altro
//	1006  timbro 2025 e timbro 2024
//	1003  solo timbro 2024
//	1004  nessun timbro, due dispositivi
//	1005  timbro 2025 ma ha scelto di non apparire
//	1007  segretario della sede A, nessun timbro
func newSeasonFixture(t *testing.T) *medalFixture {
	f := &medalFixture{t: t, app: newMigratedApp(t)}

	f.save("local_offices", "loA", map[string]any{"name": "Sede A", "region": "Regione A", "slug": "a"})
	f.save("local_offices", "loB", map[string]any{"name": "Sede B", "region": "Regione B", "slug": "b"})
	f.save("positions", "pA", map[string]any{"state": "Regione A"})
	f.save("positions", "pB", map[string]any{"state": "Regione B"})
	for _, id := range []string{"1001", "1002", "1003", "1004", "1005", "1006"} {
		f.member(id, "Regione A", true)
	}
	f.member("2001", "Regione B", true)
	f.member("2002", "Regione B", true)

	addUser(t, f.app, "1001", true, time.Now().AddDate(-4, 0, 0))
	for _, id := range []string{"1002", "1003", "1004", "1005", "1006", "1007"} {
		addUser(t, f.app, id, true, time.Now().AddDate(-1, 0, 0))
	}
	addDevice(t, f.app, "1001", "tok-1a")
	addDevice(t, f.app, "1001", "tok-1b")
	addDevice(t, f.app, "1004", "tok-4a")
	addDevice(t, f.app, "1004", "tok-4b")
	f.save("users_metadata", "", map[string]any{"user": "1005", "key": "leaderboard_hidden", "value": "true"})
	f.save("local_offices_admins", "", map[string]any{"local_office": "loA", "user": "1007", "is_the_officer": true})

	// Il timbro esiste da qualche giorno prima dell'evento, cosi' non e' di
	// "prima fila" (non c'entra con le medaglie che il test guarda).
	f.save("events", "evOld", map[string]any{"name": "Cena vecchia", "owner": "9001", "position": "pA", "when_start": at(2025, time.February, 10)})
	f.save("stamp", "sOld", map[string]any{"description": "Cena vecchia", "created": at(2025, time.January, 1)})
	f.save("events", "evNew", map[string]any{"name": "Cena nuova", "owner": "9001", "position": "pA", "when_start": at(2026, time.January, 10)})
	f.save("stamp", "sNew", map[string]any{"description": "Cena nuova", "created": at(2025, time.December, 1)})
	f.save("stamp_users", "", map[string]any{"stamp": "sOld", "user": "1006", "created": at(2025, time.February, 10)})
	f.save("stamp_users", "", map[string]any{"stamp": "sOld", "user": "1003", "created": at(2025, time.February, 10)})
	for _, u := range []string{"1001", "1002", "1005", "1006"} {
		f.save("stamp_users", "", map[string]any{"stamp": "sNew", "user": u, "created": at(2026, time.January, 10)})
	}
	return f
}

func boardRow(t *testing.T, app core.App, table, id string) *core.Record {
	t.Helper()
	r, err := app.FindRecordById(table, id)
	if err != nil {
		t.Fatalf("%s/%s: %v", table, id, err)
	}
	return r
}

func hasRow(app core.App, table, id string) bool {
	_, err := app.FindRecordById(table, id)
	return err == nil
}

func refreshAll(t *testing.T, app core.App, now time.Time) {
	t.Helper()
	if err := medals.RefreshAt(app, now); err != nil {
		t.Fatalf("medals: %v", err)
	}
	if err := leaderboard.RefreshAt(app, now); err != nil {
		t.Fatalf("leaderboard: %v", err)
	}
}

func count(t *testing.T, app core.App, query string, args dbx.Params) int {
	t.Helper()
	var n int
	if err := app.DB().NewQuery(query).Bind(args).Row(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

// Lo schema della stagione: il campo season dove serve, l'albo d'oro, le
// annate delle medaglie, lo stato con la stagione nell'indice, le medaglie di
// regione con season al posto di year. Rifare la migrazione non rompe niente.
func TestSeasonSchema(t *testing.T) {
	app := newMigratedApp(t)

	for _, name := range []string{leaderboard.MembersTable, leaderboard.RegionsTable, leaderboard.SecretariesTable, medals.Table, medals.StateTable, regionmedals.Table} {
		c, err := app.FindCollectionByNameOrId(name)
		if err != nil {
			t.Fatal(err)
		}
		f, ok := c.Fields.GetByName("season").(*core.NumberField)
		if !ok || !f.OnlyInt {
			t.Errorf("%s.season: %+v", name, c.Fields.GetByName("season"))
		}
	}
	region, _ := app.FindCollectionByNameOrId(regionmedals.Table)
	if region.Fields.GetByName("year") == nil || region.Fields.GetByName("season") == nil {
		t.Error("le medaglie di regione tengono year (alias per l'app 22.4.0) accanto a season")
	}
	for _, idx := range region.Indexes {
		if strings.Contains(idx, "year") {
			t.Errorf("indice con year: %s", idx)
		}
	}
	var mismatched int
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM ` + regionmedals.Table + ` WHERE year != season`).Row(&mismatched); err != nil {
		t.Fatal(err)
	}
	if mismatched != 0 {
		t.Errorf("%d righe con year diverso da season", mismatched)
	}

	// Albo d'oro.
	albo, err := app.FindCollectionByNameOrId(leaderboard.SeasonsTable)
	if err != nil {
		t.Fatal(err)
	}
	wantRule := `@request.auth.id != "" && (position <= 10 || subject = @request.auth.id)`
	if albo.ListRule == nil || *albo.ListRule != wantRule || albo.ViewRule == nil || *albo.ViewRule != wantRule {
		t.Errorf("regole dell'albo: %v / %v", albo.ListRule, albo.ViewRule)
	}
	if albo.CreateRule != nil || albo.UpdateRule != nil || albo.DeleteRule != nil {
		t.Error("l'albo si scrive solo dal server")
	}
	board, ok := albo.Fields.GetByName("board").(*core.SelectField)
	if !ok || !slices.Equal(board.Values, []string{"members", "regions", "secretaries"}) {
		t.Errorf("board: %+v", albo.Fields.GetByName("board"))
	}
	for _, name := range []string{"season", "subject", "name", "position", "score", "region_key", "local_office"} {
		if albo.Fields.GetByName(name) == nil {
			t.Errorf("albo: campo %s mancante", name)
		}
	}
	if !hasUnique(albo, "board", "season", "subject") {
		t.Errorf("albo: indice unico (board, season, subject) mancante: %v", albo.Indexes)
	}

	// Annate delle medaglie.
	history, err := app.FindCollectionByNameOrId(medals.HistoryTable)
	if err != nil {
		t.Fatal(err)
	}
	if history.ListRule == nil || *history.ListRule != `user = @request.auth.id` || history.ViewRule == nil || *history.ViewRule != `user = @request.auth.id` {
		t.Errorf("regole delle annate: %v / %v", history.ListRule, history.ViewRule)
	}
	tier, ok := history.Fields.GetByName("tier").(*core.SelectField)
	if !ok || !slices.Equal(tier.Values, []string{"none", "bronze", "silver", "gold", "platinum"}) {
		t.Errorf("tier: %+v", history.Fields.GetByName("tier"))
	}
	for _, name := range []string{"user", "season", "medal", "tier", "value"} {
		if history.Fields.GetByName(name) == nil {
			t.Errorf("annate: campo %s mancante", name)
		}
	}
	if !hasUnique(history, "user", "season", "medal") {
		t.Errorf("annate: indice unico (user, season, medal) mancante: %v", history.Indexes)
	}

	// Stato delle notifiche: l'indice unico ha la stagione e il vecchio non c'e'.
	state, _ := app.FindCollectionByNameOrId(medals.StateTable)
	if !hasUnique(state, "user", "medal", "season") {
		t.Errorf("stato: indice unico (user, medal, season) mancante: %v", state.Indexes)
	}
	for _, idx := range state.Indexes {
		if strings.Contains(idx, "UNIQUE") && !strings.Contains(idx, "season") {
			t.Errorf("stato: indice unico senza stagione: %s", idx)
		}
	}

	if err := upSeason(app); err != nil {
		t.Fatalf("seconda up: %v", err)
	}
}

func hasUnique(c *core.Collection, columns ...string) bool {
	for _, idx := range c.Indexes {
		if !strings.Contains(idx, "UNIQUE") {
			continue
		}
		all := true
		for _, col := range columns {
			if !strings.Contains(idx, col) {
				all = false
			}
		}
		if all {
			return true
		}
	}
	return false
}

// In classifica compare solo chi ha un timbro valido nella stagione; i timbri
// fuori stagione non contano (nemmeno a pari merito), le medaglie permanenti
// non danno punti, chi ha scelto di non apparire resta fuori, a pari punti
// pari posizione. Alla stagione dopo, senza timbri, la classifica e' vuota.
func TestSeasonLeaderboardWindowAndPermanentMedals(t *testing.T) {
	f := newSeasonFixture(t)
	refreshAll(t, f.app, inLive)

	for _, id := range []string{"1001", "1002", "1006"} {
		if !hasRow(f.app, leaderboard.MembersTable, id) {
			t.Errorf("%s ha un timbro della stagione: deve esserci", id)
		}
	}
	for id, why := range map[string]string{
		"1003": "ha solo un timbro della stagione scorsa",
		"1004": "non ha timbri, solo medaglie permanenti",
		"1005": "ha scelto di non apparire",
		"1007": "non ha timbri",
	} {
		if hasRow(f.app, leaderboard.MembersTable, id) {
			t.Errorf("%s non deve esserci: %s", id, why)
		}
	}

	a, b, c := boardRow(t, f.app, leaderboard.MembersTable, "1001"), boardRow(t, f.app, leaderboard.MembersTable, "1002"), boardRow(t, f.app, leaderboard.MembersTable, "1006")
	// Collezionista, Costante e Pioniere a bronzo: 3 + 3 + 3. Socio attivo,
	// Della prima ora e Multischermo di 1001 non danno niente.
	for _, r := range []*core.Record{a, b, c} {
		if r.GetFloat("medal_points") != 9 {
			t.Errorf("%s: punti medaglie %v, attesi 9 (solo le stagionali)", r.Id, r.GetFloat("medal_points"))
		}
		if r.GetFloat("stamps_total") != 1 {
			t.Errorf("%s: timbri %v, atteso 1", r.Id, r.GetFloat("stamps_total"))
		}
		if r.GetFloat("season") != liveSeason {
			t.Errorf("%s: season %v", r.Id, r.GetFloat("season"))
		}
		if r.GetFloat("position") != 1 {
			t.Errorf("%s: posizione %v, i tre sono a pari punti", r.Id, r.GetFloat("position"))
		}
	}
	if a.GetFloat("score") != b.GetFloat("score") || b.GetFloat("score") != c.GetFloat("score") || a.GetFloat("score") <= 9 {
		t.Errorf("punteggi: %v %v %v", a.GetFloat("score"), b.GetFloat("score"), c.GetFloat("score"))
	}
	// Il timbro della stagione scorsa di 1006 non da' punti timbri.
	if c.GetFloat("stamps_points") != b.GetFloat("stamps_points") {
		t.Errorf("il timbro fuori stagione conta: %v contro %v", c.GetFloat("stamps_points"), b.GetFloat("stamps_points"))
	}

	// Il medagliere ha la stagione e conta solo i timbri della stagione.
	m := boardRow(t, f.app, medals.Table, "1006")
	if m.GetFloat("season") != liveSeason || m.GetInt("stamps_total") != 1 {
		t.Errorf("view_medals 1006: season=%v timbri=%v", m.GetFloat("season"), m.GetInt("stamps_total"))
	}
	if boardRow(t, f.app, medals.Table, "1001").GetInt("devices") != 2 {
		t.Error("i dispositivi sono un contatore permanente")
	}
	if r := boardRow(t, f.app, leaderboard.RegionsTable, "loA"); r.GetFloat("season") != liveSeason || r.GetFloat("score") <= 0 {
		t.Errorf("regione A: season=%v punti=%v", r.GetFloat("season"), r.GetFloat("score"))
	}
	if r := boardRow(t, f.app, leaderboard.SecretariesTable, "1007"); r.GetFloat("season") != liveSeason || r.GetFloat("position") != 1 {
		t.Errorf("segretario: season=%v posizione=%v", r.GetFloat("season"), r.GetFloat("position"))
	}

	// La stagione dopo (da ottobre) nessuno ha timbri: classifica dei soci
	// vuota, medagliere azzerato, sede senza punti.
	refreshAll(t, f.app, inNext)
	if n, _ := f.app.CountRecords(leaderboard.MembersTable); n != 0 {
		t.Errorf("a stagione nuova senza timbri la classifica dei soci ha %d righe", n)
	}
	if m := boardRow(t, f.app, medals.Table, "1006"); m.GetInt("stamps_total") != 0 || m.GetFloat("season") != liveSeason+1 {
		t.Errorf("view_medals 1006 a stagione nuova: season=%v timbri=%v", m.GetFloat("season"), m.GetInt("stamps_total"))
	}
	if boardRow(t, f.app, medals.Table, "1001").GetInt("devices") != 2 {
		t.Error("i dispositivi restano anche a stagione nuova")
	}
	if r := boardRow(t, f.app, leaderboard.RegionsTable, "loA"); r.GetFloat("season") != liveSeason+1 || r.GetFloat("score") != 0 {
		t.Errorf("regione A a stagione nuova: season=%v punti=%v", r.GetFloat("season"), r.GetFloat("score"))
	}
}

// Finalize rifa' il conto con la finestra di una stagione chiusa, anche se le
// tabelle vive sono gia' di un'altra, e scrive l'albo: soci con timbri,
// tutte le sedi (anche senza punti), segretari. Due volte da' lo stesso
// risultato, e non lascia niente di temporaneo.
func TestFinalizeWritesRollOfHonour(t *testing.T) {
	f := newSeasonFixture(t)
	refreshAll(t, f.app, inNext) // le tabelle vive sono della stagione 2026

	if err := leaderboard.Finalize(f.app, liveSeason); err != nil {
		t.Fatal(err)
	}
	if err := leaderboard.Finalize(f.app, liveSeason); err != nil {
		t.Fatalf("seconda finalizzazione: %v", err)
	}

	rows := func(board string, s int) map[string]*core.Record {
		list, err := f.app.FindRecordsByFilter(leaderboard.SeasonsTable, "board = {:b} && season = {:s}", "position", 0, 0, dbx.Params{"b": board, "s": s})
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]*core.Record{}
		for _, r := range list {
			out[r.GetString("subject")] = r
		}
		return out
	}

	members := rows("members", liveSeason)
	if len(members) != 3 || members["1001"] == nil || members["1002"] == nil || members["1006"] == nil {
		t.Fatalf("soci dell'albo 2025: %v", keys(members))
	}
	for id, r := range members {
		if r.GetFloat("position") != 1 || r.GetFloat("score") <= 9 || r.GetString("name") == "" && id == "" {
			t.Errorf("%s: posizione=%v punti=%v", id, r.GetFloat("position"), r.GetFloat("score"))
		}
	}
	// La stagione 2024 non e' stata finalizzata: niente righe.
	if len(rows("members", oldSeason)) != 0 {
		t.Error("righe di una stagione mai finalizzata")
	}

	regions := rows("regions", liveSeason)
	if len(regions) != 2 {
		t.Fatalf("regioni dell'albo: %v, attese tutte e due le sedi", keys(regions))
	}
	if a := regions["loA"]; a.GetFloat("position") != 1 || a.GetFloat("score") <= 0 || a.GetString("region_key") != "Regione A" || a.GetString("local_office") != "loA" {
		t.Errorf("sede A: %v", a.PublicExport())
	}
	if b := regions["loB"]; b.GetFloat("position") != 2 || b.GetFloat("score") != 0 || b.GetString("region_key") != "Regione B" || b.GetString("local_office") != "loB" {
		t.Errorf("sede B (senza punti ma presente): %v", b.PublicExport())
	}
	secretaries := rows("secretaries", liveSeason)
	if s := secretaries["1007"]; len(secretaries) != 1 || s.GetFloat("position") != 1 || s.GetString("local_office") != "loA" || s.GetString("region_key") != "" {
		t.Errorf("segretari dell'albo: %v", keys(secretaries))
	}

	// Per la stagione 2024 i conti sono altri: solo 1003 e 1006 hanno un
	// timbro in quella finestra.
	if err := leaderboard.Finalize(f.app, oldSeason); err != nil {
		t.Fatal(err)
	}
	old := rows("members", oldSeason)
	if len(old) != 2 || old["1003"] == nil || old["1006"] == nil || old["1001"] != nil {
		t.Errorf("soci dell'albo 2024: %v", keys(old))
	}
	if len(rows("members", liveSeason)) != 3 {
		t.Error("finalizzare la 2024 ha toccato la 2025")
	}

	// Le tabelle vive sono rimaste quelle della stagione nuova, e i
	// ricalcoli dopo la finalizzazione vanno.
	if n, _ := f.app.CountRecords(leaderboard.MembersTable); n != 0 {
		t.Errorf("la classifica viva e' stata toccata: %d righe", n)
	}
	if boardRow(t, f.app, medals.Table, "1006").GetFloat("season") != liveSeason+1 {
		t.Error("view_medals e' stata sostituita dalla tabella temporanea")
	}
	if n := count(t, f.app, `SELECT COUNT(*) FROM sqlite_temp_master WHERE name IN ({:a}, {:b})`,
		dbx.Params{"a": medals.Table, "b": leaderboard.RegionsTable}); n != 0 {
		t.Errorf("%d tabelle temporanee rimaste", n)
	}
	refreshAll(t, f.app, inNext)
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// FinalizeMedals scrive per ogni socio con un timbro valido nella stagione una
// riga per ogni medaglia stagionale, anche "none", con il valore del
// contatore; niente per le permanenti, niente per chi non ha timbri.
func TestFinalizeMedalsWritesHistory(t *testing.T) {
	f := newSeasonFixture(t)
	refreshAll(t, f.app, inNext)

	if err := medals.FinalizeMedals(f.app, liveSeason); err != nil {
		t.Fatal(err)
	}
	if err := medals.FinalizeMedals(f.app, liveSeason); err != nil {
		t.Fatalf("seconda volta: %v", err)
	}
	seasonal := 0
	for _, r := range medals.Rules {
		if r.Family == medals.Stagionale {
			seasonal++
		}
	}
	// 1001, 1002, 1005 (che si e' nascosto dalla classifica ma ha il suo
	// medagliere) e 1006.
	users := count(t, f.app, `SELECT COUNT(DISTINCT user) FROM `+medals.HistoryTable+` WHERE season = {:s}`, dbx.Params{"s": liveSeason})
	rows := count(t, f.app, `SELECT COUNT(*) FROM `+medals.HistoryTable+` WHERE season = {:s}`, dbx.Params{"s": liveSeason})
	if users != 4 || rows != 4*seasonal {
		t.Errorf("annata 2025: %d soci e %d righe, attesi 4 e %d", users, rows, 4*seasonal)
	}

	row := func(user, medal string, s int) *core.Record {
		r, err := f.app.FindFirstRecordByFilter(medals.HistoryTable, "user = {:u} && medal = {:m} && season = {:s}", dbx.Params{"u": user, "m": medal, "s": s})
		if err != nil {
			t.Fatalf("%s/%s/%d: %v", user, medal, s, err)
		}
		return r
	}
	if r := row("1006", "collector", liveSeason); r.GetString("tier") != "bronze" || r.GetFloat("value") != 1 {
		t.Errorf("collezionista 2025: %v", r.PublicExport())
	}
	if r := row("1006", "marathon", liveSeason); r.GetString("tier") != "none" || r.GetFloat("value") != 1 {
		t.Errorf("maratoneta 2025 (non ottenuta): %v", r.PublicExport())
	}
	if r := row("1001", "night_owl", liveSeason); r.GetString("tier") != "none" || r.GetFloat("value") != 0 {
		t.Errorf("nottambulo 2025: %v", r.PublicExport())
	}
	if _, err := f.app.FindFirstRecordByFilter(medals.HistoryTable, "user = '1001' && medal = 'multiscreen'"); err == nil {
		t.Error("le permanenti non hanno annate")
	}
	if _, err := f.app.FindFirstRecordByFilter(medals.HistoryTable, "user = '1004'"); err == nil {
		t.Error("chi non ha timbri nella stagione non ha annata")
	}
	if _, err := f.app.FindFirstRecordByFilter(medals.HistoryTable, "user = '1003' && season = {:s}", dbx.Params{"s": liveSeason}); err == nil {
		t.Error("1003 ha timbrato solo nella stagione scorsa")
	}

	// La 2024 e' un'altra finestra: 1003 e 1006 la hanno, 1001 no.
	if err := medals.FinalizeMedals(f.app, oldSeason); err != nil {
		t.Fatal(err)
	}
	if r := row("1003", "collector", oldSeason); r.GetString("tier") != "bronze" {
		t.Errorf("collezionista 2024 di 1003: %v", r.PublicExport())
	}
	if n := count(t, f.app, `SELECT COUNT(*) FROM `+medals.HistoryTable+` WHERE season = {:s} AND user = '1001'`, dbx.Params{"s": oldSeason}); n != 0 {
		t.Errorf("1001 nella 2024: %d righe", n)
	}
	if n := count(t, f.app, `SELECT COUNT(*) FROM `+medals.HistoryTable+` WHERE season = {:s}`, dbx.Params{"s": liveSeason}); n != rows {
		t.Errorf("la 2024 ha toccato la 2025: %d righe contro %d", n, rows)
	}
}

// Stato in una stagione, ricalcolo in quella dopo: al cambio il giro del cron
// archivia la chiusa (albo, annate, Podio) e salva la nuova; i ricalcoli della
// stagione nuova seminano lo stato in silenzio, nessun avanzamento (quindi
// nessuna notifica) anche se i contatori sono ripartiti da zero; le
// permanenti restano a stagione 0 senza duplicarsi.
func TestSeasonRollover(t *testing.T) {
	f := newSeasonFixture(t)

	var got []medals.Change
	original := medals.AfterRefresh
	medals.AfterRefresh = func(_ core.App, changes []medals.Change) { got = append(got, changes...) }
	t.Cleanup(func() { medals.AfterRefresh = original })

	// Durante la stagione 2025: ricalcoli, medaglie di regione e stagione salvata.
	refreshAll(t, f.app, inLive)
	if _, err := regionmedals.RefreshSeasonAt(f.app, liveSeason, inLive); err != nil {
		t.Fatal(err)
	}
	if err := rollover.Store(f.app, liveSeason); err != nil {
		t.Fatal(err)
	}
	got = nil
	if count(t, f.app, `SELECT COUNT(*) FROM `+medals.StateTable+` WHERE season = {:s}`, dbx.Params{"s": liveSeason}) == 0 {
		t.Fatal("niente stato per la stagione 2025")
	}
	permanent := count(t, f.app, `SELECT COUNT(*) FROM `+medals.StateTable+` WHERE season = 0`, nil)
	if permanent != 7*5 {
		t.Errorf("%d righe di stato permanenti, attese %d (7 soci x 5 permanenti)", permanent, 7*5)
	}

	// Ancora la stessa stagione: il giro non fa niente.
	unlocks, err := rollover.Run(f.app, inLive.Add(time.Hour))
	if err != nil || len(unlocks) != 0 {
		t.Fatalf("giro nella stessa stagione: %v %+v", err, unlocks)
	}
	if n, _ := f.app.CountRecords(leaderboard.SeasonsTable); n != 0 {
		t.Fatalf("l'albo si scrive solo al cambio: %d righe", n)
	}

	// Il 1 ottobre.
	unlocks, err = rollover.Run(f.app, inNext)
	if err != nil {
		t.Fatal(err)
	}
	if s, ok := rollover.Stored(f.app); !ok || s != liveSeason+1 {
		t.Errorf("stagione salvata: %d (%v)", s, ok)
	}
	if n := count(t, f.app, `SELECT COUNT(*) FROM `+leaderboard.SeasonsTable+` WHERE season = {:s} AND board = 'members'`, dbx.Params{"s": liveSeason}); n != 3 {
		t.Errorf("albo dei soci 2025: %d righe, attese 3", n)
	}
	if n := count(t, f.app, `SELECT COUNT(DISTINCT user) FROM `+medals.HistoryTable+` WHERE season = {:s}`, dbx.Params{"s": liveSeason}); n != 4 {
		t.Errorf("annate delle medaglie 2025: %d soci, attesi 4", n)
	}
	// Podio: la sede A era prima, e lo sblocco torna a chi deve avvisare.
	podium := f.row("loA", "podium", liveSeason)
	if podium.GetString("tier") != "gold" || podium.GetFloat("value") != 1 {
		t.Errorf("podio 2025 della sede A: tier=%q value=%v", podium.GetString("tier"), podium.GetFloat("value"))
	}
	wantUnlocks := []regionmedals.Unlock{
		{LocalOffice: "loA", RegionKey: "Regione A", Medal: "podium", Tier: "gold", Season: liveSeason},
		{LocalOffice: "loB", RegionKey: "Regione B", Medal: "podium", Tier: "silver", Season: liveSeason},
	}
	if len(unlocks) != 2 || !slices.Contains(unlocks, wantUnlocks[0]) || !slices.Contains(unlocks, wantUnlocks[1]) {
		t.Errorf("sblocchi del cambio: %+v", unlocks)
	}

	// I ricalcoli della stagione nuova: in silenzio.
	got = nil
	refreshAll(t, f.app, inNext)
	if len(got) != 0 {
		t.Fatalf("cambio di stagione: %d avanzamenti, nessuno doveva partire: %+v", len(got), got)
	}
	seasonalRules := 0
	for _, r := range medals.Rules {
		if r.Family == medals.Stagionale {
			seasonalRules++
		}
	}
	if n := count(t, f.app, `SELECT COUNT(*) FROM `+medals.StateTable+` WHERE season = {:s}`, dbx.Params{"s": liveSeason + 1}); n != 7*seasonalRules {
		t.Errorf("stato della stagione nuova: %d righe, attese %d", n, 7*seasonalRules)
	}
	if n := count(t, f.app, `SELECT COUNT(*) FROM `+medals.StateTable+` WHERE season = 0`, nil); n != permanent {
		t.Errorf("le permanenti sono %d righe, erano %d", n, permanent)
	}
	if count(t, f.app, `SELECT COUNT(*) FROM `+medals.StateTable+` WHERE season = {:s}`, dbx.Params{"s": liveSeason}) == 0 {
		t.Error("lo stato della stagione chiusa deve restare")
	}
	stateAt := func(user, medal string, s int) string {
		r, err := f.app.FindFirstRecordByFilter(medals.StateTable, "user = {:u} && medal = {:m} && season = {:s}", dbx.Params{"u": user, "m": medal, "s": s})
		if err != nil {
			t.Fatalf("stato %s/%s/%d: %v", user, medal, s, err)
		}
		return r.GetString("tier")
	}
	if got := stateAt("1006", "collector", liveSeason+1); got != "none" {
		t.Errorf("collezionista di 1006 nella stagione nuova: %q", got)
	}
	if got := stateAt("1006", "collector", liveSeason); got != "bronze" {
		t.Errorf("collezionista di 1006 nella stagione chiusa: %q", got)
	}

	// Poi la stagione nuova va avanti: un timbro nuovo e' un avanzamento vero.
	f.save("events", "evNow", map[string]any{"name": "Cena ottobre", "owner": "9001", "position": "pA", "when_start": at(2026, time.October, 1)})
	f.save("stamp", "sNow", map[string]any{"description": "Cena ottobre", "created": at(2026, time.September, 1)})
	f.save("stamp_users", "", map[string]any{"stamp": "sNow", "user": "1002", "created": at(2026, time.October, 1)})
	refreshAll(t, f.app, inNext.Add(time.Hour))
	found := false
	for _, c := range got {
		if c.UserID == "1002" && c.Medal == "collector" && c.From == medals.TierNone && c.To == medals.TierBronze {
			found = true
		}
	}
	if !found {
		t.Errorf("manca l'avanzamento di 1002 nella stagione nuova: %+v", got)
	}

	// Un secondo giro nella stessa stagione non rifa' niente.
	if unlocks, err := rollover.Run(f.app, inNext.Add(2*time.Hour)); err != nil || len(unlocks) != 0 {
		t.Errorf("secondo giro: %v %+v", err, unlocks)
	}
}

// Senza stagione salvata il primo giro archivia la precedente a quella in
// corso; indietro di due stagioni le archivia tutte e due.
func TestRolloverCatchesUp(t *testing.T) {
	f := newSeasonFixture(t)
	refreshAll(t, f.app, inLive)

	if _, ok := rollover.Stored(f.app); ok {
		// la migrazione ha gia' salvato la stagione in corso di oggi: la tolgo
		if err := rollover.Store(f.app, 0); err != nil {
			t.Fatal(err)
		}
		if _, err := f.app.DB().NewQuery(`DELETE FROM configs WHERE key = {:k}`).Bind(dbx.Params{"k": rollover.ConfigKey}).Execute(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := rollover.Run(f.app, inNext); err != nil {
		t.Fatal(err)
	}
	if n := count(t, f.app, `SELECT COUNT(*) FROM `+leaderboard.SeasonsTable+` WHERE season = {:s}`, dbx.Params{"s": liveSeason}); n == 0 {
		t.Error("manca l'albo della stagione precedente")
	}

	if err := rollover.Store(f.app, oldSeason); err != nil {
		t.Fatal(err)
	}
	if _, err := rollover.Run(f.app, inNext); err != nil {
		t.Fatal(err)
	}
	for _, s := range []int{oldSeason, liveSeason} {
		if n := count(t, f.app, `SELECT COUNT(*) FROM `+medals.HistoryTable+` WHERE season = {:s}`, dbx.Params{"s": s}); n == 0 {
			t.Errorf("manca l'annata %d", s)
		}
	}
	if s, _ := rollover.Stored(f.app); s != liveSeason+1 {
		t.Errorf("stagione salvata %d", s)
	}
}

// Il Podio di una stagione chiusa si assegna anche senza lo snapshot del
// 30 settembre, dall'albo d'oro.
func TestRegionMedalsPodiumFromRollOfHonour(t *testing.T) {
	f := newSeasonFixture(t)
	refreshAll(t, f.app, inLive)
	if _, err := regionmedals.RefreshSeasonAt(f.app, liveSeason, inLive); err != nil {
		t.Fatal(err)
	}
	if got := f.row("loA", "podium", liveSeason).GetString("tier"); got != "none" {
		t.Fatalf("senza snapshot ne' albo: %q", got)
	}
	if err := leaderboard.Finalize(f.app, liveSeason); err != nil {
		t.Fatal(err)
	}
	unlocks, err := regionmedals.SettlePodium(f.app, liveSeason)
	if err != nil {
		t.Fatal(err)
	}
	if len(unlocks) != 2 {
		t.Errorf("sblocchi: %+v", unlocks)
	}
	if got := f.row("loA", "podium", liveSeason).GetString("tier"); got != "gold" {
		t.Errorf("sede A: %q", got)
	}
	if got := f.row("loB", "podium", liveSeason).GetString("tier"); got != "silver" {
		t.Errorf("sede B: %q", got)
	}
}

// Un database com'era prima della stagione: stato delle medaglie senza
// season con i metalli a vita, medaglie di regione con year. La migrazione
// porta tutto alla stagione: lo stato delle stagionali riparte dai contatori
// della stagione, le permanenti vanno a stagione 0, le medaglie di regione
// vecchie restano con la loro stagione, la 2025 e quella in corso si
// ricalcolano, e l'archivio retroattivo della 2025 esce dai dati che c'erano.
func TestSeasonMigrationUpgradesLegacyData(t *testing.T) {
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.ResetBootstrapState() })

	// Tutte le migrazioni tranne quella della stagione.
	var before core.MigrationsList
	before.Copy(core.SystemMigrations)
	for _, mig := range core.AppMigrations.Items() {
		if !strings.HasSuffix(mig.File, "_season.go") {
			before.Register(mig.Up, mig.Down, mig.File)
		}
	}
	if _, err := core.NewMigrationsRunner(app, before).Up(); err != nil {
		t.Fatal(err)
	}
	if c, _ := app.FindCollectionByNameOrId(regionmedals.Table); c == nil || c.Fields.GetByName("year") == nil || c.Fields.GetByName("season") != nil {
		t.Fatal("prima della stagione le medaglie di regione hanno year")
	}

	// Su un database nuovo lo stato nasce gia' con season; in produzione
	// no: lo riporto com'era (campo tolto, indice unico su socio e medaglia).
	state, err := app.FindCollectionByNameOrId(medals.StateTable)
	if err != nil {
		t.Fatal(err)
	}
	state.Fields.RemoveByName("season")
	state.AddIndex(medals.StateIndex, true, "user, medal", "")
	if err := app.Save(state); err != nil {
		t.Fatal(err)
	}

	f := &medalFixture{t: t, app: app}
	f.save("local_offices", "loA", map[string]any{"name": "Sede A", "region": "Regione A", "slug": "a"})
	f.save("positions", "pA", map[string]any{"state": "Regione A"})
	for _, id := range []string{"1001", "1002", "1003"} {
		f.member(id, "Regione A", true)
	}
	addUser(t, app, "1001", true, time.Now().AddDate(-3, 0, 0))
	f.save("events", "evNew", map[string]any{"name": "Cena nuova", "owner": "9001", "position": "pA", "when_start": at(2026, time.January, 10)})
	f.save("stamp", "sNew", map[string]any{"description": "Cena nuova", "created": at(2025, time.December, 1)})
	f.save("stamp_users", "", map[string]any{"stamp": "sNew", "user": "1001", "created": at(2026, time.January, 10)})

	// Stato di prima: nessun campo season, metalli a vita.
	for medal, tier := range map[string]string{"collector": "gold", "steady": "silver", "active_member": "platinum", "multiscreen": "platinum"} {
		if _, err := app.DB().NewQuery(`INSERT INTO ` + medals.StateTable + ` (id, user, medal, tier, sent_at, created, updated)
			VALUES ({:id}, '1001', {:m}, {:t}, '', '', '')`).Bind(dbx.Params{"id": "st_" + medal, "m": medal, "t": tier}).Execute(); err != nil {
			t.Fatal(err)
		}
	}
	// Medaglie di regione di prima: una della "stagione" 2024 (che resta) e
	// una 2026 (che si rifa').
	for year, tier := range map[int]string{2024: "silver", 2026: "gold"} {
		f.save(regionmedals.Table, "", map[string]any{"local_office": "loA", "region": "Regione A", "region_key": "Regione A",
			"year": year, "medal": "relay", "tier": tier, "current_tier": tier})
	}

	if err := app.RunAllMigrations(); err != nil {
		t.Fatal(err)
	}
	current := season.Current(time.Now())

	// Stato: permanenti a 0 con il metallo di prima, stagionali alla stagione
	// in corso e riportate ai contatori di adesso (nessun timbro).
	stateRow := func(medal string, s int) *core.Record {
		r, err := app.FindFirstRecordByFilter(medals.StateTable, "user = '1001' && medal = {:m} && season = {:s}", dbx.Params{"m": medal, "s": s})
		if err != nil {
			t.Fatalf("stato %s/%d: %v", medal, s, err)
		}
		return r
	}
	if r := stateRow("active_member", 0); r.GetString("tier") != "platinum" {
		t.Errorf("socio attivo: %q", r.GetString("tier"))
	}
	if r := stateRow("multiscreen", 0); r.GetString("tier") != "platinum" {
		t.Errorf("multischermo: %q", r.GetString("tier"))
	}
	if current > liveSeason {
		for _, medal := range []string{"collector", "steady"} {
			if r := stateRow(medal, current); r.GetString("tier") != "none" {
				t.Errorf("%s nella stagione in corso: %q, i timbri di questa stagione sono zero", medal, r.GetString("tier"))
			}
		}
	}
	upgraded, _ := app.FindCollectionByNameOrId(medals.StateTable)
	if !hasUnique(upgraded, "user", "medal", "season") {
		t.Errorf("indice unico dello stato: %v", upgraded.Indexes)
	}
	if n := count(t, app, `SELECT COUNT(*) FROM `+medals.StateTable+` WHERE user = '1001' AND medal = 'collector'`, nil); n != 1 {
		t.Errorf("collezionista: %d righe di stato", n)
	}

	// Medaglie di regione: season accanto a year, la 2024 resta, la 2025
	// e la stagione in corso sono state rifatte (10 medaglie per sede).
	region, _ := app.FindCollectionByNameOrId(regionmedals.Table)
	if region.Fields.GetByName("year") == nil || region.Fields.GetByName("season") == nil {
		t.Error("season si aggiunge e year resta come alias")
	}
	old, err := app.FindFirstRecordByFilter(regionmedals.Table, "season = 2024 && medal = 'relay'")
	if err != nil || old.GetString("tier") != "silver" {
		t.Errorf("riga 2024 persa o cambiata: %v", err)
	}
	for _, s := range []int{2025, current} {
		if n := count(t, app, `SELECT COUNT(*) FROM `+regionmedals.Table+` WHERE season = {:s}`, dbx.Params{"s": s}); n != 10 {
			t.Errorf("medaglie di regione della stagione %d: %d, attese 10", s, n)
		}
	}
	if r, err := app.FindFirstRecordByFilter(regionmedals.Table, "season = {:s} && medal = 'relay'", dbx.Params{"s": current}); err != nil || r.GetString("tier") != "none" {
		t.Errorf("la riga 2026 vecchia doveva essere rifatta da zero: %v", err)
	}

	// Archivio retroattivo della 2025 e stagione salvata.
	if n := count(t, app, `SELECT COUNT(*) FROM `+leaderboard.SeasonsTable+` WHERE season = 2025 AND board = 'members' AND subject = '1001'`, nil); n != 1 {
		t.Errorf("albo d'oro 2025 del socio 1001: %d righe", n)
	}
	if n := count(t, app, `SELECT COUNT(*) FROM `+leaderboard.SeasonsTable+` WHERE season = 2025 AND board = 'regions'`, nil); n != 1 {
		t.Errorf("albo d'oro 2025 delle regioni: %d righe", n)
	}
	if n := count(t, app, `SELECT COUNT(*) FROM `+medals.HistoryTable+` WHERE season = 2025 AND user = '1001'`, nil); n == 0 {
		t.Error("annata 2025 delle medaglie vuota")
	}
	if s, ok := rollover.Stored(app); !ok || s != current {
		t.Errorf("stagione salvata: %d (%v), attesa %d", s, ok, current)
	}
	if r := boardRow(t, app, medals.Table, "1001"); r.GetFloat("season") != float64(current) {
		t.Errorf("view_medals.season: %v", r.GetFloat("season"))
	}
}

// Lo snapshot di fine giornata e' del giorno di Roma: quello delle 23:55 del
// 30 settembre, che per un server in UTC sono le 21:55, e' lo snapshot finale
// della stagione; due minuti dopo la mezzanotte e' gia' un altro giorno. Le
// frecce (previous_*) della stagione nuova non guardano lo snapshot della
// chiusa.
func TestSnapshotUsesRomeDay(t *testing.T) {
	f := newSeasonFixture(t)
	refreshAll(t, f.app, inLive)

	final := time.Date(2026, time.September, 30, 21, 55, 0, 0, time.UTC)
	if err := leaderboard.SnapshotAt(f.app, final); err != nil {
		t.Fatal(err)
	}
	finalRows := func() int {
		return count(t, f.app, `SELECT COUNT(*) FROM `+leaderboard.HistoryTable+` WHERE day = {:d} AND board = 'regions'`, dbx.Params{"d": season.LastDay(liveSeason)})
	}
	if n := finalRows(); n != 2 {
		t.Errorf("snapshot finale delle regioni: %d righe, attese 2", n)
	}

	// Fino alla mezzanotte la freccia guarda l'ultimo snapshot...
	if err := leaderboard.RefreshAt(f.app, final.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if r := boardRow(t, f.app, leaderboard.RegionsTable, "loA"); r.GetFloat("previous_position") != 1 || r.GetFloat("season") != liveSeason {
		t.Errorf("a fine stagione: previous_position=%v season=%v", r.GetFloat("previous_position"), r.GetFloat("season"))
	}
	// ...dopo, nella stagione nuova, niente: lo snapshot e' della chiusa.
	if err := leaderboard.RefreshAt(f.app, final.Add(6*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if r := boardRow(t, f.app, leaderboard.RegionsTable, "loA"); r.GetFloat("previous_position") != 0 || r.GetFloat("season") != liveSeason+1 {
		t.Errorf("a stagione nuova: previous_position=%v season=%v", r.GetFloat("previous_position"), r.GetFloat("season"))
	}

	// Lo snapshot di due minuti dopo la mezzanotte e' del 1 ottobre e lascia
	// stare il finale.
	if err := leaderboard.SnapshotAt(f.app, final.Add(7*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if n := count(t, f.app, `SELECT COUNT(*) FROM `+leaderboard.HistoryTable+` WHERE day = {:d}`, dbx.Params{"d": season.FirstDay(liveSeason + 1)}); n == 0 {
		t.Error("lo snapshot di dopo la mezzanotte deve essere del 1 ottobre")
	}
	if n := finalRows(); n != 2 {
		t.Errorf("lo snapshot finale e' stato toccato: %d righe", n)
	}
}
