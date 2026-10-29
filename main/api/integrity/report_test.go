package integrity

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
	"github.com/pocketbase/pocketbase/tools/router"

	tools "mensadb/tools/integrity"
)

// reportApp e` un'app che scrive i log in un buffer, cosi' un test legge le
// righe che il server scriverebbe in _logs. Sotto c'e` una BaseApp NON avviata:
// qualunque accesso al DB (Save, FindRecord...) fallirebbe, e il report non ne
// deve fare.
type reportApp struct {
	core.App
	logger *slog.Logger
	buf    *bytes.Buffer
}

func newReportApp(t *testing.T) *reportApp {
	t.Helper()
	buf := new(bytes.Buffer)
	return &reportApp{
		App:    core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()}),
		logger: slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})),
		buf:    buf,
	}
}

func (a *reportApp) Logger() *slog.Logger { return a.logger }

// lines ritorna le righe di log scritte finora.
func (a *reportApp) lines(t *testing.T) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(a.buf.String()), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("riga di log non JSON: %q", l)
		}
		out = append(out, m)
	}
	return out
}

// reports ritorna solo le righe del report.
func (a *reportApp) reports(t *testing.T) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, m := range a.lines(t) {
		if m["msg"] == reportMessage {
			out = append(out, m)
		}
	}
	return out
}

type reportResult struct {
	reached bool
	status  int
	raw     string
	body    map[string]any
	header  http.Header
}

// serve fa passare la richiesta dal middleware del gate e, se passa, da
// reportHandler: la strada che fa in produzione.
func serve(t *testing.T, app *reportApp, gate *tools.Gate, auth *core.Record, method, path, body string, headers map[string]string) reportResult {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e := new(core.RequestEvent)
	e.App, e.Request, e.Response, e.Auth = app, req, rec, auth

	h := &hook.Hook[*core.RequestEvent]{}
	h.Bind(Middleware(gate))
	res := reportResult{}
	err := h.Trigger(e, func(e *core.RequestEvent) error {
		res.reached = true
		if method == http.MethodPost && path == "/api/integrity/report" {
			return reportHandler(e)
		}
		return nil
	})
	if err != nil {
		// un errore del handler (413, 400...) lo trasforma in risposta il router di PocketBase
		var ae *router.ApiError
		if !errors.As(err, &ae) {
			t.Fatalf("errore non HTTP: %v", err)
		}
		res.status, res.raw = ae.Status, ae.Message
		return res
	}
	res.status = rec.Code
	res.header = rec.Header()
	res.raw = strings.TrimSpace(rec.Body.String()) // e.JSON chiude con un a capo
	_ = json.Unmarshal(rec.Body.Bytes(), &res.body)
	return res
}

const validReport = `{"platform":"ios","stage":"attest_key","domain":"com.apple.devicecheck.error","code":2,` +
	`"detail":"boom","underlying":"NSOSStatusErrorDomain/-1","disposition":"discard_key","outcome":"failed",` +
	`"attempt":1,"key_phase":"pending","key_age_s":0,"elapsed_ms":150,"app":"31000984","os":"iOS 27.0.1","ios_app_on_mac":true}`

func reportGate(t *testing.T) *tools.Gate {
	t.Helper()
	gate := newTestGate()
	gate.Sessions = tools.NewSessionStore()
	gate.Reports = tools.NewUserLimiter(tools.ReportsPerHourPerUser, time.Hour)
	current.Store(gate)
	t.Cleanup(func() { current.Store(nil) })
	return gate
}

const reportPath = "/api/integrity/report"

func TestReportLogsTheCleanedReport(t *testing.T) {
	app, gate := newReportApp(t), reportGate(t)
	res := serve(t, app, gate, userRecord("u1", "watched"), "POST", reportPath, validReport, nil)
	if !res.reached || res.status != 200 || res.raw != `{"ok":true}` {
		t.Fatalf("risposta: reached=%v %d %q", res.reached, res.status, res.raw)
	}

	logs := app.reports(t)
	if len(logs) != 1 {
		t.Fatalf("attesa UNA riga di report, %d: %s", len(logs), app.buf.String())
	}
	got := logs[0]
	if got["level"] != "WARN" {
		t.Errorf("livello %v", got["level"])
	}
	want := map[string]any{
		"user": "u1", "platform": "ios", "stage": "attest_key", "domain": "com.apple.devicecheck.error",
		"code": float64(2), "detail": "boom", "underlying": "NSOSStatusErrorDomain/-1",
		"disposition": "discard_key", "outcome": "failed", "attempt": float64(1), "key_phase": "pending",
		"key_age_s": float64(0), "elapsed_ms": float64(150), "app": "31000984", "os": "iOS 27.0.1",
		"ios_app_on_mac": true,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v (%T), atteso %v", k, got[k], got[k], v)
		}
	}
	if len(got) != len(want)+3 { // time, level, msg
		t.Errorf("campi inattesi nella riga: %v", got)
	}
}

// L'utente si prende SOLO da e.Auth: quello che il corpo dichiara non conta.
func TestReportUserComesFromAuthNotFromBody(t *testing.T) {
	app, gate := newReportApp(t), reportGate(t)
	body := `{"platform":"ios","stage":"assertion","user":"altro","user_id":"altro","userId":"altro","id":"altro","email":"x@y.z"}`
	res := serve(t, app, gate, userRecord("u1", "watched"), "POST", reportPath, body, nil)
	if res.status != 200 {
		t.Fatalf("%d %s", res.status, res.raw)
	}
	logs := app.reports(t)
	if len(logs) != 1 || logs[0]["user"] != "u1" {
		t.Fatalf("riga di log: %v", logs)
	}
	if strings.Contains(app.buf.String(), "altro") || strings.Contains(app.buf.String(), "x@y.z") {
		t.Fatalf("un campo del corpo che non fa parte del report e` finito nel log: %s", app.buf.String())
	}
}

func TestReportSanitisesWhatItLogs(t *testing.T) {
	app, gate := newReportApp(t), reportGate(t)
	long := strings.Repeat("é", 600)
	body, _ := json.Marshal(map[string]any{
		"platform": "symbian", "stage": "boh", "disposition": "x", "outcome": "y", "key_phase": "z",
		"domain": "dom\nain", "detail": long, "underlying": "a\x00b\u202ec", "app": "\x1b[31m1", "os": "o\r\ns",
		"code": 1 << 40, "attempt": -3, "key_age_s": -50, "elapsed_ms": 1 << 50,
	})
	if len(body) > tools.MaxReportBytes {
		t.Fatalf("il test deve stare nel tetto di %d byte, %d", tools.MaxReportBytes, len(body))
	}
	res := serve(t, app, gate, userRecord("u1", "watched"), "POST", reportPath, string(body), nil)
	if res.status != 200 {
		t.Fatalf("%d %s", res.status, res.raw)
	}
	got := app.reports(t)[0]
	for k, v := range map[string]any{
		"platform": "?", "stage": "other", "disposition": "other", "outcome": "other", "key_phase": "other",
		"domain": "dom ain", "underlying": "a bc", "app": "[31m1", "os": "o  s",
		"code": float64(1<<31 - 1), "attempt": float64(0), "key_age_s": float64(-1), "elapsed_ms": float64(24 * 3600 * 1000),
	} {
		if got[k] != v {
			t.Errorf("%s = %q, atteso %q", k, got[k], v)
		}
	}
	if d, _ := got["detail"].(string); len([]rune(d)) != tools.MaxReportText {
		t.Errorf("detail di %d caratteri", len([]rune(d)))
	}
}

func TestReportRejectsBadBodies(t *testing.T) {
	app, gate := newReportApp(t), reportGate(t)
	u := userRecord("u1", "watched")

	big := `{"platform":"ios","stage":"assertion","detail":"` + strings.Repeat("a", tools.MaxReportBytes) + `"}`
	if res := serve(t, app, gate, u, "POST", reportPath, big, nil); res.status != http.StatusRequestEntityTooLarge {
		t.Errorf("corpo oltre %d byte: %d %s", tools.MaxReportBytes, res.status, res.raw)
	}
	for name, body := range map[string]string{
		"non JSON":       "platform=ios",
		"JSON troncato":  `{"platform":"ios"`,
		"tipo sbagliato": `{"platform":"ios","stage":"assertion","code":"due"}`,
		"array":          `[1,2]`,
	} {
		if res := serve(t, app, gate, u, "POST", reportPath, body, nil); res.status != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, res.status, res.raw)
		}
	}
	// Un corpo di esattamente MaxReportBytes e` ancora buono.
	pad := tools.MaxReportBytes - len(`{"platform":"ios","stage":"assertion","detail":""}`)
	exact := `{"platform":"ios","stage":"assertion","detail":"` + strings.Repeat("a", pad) + `"}`
	if len(exact) != tools.MaxReportBytes {
		t.Fatalf("costruzione del test: %d byte", len(exact))
	}
	if res := serve(t, app, gate, u, "POST", reportPath, exact, nil); res.status != 200 {
		t.Errorf("corpo di %d byte: %d %s", len(exact), res.status, res.raw)
	}
	if n := len(app.reports(t)); n != 1 {
		t.Errorf("solo il corpo buono si logga: %d righe", n)
	}
}

// Oltre il tetto orario la risposta e` identica ma non si scrive piu` niente.
func TestReportRateLimit(t *testing.T) {
	app, gate := newReportApp(t), reportGate(t)
	u1, u2 := userRecord("u1", "watched"), userRecord("u2", "watched")

	first := serve(t, app, gate, u1, "POST", reportPath, validReport, nil)
	for i := 1; i < tools.ReportsPerHourPerUser; i++ {
		serve(t, app, gate, u1, "POST", reportPath, validReport, nil)
	}
	if n := len(app.reports(t)); n != tools.ReportsPerHourPerUser {
		t.Fatalf("attese %d righe, %d", tools.ReportsPerHourPerUser, n)
	}
	for range 5 {
		over := serve(t, app, gate, u1, "POST", reportPath, validReport, nil)
		if over.status != first.status || over.raw != first.raw || over.header.Get("Content-Type") != first.header.Get("Content-Type") {
			t.Fatalf("oltre il tetto la risposta cambia: %d %q contro %d %q", over.status, over.raw, first.status, first.raw)
		}
	}
	// ...anche con un corpo che altrimenti sarebbe rifiutato: oltre il tetto non si legge.
	if res := serve(t, app, gate, u1, "POST", reportPath, "non json", nil); res.status != 200 || res.raw != `{"ok":true}` {
		t.Errorf("oltre il tetto: %d %q", res.status, res.raw)
	}
	if n := len(app.reports(t)); n != tools.ReportsPerHourPerUser {
		t.Fatalf("oltre il tetto non si logga: %d righe", n)
	}
	// un altro utente ha il suo tetto
	serve(t, app, gate, u2, "POST", reportPath, validReport, nil)
	if n := len(app.reports(t)); n != tools.ReportsPerHourPerUser+1 {
		t.Fatalf("il tetto di u1 non deve toccare u2: %d righe", n)
	}
	// Reports nil = nessun tetto
	gate.Reports = nil
	for range 3 * tools.ReportsPerHourPerUser {
		serve(t, app, gate, u1, "POST", reportPath, validReport, nil)
	}
	if n := len(app.reports(t)); n != tools.ReportsPerHourPerUser+1+3*tools.ReportsPerHourPerUser {
		t.Fatalf("senza limitatore tutte le righe: %d", n)
	}
}

func TestReportWithoutGateIsNotReady(t *testing.T) {
	app := newReportApp(t)
	current.Store(nil)
	req := httptest.NewRequest("POST", reportPath, strings.NewReader(validReport))
	rec := httptest.NewRecorder()
	e := new(core.RequestEvent)
	e.App, e.Request, e.Response, e.Auth = app, req, rec, userRecord("u1", "watched")
	if err := reportHandler(e); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusServiceUnavailable || len(app.reports(t)) != 0 {
		t.Fatalf("gate assente: %d, %d righe", rec.Code, len(app.reports(t)))
	}
}

// Esente dalla prova, ma NON dal blocco, come challenge e attest.
func TestReportThroughTheMiddleware(t *testing.T) {
	gate := reportGate(t)

	t.Run("watched senza prova arriva al handler", func(t *testing.T) {
		app := newReportApp(t)
		res := serve(t, app, gate, userRecord("u1", "watched"), "POST", reportPath, validReport, nil)
		if !res.reached || res.status != 200 || res.body["ok"] != true || len(app.reports(t)) != 1 {
			t.Fatalf("reached=%v %d %v", res.reached, res.status, res.body)
		}
		// un report non e` una richiesta rifiutata: nessuna riga "richiesta rifiutata"
		for _, m := range app.lines(t) {
			if m["msg"] == "integrity: richiesta rifiutata" {
				t.Fatalf("il report e` stato rifiutato dal gate: %v", m)
			}
		}
	})
	t.Run("free", func(t *testing.T) {
		app := newReportApp(t)
		res := serve(t, app, gate, userRecord("u1", "free"), "POST", reportPath, validReport, nil)
		if res.status != 200 || len(app.reports(t)) != 1 {
			t.Fatalf("%d %v", res.status, res.body)
		}
	})
	t.Run("blocked prende 403 account_blocked e non si logga niente", func(t *testing.T) {
		app := newReportApp(t)
		res := serve(t, app, gate, userRecord("u1", "blocked"), "POST", reportPath, validReport, nil)
		if res.reached || res.status != 403 || res.body["code"] != "account_blocked" {
			t.Fatalf("reached=%v %d %v", res.reached, res.status, res.body)
		}
		if len(app.reports(t)) != 0 {
			t.Fatal("un blocked non scrive report")
		}
	})
	t.Run("varianti del path non sono esenti", func(t *testing.T) {
		for _, c := range []struct{ method, path string }{
			{"GET", reportPath},
			{"POST", "/api/integrity//report"},
			{"POST", "/api/integrity/report/../x"},
			{"POST", "/api/integrity/report/"},
		} {
			app := newReportApp(t)
			res := serve(t, app, gate, userRecord("u1", "watched"), c.method, c.path, validReport, nil)
			if res.reached || res.status != 428 || res.body["code"] != "integrity_required" {
				t.Errorf("%s %s: reached=%v %d %v", c.method, c.path, res.reached, res.status, res.body)
			}
			if len(app.reports(t)) != 0 {
				t.Errorf("%s %s: non e` il report, non si logga", c.method, c.path)
			}
		}
	})
}

// IL REPORT DESCRIVE E NON CONCEDE, dalla strada vera (middleware + handler):
// qualunque numero di report, anche "unsupported" ripetuti e anche oltre il
// tetto, non cambia la decisione per un watched. Niente X-Integrity-Session,
// niente token di login ricordato, e senza prova le route dei dati restano 428.
// Il handler gira su un'app senza DB: se provasse a scrivere (access_level
// compreso), il test fallirebbe.
func TestReportNeverChangesAuthorization(t *testing.T) {
	app, gate := newReportApp(t), reportGate(t)
	const (
		data   = "/api/collections/events/records"
		script = "Bearer tok-script"
	)
	watched := userRecord("u1", "watched")

	// un'app vera, prima: ha una sessione sua
	appToken := "Bearer tok-app"
	res := serve(t, app, gate, watched, "GET", data, "", mergeHeaders(
		proofHeaders(t, gate, "u1", "GET", data), map[string]string{"Authorization": appToken}))
	if !res.reached || res.header.Get(tools.HeaderSession) == "" {
		t.Fatalf("la prova vera deve passare e portare la sessione: %d %v", res.status, res.body)
	}
	realSession := res.header.Get(tools.HeaderSession)

	pending, err := gate.Challenges.Issue("u1")
	if err != nil {
		t.Fatal(err)
	}

	unsupported := `{"platform":"ios","stage":"generate_key","domain":"com.apple.devicecheck.error","code":1,` +
		`"disposition":"unsupported","outcome":"failed","attempt":9,"key_phase":"none"}`
	for i := range 3 * tools.ReportsPerHourPerUser {
		body := unsupported
		if i%2 == 1 {
			body = validReport
		}
		r := serve(t, app, gate, watched, "POST", reportPath, body, map[string]string{
			"Authorization":        script,
			tools.HeaderPlatform:   tools.PlatformIOS,
			tools.HeaderSession:    "sessione-inventata",
			tools.HeaderChallenge:  pending,
			tools.HeaderKeyID:      "key1",
			tools.HeaderAssertion:  "YXNzZXJ0aW9u",
			"X-Forwarded-For":      "1.2.3.4",
			"X-Integrity-Verified": "true",
		})
		if r.status != 200 || r.raw != `{"ok":true}` {
			t.Fatalf("report %d: %d %q", i, r.status, r.raw)
		}
		if h := r.header.Get(tools.HeaderSession); h != "" {
			t.Fatalf("il report %d ha fatto emettere la sessione %q", i, h)
		}
		if h := r.header.Get(tools.HeaderSessionTTL); h != "" {
			t.Fatalf("il report %d ha messo il TTL di sessione %q", i, h)
		}
	}

	// token di login e sessione inventata: mai ricordati
	if gate.Sessions.Valid("u1", tools.AuthKey(script)) || gate.Sessions.Valid("u1", "sessione-inventata") {
		t.Fatal("un report ha ricordato il token o creato la sessione")
	}
	// la challenge pendente non e` stata toccata
	if !gate.Challenges.Consume("u1", pending) {
		t.Fatal("un report ha consumato una challenge")
	}
	// le route dei dati restano chiuse a chi non ha la prova
	for _, p := range []string{data, "/api/collections/stamp/records", "/api/cs/me", "/api/batch", "/api/realtime"} {
		method := "GET"
		if p == "/api/realtime" || p == "/api/batch" {
			method = "POST"
		}
		for _, h := range []map[string]string{
			{"Authorization": script},
			{"Authorization": script, tools.HeaderPlatform: tools.PlatformIOS, tools.HeaderSession: "sessione-inventata"},
			{tools.HeaderPlatform: tools.PlatformIOS},
		} {
			r := serve(t, app, gate, watched, method, p, "", h)
			if r.reached || r.status != 428 || r.body["code"] != "integrity_required" {
				t.Fatalf("%s %s %v: reached=%v %d %v", method, p, h, r.reached, r.status, r.body)
			}
			if r.header.Get(tools.HeaderSession) != "" {
				t.Fatalf("%s %s: sessione emessa a chi non ha la prova", method, p)
			}
		}
	}
	// la sessione dell'app vera continua a valere (il report non l'ha toccata)
	r := serve(t, app, gate, watched, "GET", data, "", map[string]string{
		tools.HeaderPlatform: tools.PlatformAndroid, tools.HeaderSession: realSession,
	})
	if !r.reached {
		t.Fatalf("la sessione vera e` stata invalidata: %d %v", r.status, r.body)
	}
	if got := watched.GetString("access_level"); got != "watched" {
		t.Fatalf("access_level cambiato in %q", got)
	}
}

func mergeHeaders(a, b map[string]string) map[string]string {
	for k, v := range b {
		a[k] = v
	}
	return a
}

// Il log dei rifiuti dice anche la piattaforma dichiarata dal client.
func TestRejectLogCarriesPlatform(t *testing.T) {
	gate := reportGate(t)
	const data = "/api/collections/events/records"

	rejected := func(t *testing.T, headers map[string]string) map[string]any {
		t.Helper()
		app := newReportApp(t)
		res := serve(t, app, gate, userRecord("u1", "watched"), "GET", data, "", headers)
		if res.reached || res.status != 428 {
			t.Fatalf("atteso 428: reached=%v %d", res.reached, res.status)
		}
		var found []map[string]any
		for _, m := range app.lines(t) {
			if m["msg"] == "integrity: richiesta rifiutata" {
				found = append(found, m)
			}
		}
		if len(found) != 1 {
			t.Fatalf("attesa una riga di rifiuto: %v", found)
		}
		return found[0]
	}

	for _, platform := range []string{"ios", "android"} {
		got := rejected(t, map[string]string{tools.HeaderPlatform: platform})
		if got["platform"] != platform {
			t.Errorf("platform = %v, atteso %q", got["platform"], platform)
		}
		// i campi di prima ci sono ancora
		for k, v := range map[string]any{"user": "u1", "method": "GET", "path": data, "code": "integrity_required"} {
			if got[k] != v {
				t.Errorf("%s = %v, atteso %v", k, got[k], v)
			}
		}
		if got["reason"] == "" || got["reason"] == nil {
			t.Error("manca il motivo")
		}
	}
	// senza header il campo c'e` ed e` vuoto: si distingue da "non loggato"
	if got := rejected(t, nil); got["platform"] != "" {
		t.Errorf("senza header: platform = %#v", got["platform"])
	}
	// scritto dal client: ripulito e tagliato
	got := rejected(t, map[string]string{tools.HeaderPlatform: "ios" + strings.Repeat("x", 500)})
	if p, _ := got["platform"].(string); len(p) != 32 || !strings.HasPrefix(p, "iosxxx") {
		t.Errorf("platform lunga non tagliata: %q", p)
	}
}

// La rotta e` registrata davvero: POST /api/integrity/report dietro il router,
// con l'auth obbligatoria e il middleware del gate davanti, come in produzione.
func TestReportRoute(t *testing.T) {
	app, gate := newReportApp(t), reportGate(t)

	r := router.NewRouter(func(w http.ResponseWriter, req *http.Request) (*core.RequestEvent, router.EventCleanupFunc) {
		e := new(core.RequestEvent)
		e.Response, e.Request, e.App = w, req, app
		return e, nil
	})
	// Al posto dei loader di auth: chi manda X-Test-Level e` un utente di quel livello.
	r.Bind(&hook.Handler[*core.RequestEvent]{Id: "testAuth", Priority: -2000, Func: func(e *core.RequestEvent) error {
		if level := e.Request.Header.Get("X-Test-Level"); level != "" {
			e.Auth = userRecord("u1", level)
		}
		return e.Next()
	}})
	r.Bind(Middleware(gate))
	Load(r.Group("/api/integrity"))
	mux, err := r.BuildMux()
	if err != nil {
		t.Fatal(err)
	}

	do := func(method, path, level string) (int, string) {
		req := httptest.NewRequest(method, path, strings.NewReader(validReport))
		req.Header.Set("Content-Type", "application/json")
		if level != "" {
			req.Header.Set("X-Test-Level", level)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code, strings.TrimSpace(rec.Body.String())
	}

	if code, body := do("POST", reportPath, ""); code != http.StatusUnauthorized || len(app.reports(t)) != 0 {
		t.Errorf("senza auth: %d %s, %d righe", code, body, len(app.reports(t)))
	}
	if code, body := do("POST", reportPath, "blocked"); code != http.StatusForbidden || !strings.Contains(body, `"code":"account_blocked"`) || len(app.reports(t)) != 0 {
		t.Errorf("blocked: %d %s, %d righe", code, body, len(app.reports(t)))
	}
	for _, level := range []string{"watched", "free"} {
		before := len(app.reports(t))
		if code, body := do("POST", reportPath, level); code != 200 || body != `{"ok":true}` || len(app.reports(t)) != before+1 {
			t.Errorf("%s: %d %s, %d righe", level, code, body, len(app.reports(t))-before)
		}
	}
	// altri metodi e path sporchi non arrivano al handler (e un watched non
	// prende nessun trattamento di favore)
	before := len(app.reports(t))
	for _, c := range []struct{ method, path string }{
		{"GET", reportPath}, {"PUT", reportPath}, {"POST", "/api/integrity//report"}, {"POST", "/api/integrity/report/"},
	} {
		if code, _ := do(c.method, c.path, "watched"); code == 200 {
			t.Errorf("%s %s: 200", c.method, c.path)
		}
	}
	if len(app.reports(t)) != before {
		t.Errorf("varianti del path: %d righe di troppo", len(app.reports(t))-before)
	}
}
