package integrity

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
	"github.com/pocketbase/pocketbase/tools/subscriptions"

	tools "mensadb/tools/integrity"
)

const mwPkg = "it.mensa.app"

// tokenIsHash: il decoder finto usa il token come requestHash del verdetto,
// cosi' un test costruisce una prova valida per (challenge, metodo, path)
// semplicemente mettendo RequestHash(ClientData(...)) come token.
type tokenIsHash struct{}

func (tokenIsHash) Decode(_ context.Context, _, token string) (*tools.Verdict, error) {
	return &tools.Verdict{
		RequestPackageName:    mwPkg,
		RequestHash:           token,
		TimestampMillis:       time.Now().UnixMilli(),
		AppRecognitionVerdict: "PLAY_RECOGNIZED",
		AppPackageName:        mwPkg,
		CertSHA256Digests:     []string{"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
		DeviceVerdicts:        []string{"MEETS_DEVICE_INTEGRITY"},
	}, nil
}

func newTestGate() *tools.Gate {
	return &tools.Gate{
		Challenges: tools.NewChallengeStore(),
		Android: &tools.AndroidVerifier{
			Decoder:     tokenIsHash{},
			PackageName: mwPkg,
			CertDigests: []string{"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
		},
	}
}

// userRecord crea un record utente in memoria (non serve il DB: il middleware
// legge solo id e access_level).
func userRecord(id, level string) *core.Record {
	users := core.NewAuthCollection("users")
	users.Fields.Add(&core.SelectField{
		Name: "access_level", Values: []string{"free", "watched", "blocked"}, MaxSelect: 1, Hidden: true,
	})
	r := core.NewRecord(users)
	r.Id = id
	r.Set("access_level", level)
	return r
}

type mwResult struct {
	reached bool
	status  int
	body    map[string]any
	header  http.Header
	event   *core.RequestEvent
}

func runMiddleware(t *testing.T, gate *tools.Gate, auth *core.Record, method, path string, headers map[string]string) mwResult {
	t.Helper()
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})

	req := httptest.NewRequest(method, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e := new(core.RequestEvent)
	e.App = app
	e.Request = req
	e.Response = rec
	e.Auth = auth

	h := &hook.Hook[*core.RequestEvent]{}
	h.Bind(Middleware(gate))
	res := mwResult{event: e}
	if err := h.Trigger(e, func(*core.RequestEvent) error { res.reached = true; return nil }); err != nil {
		t.Fatal(err)
	}
	res.status = rec.Code
	res.header = rec.Header()
	_ = json.Unmarshal(rec.Body.Bytes(), &res.body)
	return res
}

// proofHeaders costruisce gli header di una prova Android valida.
func proofHeaders(t *testing.T, gate *tools.Gate, userID, method, path string) map[string]string {
	t.Helper()
	ch, err := gate.Challenges.Issue(userID)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]string{
		tools.HeaderPlatform:  tools.PlatformAndroid,
		tools.HeaderChallenge: ch,
		tools.HeaderToken:     tools.RequestHash(tools.ClientData(ch, method, path)),
	}
}

// Tabella delle decisioni del middleware: livello x superficie x prova.
func TestMiddlewareDecisionTable(t *testing.T) {
	gate := newTestGate()

	surfaces := []struct {
		name, method, path string
		exempt             bool // esente dalla prova per i watched
	}{
		{"timbri", "GET", "/api/collections/stamp_users/records", false},
		{"medaglie", "GET", "/api/collections/view_medals/records", false},
		{"collection non protetta", "GET", "/api/collections/events/records", false},
		{"batch", "POST", "/api/batch", false},
		{"route custom", "GET", "/api/cs/file-link", false},
		{"sottoscrizioni realtime", "POST", "/api/realtime", false},
		{"challenge", "POST", "/api/integrity/challenge", true},
		{"download file", "GET", "/api/files/stamp/a/b.png", true},
		{"apertura SSE", "GET", "/api/realtime", true},
		{"health", "GET", "/api/health", true},
		{"fuori da /api", "GET", "/ical/abc", true},
		{"app esterna sk_ (valid)", "POST", "/api/cs/exapp/valid", true},
		{"app esterna sk_ (members_hashed)", "GET", "/api/cs/members-hashed", true},
	}

	for _, s := range surfaces {
		t.Run("free/"+s.name, func(t *testing.T) {
			r := runMiddleware(t, gate, userRecord("u1", "free"), s.method, s.path, nil)
			if !r.reached || r.status != http.StatusOK {
				t.Fatalf("free deve passare: reached=%v status=%d", r.reached, r.status)
			}
		})
		t.Run("livello non impostato/"+s.name, func(t *testing.T) {
			r := runMiddleware(t, gate, userRecord("u1", ""), s.method, s.path, nil)
			if !r.reached {
				t.Fatal("un utente senza access_level e` free")
			}
		})
		t.Run("blocked/"+s.name, func(t *testing.T) {
			r := runMiddleware(t, gate, userRecord("u1", "blocked"), s.method, s.path, nil)
			if s.name == "app esterna sk_ (valid)" || s.name == "app esterna sk_ (members_hashed)" {
				// e.Auth sarebbe nil, ma anche se non lo fosse non si applica il blocco
				if !r.reached {
					t.Fatal("le route delle app esterne non sono soggette al blocco")
				}
				return
			}
			if r.reached || r.status != 403 || r.body["code"] != "account_blocked" {
				t.Fatalf("blocked: reached=%v status=%d body=%v", r.reached, r.status, r.body)
			}
		})
		t.Run("watched senza prova/"+s.name, func(t *testing.T) {
			r := runMiddleware(t, gate, userRecord("u1", "watched"), s.method, s.path, nil)
			if s.exempt {
				if !r.reached {
					t.Fatalf("esente: reached=%v status=%d", r.reached, r.status)
				}
				return
			}
			if r.reached || r.status != 428 || r.body["code"] != "integrity_required" {
				t.Fatalf("reached=%v status=%d body=%v", r.reached, r.status, r.body)
			}
		})
		t.Run("watched con prova/"+s.name, func(t *testing.T) {
			h := proofHeaders(t, gate, "u1", s.method, s.path)
			r := runMiddleware(t, gate, userRecord("u1", "watched"), s.method, s.path, h)
			if !r.reached {
				t.Fatalf("con prova valida deve passare: status=%d body=%v", r.status, r.body)
			}
		})
	}
}

// Il body d'errore e` esattamente {"status","message","code"}.
func TestMiddlewareErrorBody(t *testing.T) {
	r := runMiddleware(t, newTestGate(), userRecord("u1", "watched"), "GET", "/api/cs/file-link", nil)
	if len(r.body) != 3 || r.body["status"] != float64(428) || r.body["code"] != "integrity_required" || r.body["message"] == "" {
		t.Fatalf("body: %v", r.body)
	}
}

// Prova invalida: 403 integrity_failed; challenge riusata: 428.
func TestMiddlewareInvalidAndReplayedProof(t *testing.T) {
	gate := newTestGate()
	const m, p = "GET", "/api/collections/stamp_users/records"
	watched := func() *core.Record { return userRecord("u1", "watched") }

	// token calcolato per un altro path
	h := proofHeaders(t, gate, "u1", m, "/api/collections/stamp/records")
	r := runMiddleware(t, gate, watched(), m, p, h)
	if r.reached || r.status != 403 || r.body["code"] != "integrity_failed" {
		t.Fatalf("prova per un altro path: %d %v", r.status, r.body)
	}

	// replay della stessa prova valida
	h = proofHeaders(t, gate, "u1", m, p)
	if r := runMiddleware(t, gate, watched(), m, p, h); !r.reached {
		t.Fatalf("prima volta: %d %v", r.status, r.body)
	}
	if r := runMiddleware(t, gate, watched(), m, p, h); r.reached || r.status != 428 {
		t.Fatalf("replay: reached=%v status=%d", r.reached, r.status)
	}
}

// Config assente: i watched falliscono chiusi, free e blocked come sempre.
func TestMiddlewareFailClosedWithoutConfig(t *testing.T) {
	gate := &tools.Gate{Challenges: tools.NewChallengeStore()} // niente Android, niente iOS
	const m, p = "GET", "/api/collections/stamp_users/records"

	h := proofHeaders(t, gate, "u1", m, p)
	r := runMiddleware(t, gate, userRecord("u1", "watched"), m, p, h)
	if r.reached || r.status != 403 || r.body["code"] != "integrity_failed" {
		t.Fatalf("watched senza config: reached=%v %d %v", r.reached, r.status, r.body)
	}
	if r := runMiddleware(t, gate, userRecord("u1", "free"), m, p, nil); !r.reached {
		t.Fatal("free non deve accorgersi della config mancante")
	}
	if r := runMiddleware(t, gate, userRecord("u1", "blocked"), m, p, nil); r.reached || r.status != 403 {
		t.Fatal("blocked funziona anche senza config")
	}
}

func TestMiddlewareBypasses(t *testing.T) {
	gate := newTestGate()
	const m, p = "GET", "/api/collections/stamp_users/records"

	// senza auth il livello non e` noto: si comporta come oggi
	if r := runMiddleware(t, gate, nil, m, p, nil); !r.reached {
		t.Fatal("richiesta senza auth deve passare")
	}
	// i superuser passano sempre
	su := core.NewRecord(core.NewAuthCollection(core.CollectionNameSuperusers))
	su.Id = "su1"
	su.Set("access_level", "blocked") // anche se il campo ci fosse
	if r := runMiddleware(t, gate, su, m, p, nil); !r.reached {
		t.Fatal("un superuser non e` soggetto al gate")
	}
}

// /mcp per un watched: 403 integrity_failed anche con una prova perfetta.
func TestMiddlewareMCP(t *testing.T) {
	gate := newTestGate()
	h := proofHeaders(t, gate, "u1", "POST", "/mcp")
	r := runMiddleware(t, gate, userRecord("u1", "watched"), "POST", "/mcp", h)
	if r.reached || r.status != 403 || r.body["code"] != "integrity_failed" {
		t.Fatalf("mcp watched: %d %v", r.status, r.body)
	}
	if r := runMiddleware(t, gate, userRecord("u1", "free"), "POST", "/mcp", nil); !r.reached {
		t.Fatal("free deve poter usare /mcp")
	}
	if r := runMiddleware(t, gate, userRecord("u1", "blocked"), "POST", "/mcp", nil); r.reached || r.body["code"] != "account_blocked" {
		t.Fatalf("mcp blocked: %v", r.body)
	}
}

// Il middleware deve girare dopo i loader di auth e dopo il rate limit.
func TestMiddlewarePriority(t *testing.T) {
	h := Middleware(newTestGate())
	if h.Priority <= apis.DefaultLoadAuthTokenMiddlewarePriority {
		t.Errorf("priority %d non e` dopo il loader di auth (%d)", h.Priority, apis.DefaultLoadAuthTokenMiddlewarePriority)
	}
	if h.Priority <= apis.DefaultRateLimitMiddlewarePriority {
		t.Errorf("priority %d non e` dopo il rate limit (%d)", h.Priority, apis.DefaultRateLimitMiddlewarePriority)
	}
	if h.Id == "" {
		t.Error("serve un id per poterlo sganciare/verificare")
	}
}

// Flag di richiesta verificata: solo se la prova c'e` davvero stata.
func TestMiddlewareVerifiedFlag(t *testing.T) {
	gate := newTestGate()
	const m, p = "POST", "/api/realtime"

	h := proofHeaders(t, gate, "u1", m, p)
	r := runMiddleware(t, gate, userRecord("u1", "watched"), m, p, h)
	if v, _ := r.event.Get(requestVerifiedKey).(bool); !v {
		t.Error("prova valida: flag atteso")
	}
	r = runMiddleware(t, gate, userRecord("u1", "free"), m, p, nil)
	if v, _ := r.event.Get(requestVerifiedKey).(bool); v {
		t.Error("free non porta prova: nessun flag")
	}
}

// Hook realtime: un client il cui utente e` blocked, o watched senza
// sottoscrizione verificata, non riceve piu' eventi.
func TestRealtimeMessageGate(t *testing.T) {
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
	BindRealtime(app)

	deliver := func(auth *core.Record, verified bool) bool {
		client := subscriptions.NewDefaultClient()
		if auth != nil {
			client.Set(apis.RealtimeClientAuthKey, auth)
		}
		client.Set(clientVerifiedKey, verified)
		e := &core.RealtimeMessageEvent{Client: client, Message: &subscriptions.Message{Name: "stamp_users/*"}}
		e.RequestEvent = new(core.RequestEvent)
		e.App = app
		sent := false
		if err := app.OnRealtimeMessageSend().Trigger(e, func(*core.RealtimeMessageEvent) error { sent = true; return nil }); err != nil {
			t.Fatal(err)
		}
		return sent
	}

	cases := []struct {
		name     string
		auth     *core.Record
		verified bool
		want     bool
	}{
		{"guest", nil, false, true},
		{"free", userRecord("u1", "free"), false, true},
		{"blocked", userRecord("u1", "blocked"), true, false},
		{"watched non verificato", userRecord("u1", "watched"), false, false},
		{"watched verificato", userRecord("u1", "watched"), true, true},
	}
	for _, c := range cases {
		if got := deliver(c.auth, c.verified); got != c.want {
			t.Errorf("%s: consegnato=%v, atteso %v", c.name, got, c.want)
		}
	}
}

// La sottoscrizione segna il client come verificato solo se la richiesta lo era.
func TestRealtimeSubscribeMarksClient(t *testing.T) {
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
	BindRealtime(app)

	for _, verified := range []bool{true, false} {
		re := new(core.RequestEvent)
		re.App = app
		if verified {
			re.Set(requestVerifiedKey, true)
		}
		client := subscriptions.NewDefaultClient()
		e := &core.RealtimeSubscribeRequestEvent{RequestEvent: re, Client: client}
		if err := app.OnRealtimeSubscribeRequest().Trigger(e, func(*core.RealtimeSubscribeRequestEvent) error { return nil }); err != nil {
			t.Fatal(err)
		}
		got, _ := client.Get(clientVerifiedKey).(bool)
		if got != verified {
			t.Errorf("verified=%v: client marcato %v", verified, got)
		}
	}
}

// Una prova Android valida torna con una sessione, e la sessione basta per le
// richieste successive, timbri e medaglie compresi.
func TestMiddlewareAndroidSession(t *testing.T) {
	gate := newTestGate()
	gate.Sessions = tools.NewSessionStore()
	u := userRecord("u1", "watched")
	const path = "/api/collections/events/records"

	res := runMiddleware(t, gate, u, "GET", path, proofHeaders(t, gate, "u1", "GET", path))
	if !res.reached {
		t.Fatalf("prova valida rifiutata: %d %v", res.status, res.body)
	}
	s := res.header.Get(tools.HeaderSession)
	if s == "" || res.header.Get(tools.HeaderSessionTTL) != "600" {
		t.Fatalf("sessione assente o TTL sbagliato: %v", res.header)
	}

	withSession := map[string]string{tools.HeaderPlatform: tools.PlatformAndroid, tools.HeaderSession: s}
	if res := runMiddleware(t, gate, u, "GET", path, withSession); !res.reached {
		t.Fatalf("sessione rifiutata: %d %v", res.status, res.body)
	}
	if res := runMiddleware(t, gate, u, "GET", "/api/collections/stamp/records", withSession); !res.reached {
		t.Fatalf("la sessione deve valere anche sui timbri: %d %v", res.status, res.body)
	}
}
