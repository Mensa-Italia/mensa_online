package migrations

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"mensadb/tools/leaderboard"
	"mensadb/tools/medals"

	"github.com/pocketbase/pocketbase/core"
)

// schemaEntry e' la view_medals dell'export di produzione.
func schemaEntry(t *testing.T, name string) map[string]any {
	t.Helper()
	var collections []map[string]any
	if err := json.Unmarshal(productionSchema, &collections); err != nil {
		t.Fatal(err)
	}
	for _, c := range collections {
		if c["name"] == name {
			return c
		}
	}
	t.Fatalf("%s non e' nello schema di produzione", name)
	return nil
}

// Su un DB nuovo view_medals e' una tabella con gli stessi campi, le
// stesse regole e lo stesso id-socio della vista che era nell'export, piu'
// lo stato delle notifiche (solo superuser).
func TestMedalsTableReplacesView(t *testing.T) {
	app := newMigratedApp(t)

	c, err := app.FindCollectionByNameOrId(medals.Table)
	if err != nil {
		t.Fatal(err)
	}
	if c.IsView() || c.Type != core.CollectionTypeBase {
		t.Fatalf("%s e' ancora una vista", medals.Table)
	}

	// Regole verbatim, spazi compresi quelli a capo dei due testi dell'export:
	// si confrontano le due regole normalizzate, che sono la stessa.
	entry := schemaEntry(t, medals.Table)
	norm := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	for name, got := range map[string]*string{"listRule": c.ListRule, "viewRule": c.ViewRule} {
		want, _ := entry[name].(string)
		if got == nil || norm(*got) != norm(want) {
			t.Errorf("%s: %v, attesa %q", name, got, want)
		}
	}
	if c.CreateRule != nil || c.UpdateRule != nil || c.DeleteRule != nil {
		t.Error("scrittura solo superuser")
	}

	// Gli stessi campi dell'export, con il tipo dichiarato (le date che la
	// vista dichiarava autodate o json sono date).
	family := map[string]string{
		core.FieldTypeText: "text", core.FieldTypeNumber: "number", core.FieldTypeBool: "bool",
		core.FieldTypeDate: "date", core.FieldTypeAutodate: "date", core.FieldTypeJSON: "date",
	}
	fields, _ := entry["fields"].([]any)
	if len(fields) != 34 {
		t.Fatalf("l'export ha %d campi, attesi 34", len(fields))
	}
	for _, raw := range fields {
		f, _ := raw.(map[string]any)
		name, _ := f["name"].(string)
		if name == "id" {
			continue
		}
		got := c.Fields.GetByName(name)
		if got == nil {
			t.Errorf("campo %s mancante", name)
			continue
		}
		if want := family[f["type"].(string)]; family[got.Type()] != want {
			t.Errorf("campo %s: tipo %s, atteso %s", name, got.Type(), want)
		}
		if num, ok := got.(*core.NumberField); ok && !num.OnlyInt {
			t.Errorf("campo %s: i contatori sono interi", name)
		}
	}
	if n := len(c.Fields); n != 34 {
		t.Errorf("%d campi, attesi 34 (id compreso)", n)
	}

	id, ok := c.Fields.GetByName("id").(*core.TextField)
	if !ok || id.AutogeneratePattern != "" || id.Min != 1 || id.Max != 30 {
		t.Errorf("l'id deve essere libero (id del socio): %+v", id)
	}

	state, err := app.FindCollectionByNameOrId(medals.StateTable)
	if err != nil {
		t.Fatal(err)
	}
	if state.ListRule != nil || state.ViewRule != nil {
		t.Error("lo stato delle notifiche e' solo superuser")
	}

	// Idempotente.
	if err := upMedalsTable(app); err != nil {
		t.Fatalf("seconda up: %v", err)
	}
}

// Lo schema di produzione contiene ancora la vista: su un DB migrato non
// deve essere reimportata al posto della tabella.
func TestProductionSchemaSkipsMedalsView(t *testing.T) {
	app := newMigratedApp(t)
	toImport, err := collectionsToImport(app, productionSchema)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range toImport {
		if c["name"] == medals.Table {
			t.Errorf("%v verrebbe reimportata come vista", c["name"])
		}
	}
}

func addUser(t *testing.T, app core.App, id string, active bool, created time.Time) {
	t.Helper()
	users, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatal(err)
	}
	r := core.NewRecord(users)
	r.Id = id
	r.SetEmail(id + "@example.test")
	r.SetPassword("password-di-prova")
	r.Set("is_membership_active", active)
	if !created.IsZero() {
		r.SetRaw("created", created.UTC().Format("2006-01-02 15:04:05.000Z"))
	}
	if err := app.SaveNoValidate(r); err != nil {
		t.Fatalf("users/%s: %v", id, err)
	}
}

func saveRecord(t *testing.T, app core.App, collection, id string, values map[string]any) {
	t.Helper()
	c, err := app.FindCollectionByNameOrId(collection)
	if err != nil {
		t.Fatal(err)
	}
	r := core.NewRecord(c)
	if id != "" {
		r.Id = id
	}
	for k, v := range values {
		r.Set(k, v)
	}
	if err := app.SaveNoValidate(r); err != nil {
		t.Fatalf("%s: %v", collection, err)
	}
}

func addDevice(t *testing.T, app core.App, user, token string) *core.Record {
	t.Helper()
	devices, err := app.FindCollectionByNameOrId("users_devices")
	if err != nil {
		t.Fatal(err)
	}
	r := core.NewRecord(devices)
	r.Set("user", user)
	r.Set("firebase_id", token)
	if err := app.SaveNoValidate(r); err != nil {
		t.Fatalf("users_devices: %v", err)
	}
	return r
}

func stateTier(t *testing.T, app core.App, user, medal string) (string, bool) {
	t.Helper()
	rows, err := app.FindRecordsByFilter(medals.StateTable, "user = {:u} && medal = {:m}", "", 1, 0,
		map[string]any{"u": user, "m": medal})
	if err != nil || len(rows) == 0 {
		return "", false
	}
	return rows[0].GetString("tier"), true
}

// Ricalcolo sullo schema vero, dopo tutte le migrazioni: la riga del socio
// esce con i suoi contatori, il primo giro semina lo stato in silenzio, un
// contatore che sale da' esattamente un avanzamento, uno che scende
// nessuno, e il socio sparito perde riga e stato.
func TestMedalsRefreshTracksTiers(t *testing.T) {
	app := newMigratedApp(t)

	var got []medals.Change
	original := medals.AfterRefresh
	medals.AfterRefresh = func(_ core.App, changes []medals.Change) { got = append(got, changes...) }
	t.Cleanup(func() { medals.AfterRefresh = original })
	take := func() []medals.Change { out := got; got = nil; return out }

	addUser(t, app, "1001", true, time.Now().AddDate(-3, 0, -5))
	addUser(t, app, "1002", false, time.Time{})
	addDevice(t, app, "1001", "tok-1")

	started := time.Now()
	if err := medals.Refresh(app); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	t.Logf("primo refresh (query completa di view_medals, 2 soci su DB di prova): %s", time.Since(started))
	if c := take(); len(c) != 0 {
		t.Fatalf("il primo giro semina in silenzio, trovati %+v", c)
	}

	row, err := app.FindRecordById(medals.Table, "1001")
	if err != nil {
		t.Fatalf("riga del socio: %v", err)
	}
	if !row.GetBool("is_membership_active") || row.GetInt("devices") != 1 || row.GetInt("stamps_total") != 0 ||
		row.GetDateTime("app_since").IsZero() || row.GetBool("calendar_linked") {
		t.Errorf("contatori inattesi: %v", row.PublicExport())
	}
	if _, err := app.FindRecordById(medals.Table, "1002"); err != nil {
		t.Errorf("riga dell'altro socio: %v", err)
	}
	for user, want := range map[string]map[string]string{
		"1001": {"active_member": "platinum", "early_adopter": "gold", "multiscreen": "none", "collector": "none"},
		"1002": {"active_member": "none", "early_adopter": "none"},
	} {
		for medal, tier := range want {
			if got, ok := stateTier(t, app, user, medal); !ok || got != tier {
				t.Errorf("stato %s/%s: %q (presente %v), atteso %q", user, medal, got, ok, tier)
			}
		}
	}
	var count int
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM ` + medals.StateTable).Row(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2*len(medals.Rules) {
		t.Errorf("%d righe di stato, attese %d", count, 2*len(medals.Rules))
	}

	// Un secondo giro senza novita' non cambia niente.
	if err := medals.Refresh(app); err != nil {
		t.Fatal(err)
	}
	if c := take(); len(c) != 0 {
		t.Fatalf("giro identico: %+v", c)
	}

	// Un secondo dispositivo: Multischermo (livello unico) per il socio 1001.
	second := addDevice(t, app, "1001", "tok-2")
	if err := medals.Refresh(app); err != nil {
		t.Fatal(err)
	}
	c := take()
	want := medals.Change{UserID: "1001", Medal: "multiscreen", Title: "Multischermo", From: medals.TierNone, To: medals.TierPlatinum}
	if len(c) != 1 || c[0] != want {
		t.Fatalf("avanzamenti: %+v, atteso solo %+v", c, want)
	}

	// Il contatore scende: lo stato tiene il metallo piu' alto raggiunto,
	// nessun avviso...
	if err := app.Delete(second); err != nil {
		t.Fatal(err)
	}
	if err := medals.Refresh(app); err != nil {
		t.Fatal(err)
	}
	if c := take(); len(c) != 0 {
		t.Fatalf("un contatore che scende non avvisa: %+v", c)
	}
	if tier, _ := stateTier(t, app, "1001", "multiscreen"); tier != "platinum" {
		t.Errorf("stato dopo il calo: %q, atteso platinum (il massimo non scende)", tier)
	}
	// ...e se risale non si annuncia una seconda volta.
	addDevice(t, app, "1001", "tok-3")
	if err := medals.Refresh(app); err != nil {
		t.Fatal(err)
	}
	if c := take(); len(c) != 0 {
		t.Fatalf("risalita a un metallo gia' annunciato: %+v", c)
	}

	// Un timbro che conta (un solo socio attivo nella regione, un solo
	// timbratore): il contatore dei timbri sale, la data del primo timbro
	// si legge come data, e Collezionista passa da none a bronzo.
	saveRecord(t, app, "members_registry", "1001", map[string]any{"area": "Lombardia", "is_active": true})
	saveRecord(t, app, "positions", "pos1", map[string]any{"name": "Sala", "state": "Lombardia"})
	saveRecord(t, app, "events", "ev1", map[string]any{"name": "Cena", "position": "pos1", "owner": "1002", "when_start": "2026-10-30 18:00:00.000Z"})
	saveRecord(t, app, "stamp", "st1", map[string]any{"description": "Cena"})
	saveRecord(t, app, "stamp_users", "", map[string]any{"user": "1001", "stamp": "st1"})
	if err := medals.Refresh(app); err != nil {
		t.Fatal(err)
	}
	row, err = app.FindRecordById(medals.Table, "1001")
	if err != nil {
		t.Fatal(err)
	}
	if row.GetInt("stamps_total") != 1 || row.GetDateTime("first_stamp_at").IsZero() {
		t.Fatalf("contatori dei timbri: %v", row.PublicExport())
	}
	found := false
	for _, ch := range take() {
		if ch.To == medals.TierNone || ch.To <= ch.From {
			t.Errorf("avanzamento inatteso: %+v", ch)
		}
		if ch.UserID == "1001" && ch.Medal == "collector" && ch.From == medals.TierNone && ch.To == medals.TierBronze {
			found = true
		}
	}
	if !found {
		t.Error("manca l'avanzamento di Collezionista")
	}

	// Una medaglia senza riga di stato (aggiunta dopo alle regole) per un
	// socio che ha gia' altre righe si semina in silenzio.
	stale, err := app.FindFirstRecordByFilter(medals.StateTable, "user = '1001' && medal = 'multiscreen'")
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Delete(stale); err != nil {
		t.Fatal(err)
	}
	if err := medals.Refresh(app); err != nil {
		t.Fatal(err)
	}
	if c := take(); len(c) != 0 {
		t.Fatalf("una riga nuova non avvisa: %+v", c)
	}
	if tier, ok := stateTier(t, app, "1001", "multiscreen"); !ok || tier != "platinum" {
		t.Errorf("riga seminata: %q (presente %v), attesa platinum", tier, ok)
	}

	// Un socio che non c'e' piu' perde riga e stato.
	gone, _ := app.FindRecordById("users", "1002")
	if err := app.Delete(gone); err != nil {
		t.Fatal(err)
	}
	if err := medals.Refresh(app); err != nil {
		t.Fatal(err)
	}
	if _, err := app.FindRecordById(medals.Table, "1002"); err == nil {
		t.Error("la riga del socio uscito deve sparire")
	}
	if _, ok := stateTier(t, app, "1002", "active_member"); ok {
		t.Error("lo stato del socio uscito deve sparire")
	}

	// La classifica soci legge la tabella e riparte senza errori.
	if err := leaderboard.Refresh(app); err != nil {
		t.Fatalf("classifica sulla tabella: %v", err)
	}
}
