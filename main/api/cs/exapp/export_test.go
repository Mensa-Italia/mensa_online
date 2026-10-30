package exapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	_ "mensadb/migrations"
	"mensadb/tools/dbtools"
	"mensadb/tools/medals"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
)

// Questi test usano un DB PocketBase vero (migrazioni incluse): la macchina a
// stati vive in UPDATE condizionali, e provarla su un finto non proverebbe
// niente. Con Go 1.27 serve GOEXPERIMENT=nojsonv2 (vedi migrations).

type fixture struct {
	app    core.App
	pushes []dbtools.PushNotification
	mu     sync.Mutex

	userA, userB *core.Record
	appA, appB   *core.Record

	keyAStamps, keyAMedals, keyANone, keyBStamps string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.ResetBootstrapState() })
	if err := app.RunAllMigrations(); err != nil {
		t.Fatal(err)
	}

	f := &fixture{app: app}

	oldPush, oldNow := sendPush, nowFunc
	sendPush = func(_ core.App, p dbtools.PushNotification) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.pushes = append(f.pushes, p)
	}
	t.Cleanup(func() { sendPush, nowFunc = oldPush, oldNow })

	users, _ := app.FindCollectionByNameOrId("users")
	mkUser := func(id, email string) *core.Record {
		r := core.NewRecord(users)
		r.Id = id
		r.SetEmail(email)
		r.SetPassword("password-di-prova")
		r.Set("username", "user_"+id)
		r.Set("name", "Utente "+id)
		if err := app.Save(r); err != nil {
			t.Fatal(err)
		}
		return r
	}
	f.userA = mkUser("usera0000000001", "Mario.Rossi@example.com")
	f.userB = mkUser("userb0000000001", "altro@example.com")

	exApps, _ := app.FindCollectionByNameOrId("ex_apps")
	mkApp := func(id, name string) *core.Record {
		r := core.NewRecord(exApps)
		r.Id = id
		r.Set("name", name)
		if err := app.Save(r); err != nil {
			t.Fatal(err)
		}
		return r
	}
	f.appA = mkApp("appa00000000001", "App Alfa")
	f.appB = mkApp("appb00000000001", "App Beta")

	keys, _ := app.FindCollectionByNameOrId("ex_keys")
	mkKey := func(appRec *core.Record, key, perm string) string {
		r := core.NewRecord(keys)
		r.Set("ex_app", appRec.Id)
		r.Set("key", key)
		r.Set("permissions", perm)
		if err := app.Save(r); err != nil {
			t.Fatal(err)
		}
		return "Bearer " + key
	}
	f.keyAStamps = mkKey(f.appA, "sk_a_stamps", "EXPORT_STAMPS")
	f.keyAMedals = mkKey(f.appA, "sk_a_medals", "EXPORT_MEDALS")
	f.keyANone = mkKey(f.appA, "sk_a_none", "CHECK_USER_EXISTENCE")
	f.keyBStamps = mkKey(f.appB, "sk_b_stamps", "EXPORT_STAMPS")

	return f
}

// call esegue un handler con un RequestEvent costruito a mano.
func (f *fixture) call(t *testing.T, h func(*core.RequestEvent) error, key string, auth *core.Record, body map[string]any) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/cs/exapp/export/x", strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", key)
	}
	rec := httptest.NewRecorder()
	e := new(core.RequestEvent)
	e.App = f.app
	e.Request = req
	e.Response = rec
	e.Auth = auth
	if err := h(e); err != nil {
		t.Fatalf("handler ha ritornato un errore: %v", err)
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func (f *fixture) request(t *testing.T, key, scope string) (int, map[string]any) {
	return f.call(t, exportRequestHandler, key, nil, map[string]any{
		"member_id": f.userA.Id, "email": "mario.rossi@EXAMPLE.com",
		"callme_url": "https://partner.example/cb", "scope": scope,
	})
}

func (f *fixture) decide(t *testing.T, user *core.Record, id string, accepted bool) (int, map[string]any) {
	return f.call(t, exportDecideHandler, "", user, map[string]any{"request_id": id, "accepted": accepted})
}

func (f *fixture) fetch(t *testing.T, key, id string) (int, map[string]any) {
	return f.call(t, exportFetchHandler, key, nil, map[string]any{"request_id": id})
}

func (f *fixture) status(t *testing.T, id string) string {
	t.Helper()
	r, err := f.app.FindRecordById(exportRequestsCollection, id)
	if err != nil {
		t.Fatal(err)
	}
	return r.GetString("status")
}

// newApproved crea e approva una richiesta di stamps per userA.
func (f *fixture) newApproved(t *testing.T, scope string, key string) string {
	t.Helper()
	code, out := f.request(t, key, scope)
	if code != 200 {
		t.Fatalf("request: %d %v", code, out)
	}
	id, _ := out["request_id"].(string)
	if code, out := f.decide(t, f.userA, id, true); code != 200 || out["status"] != "approved" {
		t.Fatalf("decide: %d %v", code, out)
	}
	return id
}

func TestExportRequest(t *testing.T) {
	f := newFixture(t)
	start := time.Now()

	code, out := f.request(t, f.keyAStamps, "stamps")
	if code != 200 || out["expires_in"] != float64(600) || out["request_id"] == "" {
		t.Fatalf("request: %d %v", code, out)
	}
	id := out["request_id"].(string)

	rec, _ := f.app.FindRecordById(exportRequestsCollection, id)
	if rec.GetString("status") != "pending" || rec.GetString("scope") != "stamps" ||
		rec.GetString("ex_app") != f.appA.Id || rec.GetString("user") != f.userA.Id ||
		rec.GetString("callme_url") != "https://partner.example/cb" {
		t.Errorf("record inatteso: %v", rec.PublicExport())
	}
	if d := rec.GetDateTime("expires_at").Time().Sub(start); d < 9*time.Minute || d > 11*time.Minute {
		t.Errorf("scadenza a %s, attesi ~10 minuti", d)
	}

	if len(f.pushes) != 1 {
		t.Fatalf("push inviate: %d", len(f.pushes))
	}
	p := f.pushes[0]
	if p.UserId != f.userA.Id || p.TrTag != "push_notification.confirm_data_export" ||
		p.TrNamedParams["name"] != "App Alfa" || p.TrNamedParams["scope"] != "stamps" {
		t.Errorf("push: %+v", p)
	}
	want := map[string]string{
		"type": "data_export_confirmation", "requestId": id, "keyAppId": f.appA.Id,
		"scope": "stamps", "url": "https://partner.example/cb",
	}
	for k, v := range want {
		if p.Data[k] != v {
			t.Errorf("push data[%s] = %q, atteso %q", k, p.Data[k], v)
		}
	}
}

func TestExportRequestRejections(t *testing.T) {
	f := newFixture(t)
	base := map[string]any{
		"member_id": f.userA.Id, "email": "mario.rossi@example.com",
		"callme_url": "https://partner.example/cb", "scope": "stamps",
	}
	with := func(k string, v any) map[string]any {
		m := map[string]any{}
		for kk, vv := range base {
			m[kk] = vv
		}
		m[k] = v
		return m
	}

	cases := []struct {
		name string
		key  string
		body map[string]any
		want int
	}{
		{"senza chiave", "", base, 401},
		{"chiave inesistente", "Bearer sk_boh", base, 401},
		{"chiave senza il permesso dello scope", f.keyAMedals, base, 401},
		{"chiave con altro permesso", f.keyANone, base, 401},
		{"scope sconosciuto", f.keyAStamps, with("scope", "tutto"), 400},
		{"scope vuoto", f.keyAStamps, with("scope", ""), 400},
		{"email sbagliata", f.keyAStamps, with("email", "x@example.com"), 400},
		{"utente inesistente", f.keyAStamps, with("member_id", "nonesiste"), 400},
		{"url non valido", f.keyAStamps, with("callme_url", "javascript:alert(1)"), 400},
		{"url mancante", f.keyAStamps, with("callme_url", ""), 400},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, _ := f.call(t, exportRequestHandler, c.key, nil, c.body)
			if code != c.want {
				t.Fatalf("status %d, atteso %d", code, c.want)
			}
		})
	}
	if len(f.pushes) != 0 {
		t.Errorf("nessun rifiuto deve mandare push, inviate %d", len(f.pushes))
	}

	// La stessa chiave con lo scope giusto passa: timbri e medaglie sono permessi distinti.
	if code, _ := f.request(t, f.keyAMedals, "medals"); code != 200 {
		t.Errorf("EXPORT_MEDALS su medals: %d", code)
	}
	if code, _ := f.request(t, f.keyAStamps, "medals"); code != 401 {
		t.Errorf("EXPORT_STAMPS su medals: %d", code)
	}
}

// Una nuova richiesta per la stessa terna chiude la precedente ancora pending.
func TestExportRequestSupersedesPending(t *testing.T) {
	f := newFixture(t)
	_, first := f.request(t, f.keyAStamps, "stamps")
	_, second := f.request(t, f.keyAStamps, "stamps")
	if f.status(t, first["request_id"].(string)) != "expired" {
		t.Error("la prima richiesta doveva risultare expired")
	}
	if f.status(t, second["request_id"].(string)) != "pending" {
		t.Error("la seconda deve restare pending")
	}
}

func TestExportDecide(t *testing.T) {
	f := newFixture(t)
	_, out := f.request(t, f.keyAStamps, "stamps")
	id := out["request_id"].(string)

	if code, _ := f.decide(t, f.userB, id, true); code != 404 {
		t.Errorf("richiesta di un altro utente: %d, atteso 404", code)
	}
	if code, _ := f.decide(t, f.userA, "inesistente", true); code != 404 {
		t.Errorf("richiesta inesistente: %d, atteso 404", code)
	}
	if code, _ := f.call(t, exportDecideHandler, "", f.userA, map[string]any{"accepted": true}); code != 400 {
		t.Errorf("senza request_id: %d, atteso 400", code)
	}
	if f.status(t, id) != "pending" {
		t.Fatal("i rifiuti non devono toccare lo stato")
	}

	if code, out := f.decide(t, f.userA, id, true); code != 200 || out["status"] != "approved" {
		t.Fatalf("approvazione: %d %v", code, out)
	}
	if code, _ := f.decide(t, f.userA, id, false); code != 409 {
		t.Errorf("seconda decisione: %d, atteso 409", code)
	}
	if f.status(t, id) != "approved" {
		t.Error("la seconda decisione non deve cambiare lo stato")
	}
}

func TestExportDecideDenied(t *testing.T) {
	f := newFixture(t)
	_, out := f.request(t, f.keyAStamps, "stamps")
	id := out["request_id"].(string)

	if code, out := f.decide(t, f.userA, id, false); code != 200 || out["status"] != "denied" {
		t.Fatalf("rifiuto: %d %v", code, out)
	}
	if code, _ := f.fetch(t, f.keyAStamps, id); code != 409 {
		t.Errorf("fetch di una denied: %d, atteso 409", code)
	}
	if code, _ := f.decide(t, f.userA, id, true); code != 409 {
		t.Errorf("approvare una denied: %d, atteso 409", code)
	}
	if f.status(t, id) != "denied" {
		t.Error("deve restare denied")
	}
}

// Percorso completo timbri: pending -> approved -> consumed, poi 409.
func TestExportStampsFlow(t *testing.T) {
	f := newFixture(t)

	stampColl, _ := f.app.FindCollectionByNameOrId("stamp")
	suColl, _ := f.app.FindCollectionByNameOrId("stamp_users")
	mkStamp := func(id, desc, image string) *core.Record {
		r := core.NewRecord(stampColl)
		r.Id = id
		r.Set("description", desc)
		if image != "" {
			file, err := filesystem.NewFileFromBytes([]byte("png"), image)
			if err != nil {
				t.Fatal(err)
			}
			r.Set("image", file)
		}
		if err := f.app.Save(r); err != nil {
			t.Fatal(err)
		}
		return r
	}
	s1 := mkStamp("stamp0000000001", "Raduno 2026", "pic.png")
	s2 := mkStamp("stamp0000000002", "Senza immagine", "")
	for _, s := range []*core.Record{s1, s2} {
		for _, u := range []*core.Record{f.userA, f.userB} {
			r := core.NewRecord(suColl)
			r.Set("stamp", s.Id)
			r.Set("user", u.Id)
			if err := f.app.Save(r); err != nil {
				t.Fatal(err)
			}
		}
	}

	_, out := f.request(t, f.keyAStamps, "stamps")
	id := out["request_id"].(string)

	if code, _ := f.fetch(t, f.keyAStamps, id); code != 409 {
		t.Fatalf("fetch di una pending: %d, atteso 409", code)
	}
	f.decide(t, f.userA, id, true)

	code, payload := f.fetch(t, f.keyAStamps, id)
	if code != 200 {
		t.Fatalf("fetch: %d %v", code, payload)
	}
	if payload["member_id"] != f.userA.Id || payload["scope"] != "stamps" || payload["exported_at"] == "" {
		t.Errorf("intestazione: %v", payload)
	}
	list, _ := payload["stamps"].([]any)
	if len(list) != 2 {
		t.Fatalf("timbri esportati: %d, attesi 2 (solo quelli di userA)", len(list))
	}
	byID := map[string]map[string]any{}
	for _, it := range list {
		m := it.(map[string]any)
		byID[m["stamp_id"].(string)] = m
		if m["obtained_at"] == "" {
			t.Errorf("obtained_at mancante: %v", m)
		}
	}
	wantURL := "https://svc.mensa.it/api/files/" + stampColl.Id + "/" + s1.Id + "/" + s1.GetString("image")
	if byID[s1.Id]["image_url"] != wantURL || byID[s1.Id]["description"] != "Raduno 2026" {
		t.Errorf("timbro 1: %v (url atteso %s)", byID[s1.Id], wantURL)
	}
	if byID[s2.Id]["image_url"] != "" {
		t.Errorf("timbro senza immagine: %v", byID[s2.Id])
	}
	if f.status(t, id) != "consumed" {
		t.Errorf("stato %s, atteso consumed", f.status(t, id))
	}
	if rec, _ := f.app.FindRecordById(exportRequestsCollection, id); rec.GetDateTime("consumed_at").IsZero() {
		t.Error("consumed_at non valorizzato")
	}

	// Seconda estrazione: consumata.
	if code, _ := f.fetch(t, f.keyAStamps, id); code != 409 {
		t.Errorf("secondo fetch: %d, atteso 409", code)
	}
}

func TestExportMedalsFlow(t *testing.T) {
	f := newFixture(t)
	// view_medals e' una tabella riempita dal cron: i soci della fixture
	// ci sono dopo il primo ricalcolo.
	if err := medals.Refresh(f.app); err != nil {
		t.Fatalf("ricalcolo medagliere: %v", err)
	}
	id := f.newApproved(t, "medals", f.keyAMedals)

	// la chiave dei timbri non ritira le medaglie
	if code, _ := f.fetch(t, f.keyAStamps, id); code != 401 {
		t.Fatalf("chiave EXPORT_STAMPS su richiesta medals: %d, atteso 401", code)
	}
	if f.status(t, id) != "approved" {
		t.Fatal("un 401 non deve consumare la richiesta")
	}

	code, payload := f.fetch(t, f.keyAMedals, id)
	if code != 200 || payload["scope"] != "medals" || payload["member_id"] != f.userA.Id {
		t.Fatalf("fetch: %d %v", code, payload)
	}
	medals, ok := payload["medals"].(map[string]any)
	if !ok || medals["id"] != f.userA.Id {
		t.Fatalf("riga view_medals: %v", payload["medals"])
	}
	if _, ok := medals["stamps_total"]; !ok {
		t.Errorf("mancano i contatori: %v", medals)
	}
}

func TestExportFetchAccessControl(t *testing.T) {
	f := newFixture(t)
	id := f.newApproved(t, "stamps", f.keyAStamps)

	if code, _ := f.fetch(t, "", id); code != 401 {
		t.Errorf("senza chiave: %d, atteso 401", code)
	}
	if code, _ := f.fetch(t, "Bearer sk_boh", id); code != 401 {
		t.Errorf("chiave inesistente: %d, atteso 401", code)
	}
	if code, _ := f.fetch(t, f.keyANone, id); code != 401 {
		t.Errorf("chiave senza permesso export: %d, atteso 401", code)
	}
	if code, _ := f.fetch(t, f.keyBStamps, id); code != 404 {
		t.Errorf("chiave di un altro ex_app: %d, atteso 404", code)
	}
	if code, _ := f.fetch(t, f.keyAStamps, "inesistente"); code != 404 {
		t.Errorf("richiesta inesistente: %d, atteso 404", code)
	}
	if code, _ := f.call(t, exportFetchHandler, f.keyAStamps, nil, map[string]any{}); code != 400 {
		t.Errorf("senza request_id: %d, atteso 400", code)
	}
	if f.status(t, id) != "approved" {
		t.Fatal("i rifiuti non devono consumare la richiesta")
	}
	if code, _ := f.fetch(t, f.keyAStamps, id); code != 200 {
		t.Errorf("la chiave giusta deve ancora poter ritirare: %d", code)
	}
}

// Scadenza pigra: pending non decisa, approved non ritirata, decisione tardiva.
func TestExportExpiry(t *testing.T) {
	f := newFixture(t)
	clock := time.Now()
	nowFunc = func() time.Time { return clock }

	// approved ma mai ritirata
	approved := f.newApproved(t, "stamps", f.keyAStamps)
	// pending mai decisa
	_, out := f.request(t, f.keyAMedals, "medals")
	pending := out["request_id"].(string)

	clock = clock.Add(exportTTL + time.Second)

	if code, _ := f.fetch(t, f.keyAStamps, approved); code != 410 {
		t.Errorf("fetch di una approved scaduta: %d, atteso 410", code)
	}
	if f.status(t, approved) != "expired" {
		t.Errorf("stato %s, atteso expired (scadenza pigra)", f.status(t, approved))
	}
	if code, _ := f.decide(t, f.userA, pending, true); code != 409 {
		t.Errorf("decide su una scaduta: %d, atteso 409", code)
	}
	if f.status(t, pending) != "expired" {
		t.Errorf("stato %s, atteso expired", f.status(t, pending))
	}
	if code, _ := f.fetch(t, f.keyAMedals, pending); code != 410 {
		t.Errorf("fetch di una scaduta: %d, atteso 410", code)
	}
}

// La scadenza e` quella della richiesta: l'approvazione non la allunga.
func TestExportApprovalDoesNotExtendExpiry(t *testing.T) {
	f := newFixture(t)
	clock := time.Now()
	nowFunc = func() time.Time { return clock }

	_, out := f.request(t, f.keyAStamps, "stamps")
	id := out["request_id"].(string)
	clock = clock.Add(9 * time.Minute)
	f.decide(t, f.userA, id, true)
	clock = clock.Add(2 * time.Minute) // 11 minuti dalla creazione
	if code, _ := f.fetch(t, f.keyAStamps, id); code != 410 {
		t.Errorf("fetch oltre expires_at: %d, atteso 410", code)
	}
}

// Una sola estrazione anche con chiamate concorrenti.
func TestExportFetchConcurrent(t *testing.T) {
	f := newFixture(t)
	id := f.newApproved(t, "stamps", f.keyAStamps)

	const n = 24
	codes := make([]int, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			codes[i], _ = f.fetchConcurrent(id)
		}(i)
	}
	close(start)
	wg.Wait()

	ok, conflict := 0, 0
	for _, c := range codes {
		switch c {
		case 200:
			ok++
		case 409:
			conflict++
		default:
			t.Errorf("codice inatteso %d", c)
		}
	}
	if ok != 1 || conflict != n-1 {
		t.Fatalf("200: %d, 409: %d (attesi 1 e %d)", ok, conflict, n-1)
	}
	if f.status(t, id) != "consumed" {
		t.Errorf("stato finale %s", f.status(t, id))
	}
}

// fetchConcurrent e` fetch senza t.Fatalf: si usa da goroutine diverse dal test.
func (f *fixture) fetchConcurrent(id string) (int, map[string]any) {
	raw, _ := json.Marshal(map[string]any{"request_id": id})
	req := httptest.NewRequest(http.MethodPost, "/api/cs/exapp/export/fetch", strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", f.keyAStamps)
	rec := httptest.NewRecorder()
	e := new(core.RequestEvent)
	e.App = f.app
	e.Request = req
	e.Response = rec
	if err := exportFetchHandler(e); err != nil {
		return 0, nil
	}
	return rec.Code, nil
}

func TestCleanupExportRequests(t *testing.T) {
	f := newFixture(t)
	clock := time.Now()
	nowFunc = func() time.Time { return clock }

	stale := f.newApproved(t, "stamps", f.keyAStamps) // approved che scade
	_, out := f.request(t, f.keyAMedals, "medals")
	denied := out["request_id"].(string)
	f.decide(t, f.userA, denied, false)
	_, out = f.request(t, f.keyAStamps, "stamps")
	fresh := out["request_id"].(string)

	clock = clock.Add(exportTTL + time.Minute)
	// `fresh` e` della stessa terna di `stale`: ora e` scaduta anche lei.
	expired, deleted, err := CleanupExportRequests(f.app)
	if err != nil {
		t.Fatal(err)
	}
	if expired != 2 || deleted != 0 {
		t.Errorf("expired=%d deleted=%d, attesi 2 e 0", expired, deleted)
	}
	for _, id := range []string{stale, fresh} {
		if f.status(t, id) != "expired" {
			t.Errorf("%s non e` expired", id)
		}
	}
	if f.status(t, denied) != "denied" {
		t.Error("una denied non deve cambiare")
	}

	// Oltre la retention le concluse spariscono.
	clock = clock.Add(exportRetention + time.Hour)
	_, deleted, err = CleanupExportRequests(f.app)
	if err != nil || deleted != 3 {
		t.Errorf("deleted=%d err=%v, attese 3", deleted, err)
	}
}

func TestPermissionForScope(t *testing.T) {
	if permissionForScope("stamps") != "EXPORT_STAMPS" || permissionForScope("medals") != "EXPORT_MEDALS" || permissionForScope("x") != "" {
		t.Error("mappa scope -> permesso errata")
	}
}

func TestValidCallmeURL(t *testing.T) {
	for in, want := range map[string]bool{
		"https://partner.example/cb": true, "http://localhost:3000/cb?x=1": true,
		"": false, "ftp://x/y": false, "javascript:alert(1)": false, "/relativo": false, "https://": false,
		"https://x/" + strings.Repeat("a", 2100): false,
	} {
		if validCallmeURL(in) != want {
			t.Errorf("validCallmeURL(%q) != %v", in, want)
		}
	}
}
