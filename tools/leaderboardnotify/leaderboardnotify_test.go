package leaderboardnotify

import (
	"strings"
	"testing"
	"time"

	"mensadb/tools/dbtools"
	"mensadb/tools/leaderboard"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/pocketbase/pocketbase/tools/types"
)

// Il 20 novembre 2026 a mezzogiorno: dopo l'inizio del quindicinale.
var noon = time.Date(2026, time.November, 20, 12, 0, 0, 0, time.UTC)

type fixture struct {
	t    *testing.T
	app  core.App
	sent []dbtools.PushNotification
}

// newFixture prepara un'app di prova con le collection che il pacchetto
// legge, vuote, e intercetta gli invii.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Cleanup)

	f := &fixture{t: t, app: app}
	original := send
	send = func(_ core.App, n dbtools.PushNotification) { f.sent = append(f.sent, n) }
	t.Cleanup(func() { send = original })

	numbers := func(c *core.Collection, names ...string) {
		for _, n := range names {
			c.Fields.Add(&core.NumberField{Name: n})
		}
	}
	freeID := func(c *core.Collection) {
		id := c.Fields.GetByName("id").(*core.TextField)
		id.Min, id.Max, id.AutogeneratePattern = 1, 30, ""
	}
	save := func(c *core.Collection) {
		if err := app.Save(c); err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
	}

	c := core.NewBaseCollection(leaderboard.MembersTable)
	freeID(c)
	c.Fields.Add(&core.TextField{Name: "name"})
	numbers(c, "position", "score", "stamps_total")
	save(c)

	c = core.NewBaseCollection(leaderboard.RegionsTable)
	freeID(c)
	c.Fields.Add(&core.TextField{Name: "name"})
	numbers(c, "position", "score")
	save(c)

	c = core.NewBaseCollection(leaderboard.HistoryTable)
	c.Fields.Add(&core.TextField{Name: "board"}, &core.TextField{Name: "subject"}, &core.TextField{Name: "day"})
	numbers(c, "position", "score")
	save(c)

	for _, name := range []string{"users_metadata", "users_devices", "stamp_users", "local_offices_admins", "configs", "positions", "events"} {
		c = core.NewBaseCollection(name)
		switch name {
		case "users_metadata":
			c.Fields.Add(&core.TextField{Name: "user"}, &core.TextField{Name: "key"}, &core.TextField{Name: "value"})
		case "users_devices":
			c.Fields.Add(&core.TextField{Name: "user"}, &core.TextField{Name: "firebase_id"}, &core.TextField{Name: "language"})
		case "stamp_users":
			c.Fields.Add(&core.TextField{Name: "user"}, &core.TextField{Name: "stamp"})
		case "local_offices_admins":
			c.Fields.Add(&core.TextField{Name: "user"}, &core.TextField{Name: "local_office"}, &core.BoolField{Name: "is_the_officer"})
		case "configs":
			c.Fields.Add(&core.TextField{Name: "key"}, &core.TextField{Name: "value"})
		case "positions":
			c.Fields.Add(&core.TextField{Name: "state"})
		case "events":
			c.Fields.Add(&core.TextField{Name: "name"}, &core.DateField{Name: "when_start"}, &core.TextField{Name: "position"})
		}
		save(c)
	}
	c = core.NewBaseCollection("members_registry")
	freeID(c)
	c.Fields.Add(&core.TextField{Name: "area"})
	save(c)

	if err := EnsureStateCollection(app); err != nil {
		t.Fatal(err)
	}
	// Due volte: idempotente.
	if err := EnsureStateCollection(app); err != nil {
		t.Fatal(err)
	}
	f.config(membersEnabledKey, "true")
	f.config(regionsEnabledKey, "true")
	f.config(thresholdsEnabledKey, "true")
	f.config(fortnightlyEnabledKey, "true")
	return f
}

func (f *fixture) record(collection string, id string, values map[string]any) *core.Record {
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
		r.Set(k, v)
	}
	if err := f.app.SaveNoValidate(r); err != nil {
		f.t.Fatalf("%s/%s: %v", collection, id, err)
	}
	return r
}

func (f *fixture) config(key, value string) {
	f.record("configs", "", map[string]any{"key": key, "value": value})
}

// member mette un socio in classifica con tutto quello che serve per
// ricevere notifiche: timbro, dispositivo, utente iscritto il giorno dato.
func (f *fixture) member(id string, position, score int, created time.Time) {
	f.board(leaderboard.MembersTable, id, position, score, 3)
	f.record("stamp_users", "", map[string]any{"user": id, "stamp": "s1"})
	f.record("users_devices", "", map[string]any{"user": id, "firebase_id": "tok-" + id, "language": "it"})
	f.user(id, created)
}

func (f *fixture) user(id string, created time.Time) {
	c, err := f.app.FindCollectionByNameOrId("users")
	if err != nil {
		f.t.Fatal(err)
	}
	r := core.NewRecord(c)
	r.Id = id
	r.SetEmail(id + "@example.test")
	r.SetPassword("password-di-prova")
	dt, _ := types.ParseDateTime(created)
	r.SetRaw("created", dt)
	if err := f.app.SaveNoValidate(r); err != nil {
		f.t.Fatalf("users/%s: %v", id, err)
	}
}

func (f *fixture) board(table, id string, position, score, stamps int) {
	c, err := f.app.FindCollectionByNameOrId(table)
	if err != nil {
		f.t.Fatal(err)
	}
	r, err := f.app.FindRecordById(c, id)
	if err != nil {
		r = core.NewRecord(c)
		r.Id = id
	}
	r.Set("name", "Socio "+id)
	r.Set("position", position)
	r.Set("score", score)
	if table == leaderboard.MembersTable {
		r.Set("stamps_total", stamps)
	}
	if err := f.app.SaveNoValidate(r); err != nil {
		f.t.Fatalf("%s/%s: %v", table, id, err)
	}
}

func (f *fixture) remove(table, id string) {
	if r, err := f.app.FindRecordById(table, id); err == nil {
		if err := f.app.Delete(r); err != nil {
			f.t.Fatal(err)
		}
	}
}

func (f *fixture) snapshot(board, subject, day string, position, score int) {
	f.record(leaderboard.HistoryTable, "", map[string]any{"board": board, "subject": subject, "day": day, "position": position, "score": score})
}

func (f *fixture) state(board, subject string) *core.Record {
	rows, err := stateRows(f.app, board)
	if err != nil {
		f.t.Fatal(err)
	}
	return rows[subject]
}

func (f *fixture) take() []dbtools.PushNotification {
	out := f.sent
	f.sent = nil
	return out
}

func (f *fixture) run(now time.Time) []dbtools.PushNotification {
	ThresholdsAt(f.app, now)
	return f.take()
}

func (f *fixture) expectOne(sent []dbtools.PushNotification, user, tag string) dbtools.PushNotification {
	f.t.Helper()
	if len(sent) != 1 {
		f.t.Fatalf("attese 1 notifica, trovate %d: %+v", len(sent), sent)
	}
	if sent[0].UserId != user || sent[0].TrTag != tag {
		f.t.Fatalf("attesa %s/%s, trovata %s/%s", user, tag, sent[0].UserId, sent[0].TrTag)
	}
	return sent[0]
}

func TestThresholdsSeedingThenEntering(t *testing.T) {
	f := newFixture(t)
	f.member("100", 12, 40, noon.AddDate(-1, 0, 0))
	f.member("101", 2, 90, noon.AddDate(-1, 0, 0))

	// Primo giro: stato vuoto, si registra e basta.
	if sent := f.run(noon); len(sent) != 0 {
		t.Fatalf("il seeding non manda niente: %+v", sent)
	}
	if s := f.state("members", "101"); s == nil || s.GetFloat("last_position") != 2 {
		t.Fatalf("stato dopo il seeding: %+v", s)
	}

	// Secondo giro, nessun movimento: niente.
	if sent := f.run(noon); len(sent) != 0 {
		t.Fatalf("senza movimenti non si manda: %+v", sent)
	}

	// 100 entra nei primi dieci.
	f.board(leaderboard.MembersTable, "100", 8, 60, 3)
	n := f.expectOne(f.run(noon.Add(time.Hour)), "100", "leaderboard.notify.top10")
	if n.TrNamedParams["position"] != "8" || n.TrNamedParams["score"] != "60" {
		t.Errorf("parametri: %v", n.TrNamedParams)
	}
	if n.Title == "" || !strings.Contains(n.Body, "8°") || !strings.Contains(n.Body, "60 punti") {
		t.Errorf("testo di riserva: %q / %q", n.Title, n.Body)
	}
	if n.Data["type"] != "leaderboard" || n.Data["board"] != "members" {
		t.Errorf("data: %v", n.Data)
	}
	if s := f.state("members", "100"); s.GetDateTime("sent_top10").IsZero() || s.GetFloat("last_position") != 8 {
		t.Errorf("stato dopo l'invio: %v", s.PublicExport())
	}

	// Esce e rientra nella stessa giornata: cooldown, niente.
	f.board(leaderboard.MembersTable, "100", 11, 55, 3)
	if sent := f.run(noon.Add(2 * time.Hour)); len(sent) != 0 {
		t.Fatalf("uscire non manda: %+v", sent)
	}
	f.board(leaderboard.MembersTable, "100", 9, 61, 3)
	if sent := f.run(noon.Add(3 * time.Hour)); len(sent) != 0 {
		t.Fatalf("rientrare entro 30 giorni non manda: %+v", sent)
	}
}

func TestThresholdsHysteresisAndCooldown(t *testing.T) {
	f := newFixture(t)
	f.member("200", 12, 40, noon)
	f.member("201", 12, 40, noon)
	f.run(noon) // seeding

	// 200: ieri sera era quinto (dentro), stamattina era sceso a 12 e ora
	// torna ottavo. Isteresi: niente.
	f.snapshot("members", "200", "2026-11-19", 5, 50)
	f.board(leaderboard.MembersTable, "200", 8, 60, 3)
	if sent := f.run(noon); len(sent) != 0 {
		t.Fatalf("isteresi: %+v", sent)
	}

	// 201: ieri sera era fuori, ma ha gia' ricevuto il top 10 venti giorni fa.
	f.snapshot("members", "201", "2026-11-19", 15, 30)
	s := f.state("members", "201")
	s.Set("sent_top10", noon.AddDate(0, 0, -20))
	if err := f.app.SaveNoValidate(s); err != nil {
		t.Fatal(err)
	}
	f.board(leaderboard.MembersTable, "201", 7, 62, 3)
	if sent := f.run(noon); len(sent) != 0 {
		t.Fatalf("cooldown: %+v", sent)
	}

	// Con l'ultimo invio a 40 giorni fa si rinotifica (esce e rientra).
	f.board(leaderboard.MembersTable, "201", 12, 40, 3)
	f.run(noon)
	s = f.state("members", "201")
	s.Set("sent_top10", noon.AddDate(0, 0, -40))
	if err := f.app.SaveNoValidate(s); err != nil {
		t.Fatal(err)
	}
	f.board(leaderboard.MembersTable, "201", 7, 62, 3)
	f.expectOne(f.run(noon), "201", "leaderboard.notify.top10")
}

func TestThresholdsBestOneWins(t *testing.T) {
	f := newFixture(t)
	f.member("300", 15, 40, noon)
	f.run(noon)

	// Da 15 a 1 in un colpo: una notifica sola, "sei il primo", e tutte e
	// tre le soglie segnate come inviate.
	f.board(leaderboard.MembersTable, "300", 1, 200, 3)
	f.expectOne(f.run(noon), "300", "leaderboard.notify.top1")
	s := f.state("members", "300")
	for _, field := range []string{"sent_top10", "sent_top3", "sent_top1"} {
		if s.GetDateTime(field).IsZero() {
			t.Errorf("%s non segnata", field)
		}
	}

	// Scende a 2 e risale a 1 il giorno dopo con lo snapshot che lo da'
	// secondo: top1 e' fuori ieri (2 > 1) ma in cooldown. Niente.
	f.board(leaderboard.MembersTable, "300", 2, 190, 3)
	f.run(noon.AddDate(0, 0, 1))
	f.snapshot("members", "300", "2026-11-21", 2, 190)
	f.board(leaderboard.MembersTable, "300", 1, 200, 3)
	if sent := f.run(noon.AddDate(0, 0, 2)); len(sent) != 0 {
		t.Fatalf("cooldown sul primo posto: %+v", sent)
	}
}

func TestThresholdsExclusions(t *testing.T) {
	f := newFixture(t)
	day := noon
	cases := []struct {
		id    string
		setup func(id string)
	}{
		{"400", func(id string) { // zero timbri in stamp_users
			f.board(leaderboard.MembersTable, id, 12, 40, 3)
			f.record("users_devices", "", map[string]any{"user": id, "firebase_id": "tok"})
		}},
		{"401", func(id string) { // stamps_total 0 in classifica
			f.member(id, 12, 40, day)
			f.board(leaderboard.MembersTable, id, 12, 40, 0)
		}},
		{"402", func(id string) { // nascosto
			f.member(id, 12, 40, day)
			f.record("users_metadata", "", map[string]any{"user": id, "key": hiddenKey, "value": "true"})
		}},
		{"403", func(id string) { // senza dispositivi
			f.board(leaderboard.MembersTable, id, 12, 40, 3)
			f.record("stamp_users", "", map[string]any{"user": id, "stamp": "s1"})
		}},
		{"404", func(id string) { // notifiche generali spente
			f.member(id, 12, 40, day)
			f.record("users_metadata", "", map[string]any{"user": id, "key": notifyGeneral, "value": "false"})
		}},
		{"405", func(id string) { // notifiche classifica spente
			f.member(id, 12, 40, day)
			f.record("users_metadata", "", map[string]any{"user": id, "key": notifyBoardKey, "value": "false"})
		}},
	}
	for _, c := range cases {
		c.setup(c.id)
	}
	f.member("499", 12, 40, day) // il controllo: riceve
	f.run(noon)
	for _, c := range cases {
		f.board(leaderboard.MembersTable, c.id, 5, 80, 3)
		if c.id == "401" {
			f.board(leaderboard.MembersTable, c.id, 5, 80, 0)
		}
	}
	f.board(leaderboard.MembersTable, "499", 6, 79, 3)
	f.expectOne(f.run(noon), "499", "leaderboard.notify.top10")

	// Lo stato si aggiorna anche per chi non riceve.
	if s := f.state("members", "402"); s == nil || s.GetFloat("last_position") != 5 {
		t.Errorf("stato di un escluso non aggiornato: %v", s)
	}
}

func TestThresholdsBoardDisabledStillTracks(t *testing.T) {
	f := newFixture(t)
	f.member("500", 12, 40, noon)
	f.run(noon)
	cfg, _ := f.app.FindFirstRecordByData("configs", "key", membersEnabledKey)
	cfg.Set("value", "false")
	if err := f.app.SaveNoValidate(cfg); err != nil {
		t.Fatal(err)
	}
	f.board(leaderboard.MembersTable, "500", 5, 80, 3)
	if sent := f.run(noon); len(sent) != 0 {
		t.Fatalf("classifica spenta: %+v", sent)
	}
	if s := f.state("members", "500"); s.GetFloat("last_position") != 5 {
		t.Errorf("stato non aggiornato a classifica spenta")
	}
	// Riaccesa: l'ingresso e' gia' passato, niente raffica.
	cfg.Set("value", "true")
	if err := f.app.SaveNoValidate(cfg); err != nil {
		t.Fatal(err)
	}
	if sent := f.run(noon); len(sent) != 0 {
		t.Fatalf("riaccendere non manda: %+v", sent)
	}
}

// L'interruttore generale delle soglie spento: niente invii, ma lo stato
// si aggiorna, e riaccenderlo non fa partire una raffica.
func TestThresholdsMasterSwitchOff(t *testing.T) {
	f := newFixture(t)
	f.member("500", 12, 40, noon)
	f.run(noon)
	cfg, _ := f.app.FindFirstRecordByData("configs", "key", thresholdsEnabledKey)
	cfg.Set("value", "false")
	if err := f.app.SaveNoValidate(cfg); err != nil {
		t.Fatal(err)
	}
	f.board(leaderboard.MembersTable, "500", 5, 80, 3)
	if sent := f.run(noon); len(sent) != 0 {
		t.Fatalf("interruttore spento: %+v", sent)
	}
	if s := f.state("members", "500"); s.GetFloat("last_position") != 5 {
		t.Errorf("stato non aggiornato a interruttore spento")
	}
	cfg.Set("value", "true")
	if err := f.app.SaveNoValidate(cfg); err != nil {
		t.Fatal(err)
	}
	if sent := f.run(noon); len(sent) != 0 {
		t.Fatalf("riaccendere non manda: %+v", sent)
	}
}

func TestThresholdsRegionsToOfficers(t *testing.T) {
	f := newFixture(t)
	f.member("600", 50, 5, noon) // segretario, con timbro e dispositivo
	f.member("601", 51, 5, noon) // collaboratore, non segretario
	f.member("602", 52, 5, noon) // segretario senza timbri
	stamps, _ := f.app.FindAllRecords("stamp_users")
	for _, s := range stamps {
		if s.GetString("user") == "602" {
			if err := f.app.Delete(s); err != nil {
				t.Fatal(err)
			}
		}
	}
	f.record("local_offices_admins", "", map[string]any{"user": "600", "local_office": "lo1", "is_the_officer": true})
	f.record("local_offices_admins", "", map[string]any{"user": "601", "local_office": "lo1", "is_the_officer": false})
	f.record("local_offices_admins", "", map[string]any{"user": "602", "local_office": "lo2", "is_the_officer": true})
	f.board(leaderboard.RegionsTable, "lo1", 12, 10, 0)
	f.board(leaderboard.RegionsTable, "lo2", 13, 9, 0)
	f.run(noon)

	f.board(leaderboard.RegionsTable, "lo1", 3, 30, 0)
	f.board(leaderboard.RegionsTable, "lo2", 2, 31, 0)
	n := f.expectOne(f.run(noon), "600", "leaderboard.notify.region_top3")
	if n.Data["board"] != "regions" || n.TrNamedParams["position"] != "3" {
		t.Errorf("notifica regione: %+v", n)
	}
	if s := f.state("regions", "lo1"); s.GetDateTime("sent_top10").IsZero() || s.GetDateTime("sent_top3").IsZero() {
		t.Errorf("soglie della regione non segnate")
	}
}

func TestThresholdsVanishedSubjectIsOut(t *testing.T) {
	f := newFixture(t)
	f.member("700", 5, 80, noon)
	f.run(noon)
	f.remove(leaderboard.MembersTable, "700")
	f.run(noon)
	if s := f.state("members", "700"); s.GetFloat("last_position") != 0 {
		t.Fatalf("chi sparisce e' fuori: %v", s.GetFloat("last_position"))
	}
	// Torna: ieri non era in classifica, e' un ingresso.
	f.board(leaderboard.MembersTable, "700", 5, 80, 3)
	f.expectOne(f.run(noon), "700", "leaderboard.notify.top10")
}

func TestIsTurn(t *testing.T) {
	base := time.Date(2026, time.November, 20, 18, 0, 0, 0, time.UTC)
	if !IsTurn(base.AddDate(0, 0, -30), base) || !IsTurn(base.AddDate(0, 0, -15), base) || !IsTurn(base.Add(-time.Hour), base) {
		t.Error("stesso giorno modulo 15: tocca")
	}
	if IsTurn(base.AddDate(0, 0, -1), base) || IsTurn(base.AddDate(0, 0, -14), base) {
		t.Error("giorno diverso modulo 15: non tocca")
	}
	if IsTurn(time.Time{}, base) {
		t.Error("senza data non tocca mai")
	}
	// In quindici giorni consecutivi tocca una volta sola.
	created := time.Date(2024, time.March, 7, 0, 0, 0, 0, time.UTC)
	turns := 0
	for d := 0; d < 15; d++ {
		if IsTurn(created, base.AddDate(0, 0, d)) {
			turns++
		}
	}
	if turns != 1 {
		t.Errorf("tocca %d volte in 15 giorni", turns)
	}
}

func TestVariantDeterministic(t *testing.T) {
	seen := map[int]bool{}
	for _, id := range []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12", "13", "14", "15", "16", "17", "18", "19", "20"} {
		v := variant(id, noon, Variants)
		if v < 1 || v > Variants {
			t.Fatalf("variante fuori intervallo: %d", v)
		}
		if v != variant(id, noon.Add(5*time.Hour), Variants) {
			t.Errorf("stesso socio, stesso giorno, variante diversa")
		}
		seen[v] = true
	}
	if len(seen) < 3 {
		t.Errorf("le varianti non si distribuiscono: %v", seen)
	}
}

func TestTextsCoverAllKeys(t *testing.T) {
	loadTexts()
	tags := []string{"leaderboard.notify.top10", "leaderboard.notify.top3", "leaderboard.notify.top1",
		"leaderboard.notify.region_top10", "leaderboard.notify.region_top3"}
	for _, base := range []string{"up", "down", "down_noevent", "still", "still_noevent"} {
		for i := 1; i <= Variants; i++ {
			tags = append(tags, "leaderboard.notify."+base+"."+string(rune('0'+i)))
		}
	}
	for _, tag := range tags {
		title, body := fallback(tag, map[string]string{"position": "4", "score": "77", "gained": "2 posizioni", "lost": "una posizione", "event": "Cena", "date": "giovedì 3 dicembre"})
		if title == "" || body == "" {
			t.Errorf("%s: testo mancante", tag)
		}
		if strings.Contains(body, "{") || strings.Contains(title, "{") {
			t.Errorf("%s: segnaposto non sostituito: %q", tag, body)
		}
	}
	if positionsText(1) != "una posizione" || positionsText(-3) != "3 posizioni" {
		t.Error("positionsText")
	}
}

func TestPickHistoryRow(t *testing.T) {
	now := noon
	rows := []snapshotRow{{Day: "2026-11-06", Position: 3}, {Day: "2026-11-05", Position: 4}, {Day: "2026-11-02", Position: 9}}
	if r, ok, _ := pickHistoryRow(rows, now); !ok || r.Day != "2026-11-05" {
		t.Errorf("quindici giorni fa: %+v", r)
	}
	if r, ok, _ := pickHistoryRow(rows[:1], now); !ok || r.Day != "2026-11-06" {
		t.Errorf("la piu' vicina disponibile: %+v", r)
	}
	if _, ok, _ := pickHistoryRow(nil, now); ok {
		t.Error("senza storico niente")
	}
}

func TestFortnightly(t *testing.T) {
	f := newFixture(t)
	day := noon.YearDay() % fortnightlyPeriod
	turn := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)
	for turn.YearDay()%fortnightlyPeriod != day {
		turn = turn.AddDate(0, 0, 1)
	}
	notTurn := turn.AddDate(0, 0, 1)

	f.member("800", 7, 80, turn)    // salito da 10
	f.member("801", 12, 50, turn)   // sceso da 9, evento in regione
	f.member("802", 12, 50, turn)   // sceso da 9, nessun evento
	f.member("803", 4, 70, turn)    // fermo
	f.member("804", 7, 80, notTurn) // non tocca oggi
	f.member("805", 7, 80, turn)    // storico troppo corto
	f.member("806", 7, 80, turn)    // zero timbri
	f.board(leaderboard.MembersTable, "806", 7, 80, 0)
	for _, id := range []string{"800", "801", "802", "803", "804", "806"} {
		pos := map[string]int{"800": 10, "801": 9, "802": 9, "803": 4, "804": 10, "806": 10}[id]
		f.snapshot("members", id, "2026-11-05", pos, 40)
	}
	f.snapshot("members", "805", "2026-11-10", 10, 40)

	f.record("members_registry", "801", map[string]any{"area": "Valle d'Aosta"})
	f.record("members_registry", "802", map[string]any{"area": "Sicilia"})
	f.record("positions", "p1", map[string]any{"state": "Piemonte"})
	f.record("positions", "p2", map[string]any{"state": "Lazio"})
	f.record("events", "evt-old", map[string]any{"name": "Passato", "when_start": noon.AddDate(0, 0, -3), "position": "p1"})
	f.record("events", "evt-lazio", map[string]any{"name": "Altrove", "when_start": noon.AddDate(0, 0, 2), "position": "p2"})
	f.record("events", "evt-piem", map[string]any{"name": "Cena a Torino", "when_start": noon.AddDate(0, 0, 5), "position": "p1"})

	// Prima della data d'inizio: niente.
	if n, err := Fortnightly(f.app, FortnightlyStart.Add(-time.Hour)); err != nil || n != 0 || len(f.take()) != 0 {
		t.Fatalf("prima dell'inizio: %d %v", n, err)
	}

	n, err := Fortnightly(f.app, noon)
	if err != nil {
		t.Fatal(err)
	}
	sent := f.take()
	if n != 4 || len(sent) != 4 {
		t.Fatalf("attese 4 notifiche, %d: %+v", len(sent), sent)
	}
	byUser := map[string]dbtools.PushNotification{}
	for _, s := range sent {
		byUser[s.UserId] = s
	}
	up := byUser["800"]
	if !strings.HasPrefix(up.TrTag, "leaderboard.notify.up.") || up.TrNamedParams["gained"] != "3 posizioni" || up.TrNamedParams["position"] != "7" || up.TrNamedParams["score"] != "80" {
		t.Errorf("salito: %+v", up)
	}
	if !strings.Contains(up.Body, "3 posizioni") || up.Data["type"] != "leaderboard" {
		t.Errorf("salito, testo/data: %+v", up)
	}
	down := byUser["801"]
	if !strings.HasPrefix(down.TrTag, "leaderboard.notify.down.") || down.TrNamedParams["lost"] != "3 posizioni" || down.TrNamedParams["event"] != "Cena a Torino" {
		t.Errorf("sceso con evento: %+v", down)
	}
	if down.Data["type"] != "event" || down.Data["event_id"] != "evt-piem" || !strings.Contains(down.Body, "Cena a Torino") || !strings.Contains(down.Body, "novembre") {
		t.Errorf("sceso, deep link: %+v", down)
	}
	noEvent := byUser["802"]
	if !strings.HasPrefix(noEvent.TrTag, "leaderboard.notify.down_noevent.") || noEvent.Data["type"] != "leaderboard" {
		t.Errorf("sceso senza evento: %+v", noEvent)
	}
	still := byUser["803"]
	if !strings.HasPrefix(still.TrTag, "leaderboard.notify.still_noevent.") {
		t.Errorf("fermo: %+v", still)
	}
	for _, s := range sent {
		if strings.Contains(s.Body, "{") || s.Title == "" {
			t.Errorf("testo: %+v", s)
		}
	}

	// Stesso giorno di nuovo: nessun doppione.
	if n, err := Fortnightly(f.app, noon.Add(time.Hour)); err != nil || n != 0 {
		t.Fatalf("doppione: %d %v", n, err)
	}

	// Fallback alle regioni di notify_me_events quando il registro non ha
	// l'area: 802 sceglie il Piemonte e quindici giorni dopo trova l'evento.
	f.record("users_metadata", "", map[string]any{"user": "802", "key": notifyRegions, "value": `["Piemonte"]`})
	f.remove("members_registry", "802")
	later := noon.AddDate(0, 0, 15)
	f.snapshot("members", "802", "2026-11-20", 9, 40)
	f.record("events", "evt-later", map[string]any{"name": "Aperitivo", "when_start": later.AddDate(0, 0, 1), "position": "p1"})
	if _, err := Fortnightly(f.app, later); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range f.take() {
		if s.UserId == "802" {
			found = true
			if s.Data["event_id"] != "evt-later" {
				t.Errorf("fallback regioni: %+v", s)
			}
		}
	}
	if !found {
		t.Error("802 non ha ricevuto il secondo giro")
	}
}

func TestRegionKey(t *testing.T) {
	cases := map[string]string{
		"Valle d'Aosta": "Piemonte", "Basilicata": "Puglia", "Trentino-Alto Adige/Südtirol": "Trentino-Alto Adige",
		"Emilia Romagna": "Emilia-Romagna", "Lazio": "Lazio", "": "", "NaN": "",
	}
	for in, want := range cases {
		if got := regionKey(in); got != want {
			t.Errorf("%q: %q, atteso %q", in, got, want)
		}
	}
}
