package migrations

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"mensadb/tools/leaderboard"
	"mensadb/tools/regionmedals"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

// La collection delle medaglie di regione nasce con i campi, le regole e
// l'indice unico del contratto con l'app, e rifare la migrazione non rompe
// niente.
func TestRegionMedalsCollection(t *testing.T) {
	app := newMigratedApp(t)

	c, err := app.FindCollectionByNameOrId(regionmedals.Table)
	if err != nil {
		t.Fatal(err)
	}
	if c.IsView() {
		t.Fatal("deve essere una collection base")
	}
	want := `@request.auth.id != ""`
	if c.ListRule == nil || *c.ListRule != want || c.ViewRule == nil || *c.ViewRule != want {
		t.Errorf("regole di lettura: list=%v view=%v", c.ListRule, c.ViewRule)
	}
	if c.CreateRule != nil || c.UpdateRule != nil || c.DeleteRule != nil {
		t.Error("scrittura solo superuser")
	}

	for _, name := range []string{"local_office", "region", "region_key", "season", "medal", "value", "count", "extra",
		"levels", "minimum", "eligible", "current_tier", "tier", "unlocked", "witnesses", "created", "updated"} {
		if c.Fields.GetByName(name) == nil {
			t.Errorf("campo %s mancante", name)
		}
	}
	rel, ok := c.Fields.GetByName("local_office").(*core.RelationField)
	if !ok || rel.MaxSelect != 1 || !rel.Required {
		t.Errorf("local_office: %+v", c.Fields.GetByName("local_office"))
	}
	medal, ok := c.Fields.GetByName("medal").(*core.SelectField)
	wantMedals := []string{"present", "host", "away", "months", "welcome", "fullhouse", "core", "national", "relay", "podium"}
	if !ok || !slices.Equal(medal.Values, wantMedals) {
		t.Errorf("medal: %+v", c.Fields.GetByName("medal"))
	}
	for _, name := range []string{"tier", "current_tier"} {
		f, ok := c.Fields.GetByName(name).(*core.SelectField)
		if !ok || !slices.Equal(f.Values, []string{"none", "bronze", "silver", "gold"}) {
			t.Errorf("%s: %+v", name, c.Fields.GetByName(name))
		}
	}
	for _, name := range []string{"levels", "unlocked", "witnesses"} {
		if _, ok := c.Fields.GetByName(name).(*core.JSONField); !ok {
			t.Errorf("%s non e' json", name)
		}
	}
	if _, ok := c.Fields.GetByName("eligible").(*core.BoolField); !ok {
		t.Error("eligible non e' bool")
	}
	for _, name := range []string{"created", "updated"} {
		f, ok := c.Fields.GetByName(name).(*core.AutodateField)
		if !ok || !f.OnCreate || (name == "updated" && !f.OnUpdate) {
			t.Errorf("%s: %+v", name, c.Fields.GetByName(name))
		}
	}

	unique := false
	for _, idx := range c.Indexes {
		if strings.Contains(idx, "UNIQUE") && strings.Contains(idx, "local_office") && strings.Contains(idx, "season") && strings.Contains(idx, "medal") {
			unique = true
		}
	}
	if !unique {
		t.Errorf("indice unico (local_office, season, medal) mancante: %v", c.Indexes)
	}

	if err := upRegionMedals(app); err != nil {
		t.Fatalf("seconda up: %v", err)
	}
	if err := regionmedals.Refresh(app); err != nil {
		t.Fatalf("refresh senza dati: %v", err)
	}
}

// medalFixture e' una piccola Italia: tre sedi (la terza e' una sede
// accorpata, con un nome diverso da quello del registro), qualche socio e
// qualche evento del 2026 (che e' la stagione 2025/26).
type medalFixture struct {
	t   *testing.T
	app core.App
}

func (f *medalFixture) save(collection, id string, values map[string]any) {
	f.t.Helper()
	c, err := f.app.FindCollectionByNameOrId(collection)
	if err != nil {
		f.t.Fatal(err)
	}
	r := core.NewRecord(c)
	if id != "" {
		r.Id = id
	}
	for k, v := range values {
		// created e' un autodate: Set lo riscriverebbe con l'ora di adesso,
		// con SetRaw la data del fixture resta quella.
		if dt, ok := v.(types.DateTime); ok && k == "created" {
			r.SetRaw(k, dt.String())
			continue
		}
		r.Set(k, v)
	}
	if err := f.app.SaveNoValidate(r); err != nil {
		f.t.Fatalf("%s/%s: %v", collection, id, err)
	}
}

func day(month time.Month, d int) types.DateTime {
	dt, _ := types.ParseDateTime(time.Date(2026, month, d, 10, 0, 0, 0, time.UTC))
	return dt
}

func (f *medalFixture) member(id, area string, active bool) {
	f.save("members_registry", id, map[string]any{"area": area, "is_active": active})
}

// event crea un evento con il suo timbro (stesso nome) nella data data.
func (f *medalFixture) event(id, owner, position string, national bool, when types.DateTime) {
	f.save("events", id, map[string]any{"name": "Evento " + id, "owner": owner, "position": position, "is_national": national, "when_start": when})
	f.save("stamp", "s"+id, map[string]any{"description": "Evento " + id})
}

func (f *medalFixture) stamp(event, user string) {
	f.t.Helper()
	ev, err := f.app.FindRecordById("events", event)
	if err != nil {
		f.t.Fatal(err)
	}
	f.save("stamp_users", "", map[string]any{"stamp": "s" + event, "user": user, "created": ev.GetDateTime("when_start")})
}

func (f *medalFixture) unstamp(event, user string) {
	f.t.Helper()
	r, err := f.app.FindFirstRecordByFilter("stamp_users", "stamp = {:s} && user = {:u}", dbx.Params{"s": "s" + event, "u": user})
	if err != nil {
		f.t.Fatal(err)
	}
	if err := f.app.Delete(r); err != nil {
		f.t.Fatal(err)
	}
}

func (f *medalFixture) row(office, medal string, s int) *core.Record {
	f.t.Helper()
	r, err := f.app.FindFirstRecordByFilter(regionmedals.Table, "local_office = {:o} && medal = {:m} && season = {:y}",
		dbx.Params{"o": office, "m": medal, "y": s})
	if err != nil {
		f.t.Fatalf("riga %s/%s/%d: %v", office, medal, s, err)
	}
	return r
}

func jsonOf[T any](t *testing.T, r *core.Record, field string) T {
	t.Helper()
	var out T
	if err := json.Unmarshal([]byte(r.GetString(field)), &out); err != nil {
		t.Fatalf("%s: %v", field, err)
	}
	return out
}

func newMedalFixture(t *testing.T) *medalFixture {
	f := &medalFixture{t: t, app: newMigratedApp(t)}

	f.save("local_offices", "loA", map[string]any{"name": "Sede A", "region": "Regione A", "slug": "a"})
	f.save("local_offices", "loB", map[string]any{"name": "Sede B", "region": "Regione B", "slug": "b"})
	f.save("local_offices", "loC", map[string]any{"name": "Sede C", "region": "Piemonte e Val d'Aosta", "slug": "c"})
	f.save("positions", "pA", map[string]any{"state": "Regione A"})
	f.save("positions", "pB", map[string]any{"state": "Regione B"})

	// Regione A: sei soci attivi piu' uno non attivo (non ne fa parte).
	for _, id := range []string{"1001", "1002", "1003", "1004", "1005", "1006"} {
		f.member(id, "Regione A", true)
	}
	f.member("1007", "Regione A", false)
	f.member("2001", "Regione B", true)
	f.member("2002", "Regione B", true)
	f.member("2003", "Regione B", true)
	// La sede C raccoglie un'area che il registro chiama in un altro modo.
	f.member("3001", "Valle d'Aosta", true)

	// Iscritti all'app nella stagione: Benvenuto.
	for _, id := range []string{"1001", "1002", "1003"} {
		f.save("users", id, map[string]any{"email": id + "@example.test", "created": day(time.January, 5)})
	}

	// Tre eventi regionali in A organizzati da due persone, uno nazionale.
	f.event("e1", "9001", "pA", false, day(time.January, 10))
	f.event("e2", "9001", "pA", false, day(time.February, 10))
	f.event("e3", "9002", "pA", false, day(time.March, 10))
	f.event("n1", "9001", "pA", true, day(time.April, 10))

	for _, u := range []string{"1001", "1002", "1003", "1004", "1005"} {
		f.stamp("e1", u)
	}
	for _, u := range []string{"1001", "1002", "1003", "1004"} {
		f.stamp("e2", u)
	}
	f.stamp("e3", "1001")
	f.stamp("e3", "1002")
	f.stamp("n1", "1001")
	// Ospiti da altre regioni, visti due volte.
	for _, u := range []string{"2001", "2002", "3001"} {
		f.stamp("e1", u)
		f.stamp("e2", u)
	}
	// Nella tabella delle regioni A e' prima, B seconda.
	f.save(leaderboard.RegionsTable, "loA", map[string]any{"name": "Sede A", "position": 1, "score": 40})
	f.save(leaderboard.RegionsTable, "loB", map[string]any{"name": "Sede B", "position": 2, "score": 25})
	return f
}

// L'estate del 2026 e' ancora la stagione 2025 (dal 1/10/2025 al 30/9/2026):
// gli eventi di gennaio-maggio del fixture sono tutti suoi.
var summer2026 = time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)

const fixtureSeason = 2025

// Un ricalcolo su una piccola fotografia produce i metalli attesi, con i
// numeri di contorno, le soglie e chi c'era; la prima volta non sblocca
// niente (le righe nascono con i metalli che hanno gia').
func TestRegionMedalsRefresh(t *testing.T) {
	f := newMedalFixture(t)

	unlocks, err := regionmedals.RefreshUnlocksAt(f.app, summer2026)
	if err != nil {
		t.Fatal(err)
	}
	if len(unlocks) != 0 {
		t.Errorf("il primo ricalcolo non sblocca niente: %+v", unlocks)
	}
	if n, _ := f.app.CountRecords(regionmedals.Table); n != 30 {
		t.Fatalf("righe: %d, attese 30 (3 sedi x 10 medaglie)", n)
	}

	type want struct {
		tier     string
		value    float64
		count    float64
		eligible bool
	}
	expect := map[string]want{
		"present":   {tier: "gold", count: 4, eligible: true},
		"host":      {tier: "bronze", value: 2, count: 3, eligible: true},
		"away":      {tier: "none", eligible: true},
		"months":    {tier: "none", value: 3, count: 3, eligible: true},
		"welcome":   {tier: "gold", value: 100, count: 3, eligible: true},
		"fullhouse": {tier: "gold", count: 8, eligible: true},
		"core":      {tier: "gold", count: 8, eligible: true},
		"national":  {tier: "bronze", value: 1, eligible: true},
		"relay":     {tier: "bronze", value: 2, count: 3, eligible: true},
		"podium":    {tier: "none", value: 1, count: 40, eligible: true},
	}
	for medal, w := range expect {
		r := f.row("loA", medal, fixtureSeason)
		if r.GetString("tier") != w.tier || r.GetString("current_tier") != w.tier {
			t.Errorf("%s: tier %q / current %q, atteso %q", medal, r.GetString("tier"), r.GetString("current_tier"), w.tier)
		}
		if r.GetBool("eligible") != w.eligible || r.GetFloat("count") != w.count {
			t.Errorf("%s: eligible=%v count=%v, attesi %v %v", medal, r.GetBool("eligible"), r.GetFloat("count"), w.eligible, w.count)
		}
		if w.value != 0 && r.GetFloat("value") != w.value {
			t.Errorf("%s: value %v, atteso %v", medal, r.GetFloat("value"), w.value)
		}
		if r.GetString("region") != "Regione A" || r.GetString("region_key") != "Regione A" {
			t.Errorf("%s: regione %q / %q", medal, r.GetString("region"), r.GetString("region_key"))
		}
	}
	if r := f.row("loA", "present", fixtureSeason); r.GetFloat("extra") != 1 || r.GetFloat("value") <= 15 {
		t.Errorf("present: extra=%v value=%v", r.GetFloat("extra"), r.GetFloat("value"))
	}
	if r := f.row("loA", "welcome", fixtureSeason); r.GetFloat("extra") != 3 || r.GetFloat("minimum") != 3 {
		t.Errorf("welcome: extra=%v minimum=%v", r.GetFloat("extra"), r.GetFloat("minimum"))
	}
	if r := f.row("loA", "core", fixtureSeason); r.GetFloat("extra") < 7 || r.GetFloat("extra") > 8 {
		t.Errorf("core: persone effettive %v", r.GetFloat("extra"))
	}

	// Soglie nel formato del contratto.
	levels := jsonOf[map[string]float64](t, f.row("loA", "fullhouse", fixtureSeason), "levels")
	if levels["bronze"] != 8.5 || levels["silver"] != 15 || levels["gold"] != 25 {
		t.Errorf("soglie sala piena: %v", levels)
	}
	levels = jsonOf[map[string]float64](t, f.row("loA", "podium", fixtureSeason), "levels")
	if levels["bronze"] != 3 || levels["silver"] != 2 || levels["gold"] != 1 {
		t.Errorf("soglie podio: %v", levels)
	}

	// Sblocchi e testimoni: oro include bronzo e argento, con i soci attivi.
	relay := f.row("loA", "relay", fixtureSeason)
	unlocked := jsonOf[map[string]string](t, relay, "unlocked")
	if _, ok := unlocked["bronze"]; !ok || len(unlocked) != 1 {
		t.Errorf("relay unlocked: %v", unlocked)
	}
	witnesses := jsonOf[map[string][]string](t, relay, "witnesses")
	if !slices.Equal(witnesses["bronze"], []string{"1001", "1002", "1003", "1004", "1005", "1006"}) {
		t.Errorf("testimoni (solo attivi, 1007 escluso): %v", witnesses)
	}
	present := f.row("loA", "present", fixtureSeason)
	if u := jsonOf[map[string]string](t, present, "unlocked"); len(u) != 3 {
		t.Errorf("oro al primo colpo: bronzo, argento e oro tutti sbloccati: %v", u)
	}
	if _, err := time.Parse("2006-01-02 15:04:05.000Z", jsonOf[map[string]string](t, present, "unlocked")["gold"]); err != nil {
		t.Errorf("formato data: %v", err)
	}

	// La sede accorpata prende i soci dell'area che il registro chiama in
	// modo diverso, e la sua regione normalizzata.
	c := f.row("loC", "months", fixtureSeason)
	if c.GetString("region") != "Piemonte e Val d'Aosta" || c.GetString("region_key") != "Piemonte" {
		t.Errorf("sede accorpata: %q / %q", c.GetString("region"), c.GetString("region_key"))
	}
	if c.GetString("tier") != "none" {
		t.Errorf("la sede C non ha eventi: %q", c.GetString("tier"))
	}

	// Un secondo ricalcolo identico non cambia niente (nemmeno updated).
	before := present.GetString("updated")
	time.Sleep(5 * time.Millisecond)
	if unlocks, err := regionmedals.RefreshUnlocksAt(f.app, summer2026.Add(time.Hour)); err != nil || len(unlocks) != 0 {
		t.Fatalf("secondo ricalcolo: %v %+v", err, unlocks)
	}
	if after := f.row("loA", "present", fixtureSeason).GetString("updated"); after != before {
		t.Errorf("riga invariata riscritta: %s -> %s", before, after)
	}
}

// Il metallo massimo della stagione non scende mai; i testimoni restano quelli
// del momento dello sblocco; un metallo nuovo su una riga che c'era
// ritorna come sblocco, con i soci di quel momento.
func TestRegionMedalsNeverDecrease(t *testing.T) {
	f := newMedalFixture(t)
	if _, err := regionmedals.RefreshUnlocksAt(f.app, summer2026); err != nil {
		t.Fatal(err)
	}
	firstUnlocked := f.row("loA", "present", fixtureSeason).GetString("unlocked")

	// Meno dati: restano a Tutti presenti solo due soci, sotto il minimo.
	f.unstamp("e2", "1003")
	f.unstamp("e2", "1004")
	f.member("1008", "Regione A", true)

	unlocks, err := regionmedals.RefreshUnlocksAt(f.app, summer2026.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(unlocks) != 0 {
		t.Errorf("nessun metallo nuovo: %+v", unlocks)
	}
	present := f.row("loA", "present", fixtureSeason)
	if present.GetString("current_tier") != "none" || present.GetBool("eligible") {
		t.Errorf("adesso dovrebbe essere sotto il minimo: current=%q eligible=%v", present.GetString("current_tier"), present.GetBool("eligible"))
	}
	if present.GetString("tier") != "gold" {
		t.Errorf("il massimo non scende: %q", present.GetString("tier"))
	}
	if present.GetString("unlocked") != firstUnlocked {
		t.Error("le date di sblocco non si riscrivono")
	}
	if w := jsonOf[map[string][]string](t, present, "witnesses")["gold"]; slices.Contains(w, "1008") || len(w) != 6 {
		t.Errorf("testimoni dell'oro cambiati: %v", w)
	}

	// Un terzo organizzatore in A: Staffetta passa da bronzo ad argento.
	f.event("e4", "9003", "pA", false, day(time.May, 10))
	f.stamp("e4", "1001")
	unlocks, err = regionmedals.RefreshUnlocksAt(f.app, summer2026.Add(48*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(unlocks) != 1 || unlocks[0] != (regionmedals.Unlock{LocalOffice: "loA", RegionKey: "Regione A", Medal: "relay", Tier: "silver", Season: fixtureSeason}) {
		t.Fatalf("sblocchi: %+v", unlocks)
	}
	relay := f.row("loA", "relay", fixtureSeason)
	w := jsonOf[map[string][]string](t, relay, "witnesses")
	if slices.Contains(w["bronze"], "1008") || !slices.Contains(w["silver"], "1008") {
		t.Errorf("testimoni al momento di ogni sblocco: %v", w)
	}

	// Altre stagioni e altre sedi non si toccano: un ricalcolo della stagione
	// dopo lascia stare le righe della 2025 (a parte il Podio, qui senza
	// snapshot).
	if _, err := regionmedals.RefreshUnlocksAt(f.app, time.Date(2027, time.February, 1, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if got := f.row("loA", "relay", fixtureSeason).GetString("tier"); got != "silver" {
		t.Errorf("riga della stagione scorsa toccata: %q", got)
	}
	if got := f.row("loA", "relay", fixtureSeason+1).GetString("tier"); got != "none" {
		t.Errorf("riga della stagione nuova: %q", got)
	}
	if n, _ := f.app.CountRecords(regionmedals.Table); n != 60 {
		t.Errorf("righe: %d, attese 60", n)
	}
}

// Il Podio resta senza metallo finche' non c'e' lo snapshot del 30/9, poi
// segue la posizione di quel giorno; il value e' sempre quello di adesso. Il
// primo ricalcolo della stagione dopo assegna il Podio della stagione chiusa
// se lo snapshot e' arrivato dopo l'ultimo ricalcolo.
func TestRegionMedalsPodium(t *testing.T) {
	f := newMedalFixture(t)
	if _, err := regionmedals.RefreshUnlocksAt(f.app, summer2026); err != nil {
		t.Fatal(err)
	}
	podium := f.row("loA", "podium", fixtureSeason)
	if podium.GetString("tier") != "none" || podium.GetString("current_tier") != "none" || podium.GetFloat("value") != 1 {
		t.Fatalf("senza snapshot: tier=%q current=%q value=%v", podium.GetString("tier"), podium.GetString("current_tier"), podium.GetFloat("value"))
	}

	// Snapshot del 30/9: A prima, B seconda, C quarta.
	for _, s := range []struct {
		office   string
		position int
	}{{"loA", 1}, {"loB", 2}, {"loC", 4}} {
		f.save(leaderboard.HistoryTable, "", map[string]any{"board": "regions", "subject": s.office, "day": "2026-09-30", "position": s.position})
	}
	// Uno snapshot di un'altra classifica o di un altro giorno non conta.
	f.save(leaderboard.HistoryTable, "", map[string]any{"board": "members", "subject": "loC", "day": "2026-09-30", "position": 1})

	// Il 30/9 sera (21:58 a Roma), stagione ancora la 2025.
	regionA, err := f.app.FindRecordById(leaderboard.RegionsTable, "loA")
	if err != nil {
		t.Fatal(err)
	}
	regionA.Set("position", 3)
	regionA.Set("score", 30)
	if err := f.app.SaveNoValidate(regionA); err != nil {
		t.Fatal(err)
	}
	unlocks, err := regionmedals.RefreshUnlocksAt(f.app, time.Date(2026, time.September, 30, 19, 58, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if got := f.row("loA", "podium", fixtureSeason); got.GetString("tier") != "gold" || got.GetString("current_tier") != "gold" || got.GetFloat("value") != 3 {
		t.Errorf("podio A: tier=%q current=%q value=%v (value e' la posizione di adesso)", got.GetString("tier"), got.GetString("current_tier"), got.GetFloat("value"))
	}
	if got := f.row("loB", "podium", fixtureSeason).GetString("tier"); got != "silver" {
		t.Errorf("podio B: %q", got)
	}
	if got := f.row("loC", "podium", fixtureSeason).GetString("tier"); got != "none" {
		t.Errorf("podio C: %q", got)
	}
	podiumUnlocks := 0
	for _, u := range unlocks {
		if u.Medal == "podium" {
			podiumUnlocks++
		}
	}
	if podiumUnlocks != 2 {
		t.Errorf("sblocchi del podio: %d in %+v", podiumUnlocks, unlocks)
	}
}

func TestRegionMedalsPodiumSettledNextSeason(t *testing.T) {
	f := newMedalFixture(t)
	if _, err := regionmedals.RefreshUnlocksAt(f.app, time.Date(2026, time.September, 30, 16, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	f.save(leaderboard.HistoryTable, "", map[string]any{"board": "regions", "subject": "loB", "day": "2026-09-30", "position": 1})
	f.save(leaderboard.HistoryTable, "", map[string]any{"board": "regions", "subject": "loA", "day": "2026-09-30", "position": 3})

	unlocks, err := regionmedals.RefreshUnlocksAt(f.app, time.Date(2026, time.October, 1, 0, 15, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	b := f.row("loB", "podium", fixtureSeason)
	if b.GetString("tier") != "gold" || b.GetFloat("value") != 1 {
		t.Errorf("podio 2025 di B: tier=%q value=%v", b.GetString("tier"), b.GetFloat("value"))
	}
	if got := f.row("loA", "podium", fixtureSeason).GetString("tier"); got != "bronze" {
		t.Errorf("podio 2025 di A: %q", got)
	}
	if len(jsonOf[map[string][]string](t, b, "witnesses")["gold"]) != 3 {
		t.Errorf("testimoni del podio: %v", b.GetString("witnesses"))
	}
	if got := f.row("loB", "podium", fixtureSeason+1).GetString("tier"); got != "none" {
		t.Errorf("la stagione nuova riparte da zero: %q", got)
	}
	if len(unlocks) != 2 || unlocks[0].Medal != "podium" || unlocks[0].Season != fixtureSeason {
		t.Errorf("sblocchi: %+v", unlocks)
	}

	// Una volta assegnato non si riassegna.
	unlocks, err = regionmedals.RefreshUnlocksAt(f.app, time.Date(2026, time.October, 1, 0, 30, 0, 0, time.UTC))
	if err != nil || len(unlocks) != 0 {
		t.Errorf("secondo giro: %v %+v", err, unlocks)
	}
}
