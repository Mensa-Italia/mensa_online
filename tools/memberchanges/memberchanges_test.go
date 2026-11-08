package memberchanges_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	_ "mensadb/migrations"
	"mensadb/tools/memberchanges"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Con Go 1.27 serve GOEXPERIMENT=nojsonv2 (vedi migrations).

func newApp(t *testing.T) core.App {
	t.Helper()
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.ResetBootstrapState() })
	if err := app.RunAllMigrations(); err != nil {
		t.Fatal(err)
	}
	return app
}

func save(t *testing.T, app core.App, collection, id string, values map[string]any) {
	t.Helper()
	c, err := app.FindCollectionByNameOrId(collection)
	if err != nil {
		t.Fatal(err)
	}
	r := core.NewRecord(c)
	r.Id = id
	for k, v := range values {
		r.Set(k, v)
	}
	if err := app.SaveNoValidate(r); err != nil {
		t.Fatal(err)
	}
}

func TestDiff(t *testing.T) {
	prev := memberchanges.Snapshot{
		"1": {Area: "Lombardia", Active: true, Name: "Uno"},      // esce
		"2": {Area: "Lazio", Active: true, Name: "Due"},          // resta
		"3": {Area: "Valle d'Aosta", Active: true, Name: "Tre"},  // Piemonte -> Lombardia
		"4": {Area: "Basilicata", Active: true, Name: "Quattro"}, // stessa chiave (Puglia)
		"5": {Area: "Lazio", Active: false, Name: "Cinque"},      // si riattiva
		"6": {Area: "Toscana", Active: true, Name: "Sei"},        // va all'estero
		"7": {Area: "NaN", Active: true, Name: "Sette"},          // torna dall'estero
		"8": {Area: "Sicilia", Active: false, Name: "Otto"},      // resta non attivo
		"9": {Area: "Marche", Active: true, Name: "Nove"},        // diventa non attivo
	}
	next := memberchanges.Snapshot{
		"2":  {Area: "Lazio", Active: true, Name: "Due"},
		"3":  {Area: "Lombardia", Active: true, Name: "Tre"},
		"4":  {Area: "Puglia", Active: true, Name: "Quattro"},
		"5":  {Area: "Lazio", Active: true, Name: "Cinque"},
		"6":  {Area: "", Active: true, Name: "Sei"},
		"7":  {Area: "Veneto", Active: true, Name: "Sette"},
		"8":  {Area: "Sicilia", Active: false, Name: "Otto"},
		"9":  {Area: "Marche", Active: false, Name: "Nove"},
		"10": {Area: "Trentino-Alto Adige/Südtirol", Active: true, Name: "Dieci"}, // nuovo
		"11": {Area: "", Active: true, Name: "Undici"},                            // nuovo all'estero
	}
	got := memberchanges.Diff(prev, next)
	want := []memberchanges.Change{
		{Member: "1", Name: "Uno", Kind: "left", RegionKey: "Lombardia"},
		{Member: "10", Name: "Dieci", Kind: "joined", RegionKey: "Trentino-Alto Adige"},
		{Member: "11", Name: "Undici", Kind: "joined", RegionKey: ""},
		{Member: "3", Name: "Tre", Kind: "moved_out", RegionKey: "Piemonte"},
		{Member: "3", Name: "Tre", Kind: "moved_in", RegionKey: "Lombardia"},
		{Member: "5", Name: "Cinque", Kind: "joined", RegionKey: "Lazio"},
		{Member: "6", Name: "Sei", Kind: "moved_out", RegionKey: "Toscana"},
		{Member: "6", Name: "Sei", Kind: "moved_in", RegionKey: ""},
		{Member: "7", Name: "Sette", Kind: "moved_out", RegionKey: ""},
		{Member: "7", Name: "Sette", Kind: "moved_in", RegionKey: "Veneto"},
		{Member: "9", Name: "Nove", Kind: "left", RegionKey: "Marche"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("Diff:\n got %+v\nwant %+v", got, want)
	}
	if len(memberchanges.Diff(next, next)) != 0 {
		t.Error("due fotografie uguali non danno movimenti")
	}
}

func TestRecordResolvesOfficeAndIsIdempotent(t *testing.T) {
	app := newApp(t)
	save(t, app, "local_offices", "lopiemonte00001", map[string]any{"name": "Piemonte", "region": "Piemonte e Val d'Aosta", "slug": "piemonte"})
	save(t, app, "local_offices", "lolombardia0001", map[string]any{"name": "Lombardia", "region": "Lombardia", "region_key": "Lombardia", "slug": "lombardia"})
	for _, id := range []string{"3", "10"} {
		save(t, app, "members_registry", id, map[string]any{"name": "Socio " + id, "is_active": true})
	}

	changes := []memberchanges.Change{
		{Member: "3", Name: "Tre", Kind: "moved_out", RegionKey: "Piemonte"},
		{Member: "3", Name: "Tre", Kind: "moved_in", RegionKey: "Lombardia"},
		{Member: "10", Name: "Dieci", Kind: "joined", RegionKey: "Calabria"}, // regione senza sede
		{Member: "11", Name: "Undici", Kind: "joined", RegionKey: ""},        // estero: niente riga
	}
	day := time.Date(2026, time.November, 7, 23, 30, 0, 0, time.UTC) // 8 novembre a Roma
	for range 2 {
		if err := memberchanges.Record(app, changes, day); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := app.FindAllRecords(memberchanges.Table)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("righe: %d, attese 3", len(rows))
	}
	byKind := map[string]*core.Record{}
	for _, r := range rows {
		byKind[r.GetString("member")+"/"+r.GetString("kind")] = r
	}
	if r := byKind["3/moved_out"]; r == nil || r.GetString("local_office") != "lopiemonte00001" {
		t.Errorf("moved_out: %v", r)
	}
	if r := byKind["3/moved_in"]; r == nil || r.GetString("local_office") != "lolombardia0001" || r.GetString("name") != "Tre" {
		t.Errorf("moved_in: %v", r)
	}
	if r := byKind["10/joined"]; r == nil || r.GetString("local_office") != "" || r.GetString("region_key") != "Calabria" {
		t.Errorf("joined senza sede: %v", r)
	}
	// Il giorno e' la mezzanotte di Roma: 8 novembre 00:00 CET = 7 novembre 23:00 UTC.
	if got := byKind["3/moved_in"].GetDateTime("day").String(); got != "2026-11-07 23:00:00.000Z" {
		t.Errorf("day: %s", got)
	}

	// Un giorno diverso scrive di nuovo.
	if err := memberchanges.Record(app, changes[:1], day.AddDate(0, 0, 1)); err != nil {
		t.Fatal(err)
	}
	total, _ := app.CountRecords(memberchanges.Table)
	if total != 4 {
		t.Errorf("righe dopo il secondo giorno: %d", total)
	}
}

// fakeSource e' una cartella di snapshot finta.
type fakeSource struct {
	files map[string]string
	err   error
}

func (f fakeSource) Keys() ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make([]string, 0, len(f.files))
	for k := range f.files {
		out = append(out, k)
	}
	return out, nil
}

func (f fakeSource) Get(key string) ([]byte, error) { return []byte(f.files[key]), nil }

func TestBackfill(t *testing.T) {
	app := newApp(t)
	save(t, app, "local_offices", "lolazio00000001", map[string]any{"name": "Lazio", "region": "Lazio", "slug": "lazio"})
	save(t, app, "members_registry", "1", map[string]any{"area": "Lazio", "is_active": false})
	save(t, app, "members_registry", "2", map[string]any{"area": "Lazio", "is_active": true})
	save(t, app, "members_registry", "3", map[string]any{"area": "Lazio", "is_active": true})

	// S3 spento: niente righe, niente chiave, nessun errore.
	if err := memberchanges.BackfillFrom(app, fakeSource{err: errors.New("s3 is disabled")}); err != nil {
		t.Fatal(err)
	}
	if rec, _ := app.FindFirstRecordByData("configs", "key", memberchanges.BackfillConfigKey); rec != nil {
		t.Fatal("la chiave non va scritta se S3 non c'e'")
	}

	src := fakeSource{files: map[string]string{
		// primo giorno: nessun movimento, non c'e' un prima
		"snapshot_members/2026-11-01_00-00-00.json.gz": `[{"id":"1","area":"Lazio","is_active":true}]`,
		// secondo dello stesso giorno: ignorato
		"snapshot_members/2026-11-01_09-00-00.json.gz": `[{"id":"1","area":"Lazio","is_active":true},{"id":"9","area":"Lazio","is_active":true}]`,
		// entra 2
		"snapshot_members/2026-11-03_00-00-00.json.gz": `[{"id":"1","area":"Lazio","is_active":true},{"id":"2","area":"Lazio","is_active":true}]`,
		"snapshot_members/altro.txt":                   `x`,
	}}
	// Adesso: 1 non e' piu' attivo, entra 3.
	if err := memberchanges.BackfillFrom(app, src); err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Member string `db:"member"`
		Kind   string `db:"kind"`
		Day    string `db:"day"`
	}
	if err := app.DB().NewQuery(`SELECT member, kind, day FROM ` + memberchanges.Table + ` ORDER BY day, member`).All(&rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("righe: %+v", rows)
	}
	if rows[0].Member != "2" || rows[0].Kind != "joined" || rows[0].Day != "2026-11-02 23:00:00.000Z" {
		t.Errorf("primo movimento: %+v", rows[0])
	}
	got := map[string]bool{}
	for _, r := range rows[1:] {
		got[r.Member+"/"+r.Kind] = true
	}
	if !got["1/left"] || !got["3/joined"] {
		t.Errorf("movimenti di oggi: %+v", rows[1:])
	}

	// Fatto: la seconda volta non fa niente, nemmeno con altri snapshot.
	if _, err := app.DB().NewQuery(`DELETE FROM ` + memberchanges.Table).Execute(); err != nil {
		t.Fatal(err)
	}
	if err := memberchanges.BackfillFrom(app, src); err != nil {
		t.Fatal(err)
	}
	n, _ := app.CountRecords(memberchanges.Table, dbx.NewExp("1=1"))
	if n != 0 {
		t.Errorf("il secondo backfill ha scritto %d righe", n)
	}
}
