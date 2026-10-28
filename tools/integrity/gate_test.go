package integrity

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/base64"
	"net/http"
	"net/url"
	"testing"
)

type decoderFunc func(token string) (*Verdict, error)

func (f decoderFunc) Decode(_ context.Context, _, token string) (*Verdict, error) { return f(token) }

type gateEnv struct {
	gate *Gate
	keys *memKeys
	dev  *ecdsa.PrivateKey // chiave del dispositivo iOS registrata per testUser come "key1"
	// counter dell'ultima asserzione iOS emessa
	counter uint32
}

// newGateEnv costruisce un Gate completo: Android con un decoder finto che
// risponde con un verdetto buono per il token "good" (hash calcolato sulla
// richiesta attesa) e App Attest con una chiave registrata.
func newGateEnv(t *testing.T) *gateEnv {
	t.Helper()
	g := &gateEnv{keys: newMemKeys()}
	g.gate = &Gate{Challenges: NewChallengeStore()}

	// Il decoder finto restituisce un verdetto il cui requestHash viene
	// fissato dal test tramite expectedHashFor.
	g.gate.Android = newVerifier(decoderFunc(func(token string) (*Verdict, error) {
		switch token {
		case "good":
			return goodVerdict(currentHash), nil
		case "bad-device":
			v := goodVerdict(currentHash)
			v.DeviceVerdicts = nil
			return v, nil
		}
		return nil, context.DeadlineExceeded
	}))

	p := newPKI(t)
	g.gate.IOS = &AppAttest{AppID: testAppID, Environment: "production", Roots: p.rootPool, Keys: g.keys}
	g.dev = registerKey(t, g.gate.IOS, testUser, "key1", 0)
	return g
}

// currentHash e` il requestHash che il decoder finto mette nel verdetto
// "good": lo imposta androidReq prima di ogni chiamata.
var currentHash string

func (g *gateEnv) challenge(t *testing.T) string {
	t.Helper()
	c, err := g.gate.Challenges.Issue(testUser)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// androidReq costruisce una richiesta con prova Android per (method, path).
func (g *gateEnv) androidReq(t *testing.T, method, path, token string) Request {
	t.Helper()
	ch := g.challenge(t)
	currentHash = RequestHash(ClientData(ch, method, path))
	h := http.Header{}
	h.Set(HeaderPlatform, PlatformAndroid)
	h.Set(HeaderChallenge, ch)
	h.Set(HeaderToken, token)
	return Request{Method: method, Path: path, Header: h}
}

// iosReq costruisce una richiesta con asserzione iOS per (method, path).
func (g *gateEnv) iosReq(t *testing.T, method, path string) Request {
	t.Helper()
	ch := g.challenge(t)
	g.counter++
	cdh := ClientDataHash(ClientData(ch, method, path))
	assertion := makeAssertion(t, g.dev, assertionAuthData(testAppID, g.counter), cdh)
	h := http.Header{}
	h.Set(HeaderPlatform, PlatformIOS)
	h.Set(HeaderChallenge, ch)
	h.Set(HeaderKeyID, "key1")
	h.Set(HeaderAssertion, base64.StdEncoding.EncodeToString(assertion))
	return Request{Method: method, Path: path, Header: h}
}

func noProof(method, path string) Request {
	return Request{Method: method, Path: path, Header: http.Header{}}
}

func expect(t *testing.T, got *Rejection, wantStatus int, wantCode string) {
	t.Helper()
	switch {
	case wantStatus == 0 && got != nil:
		t.Fatalf("atteso via libera, rifiutato: %d %s (%s)", got.Status, got.Code, got.Reason)
	case wantStatus != 0 && got == nil:
		t.Fatalf("atteso %d %s, passato", wantStatus, wantCode)
	case wantStatus != 0 && (got.Status != wantStatus || got.Code != wantCode):
		t.Fatalf("atteso %d %s, ottenuto %d %s (%s)", wantStatus, wantCode, got.Status, got.Code, got.Reason)
	}
}

// Tabella delle decisioni: livello x superficie x prova presente/assente.
func TestAuthorizeDecisionTable(t *testing.T) {
	g := newGateEnv(t)
	ctx := context.Background()
	sub := func(l Level) Subject { return Subject{UserID: testUser, Level: l} }

	surfaces := []struct {
		name, method, path string
		exempt             bool // esente dalla prova per i watched
	}{
		{"collection stamp_users", "GET", "/api/collections/stamp_users/records", false},
		{"collection stamp record", "GET", "/api/collections/stamp/records/abc", false},
		{"collection view_medals", "GET", "/api/collections/view_medals/records", false},
		{"collection qualsiasi (events)", "GET", "/api/collections/events/records", false},
		{"create", "POST", "/api/collections/stamp_users/records", false},
		{"update", "PATCH", "/api/collections/users/records/u1", false},
		{"delete", "DELETE", "/api/collections/users/records/u1", false},
		{"batch", "POST", "/api/batch", false},
		{"auth refresh", "POST", "/api/collections/users/auth-refresh", false},
		{"route custom", "GET", "/api/cs/file-link", false},
		{"route custom pagamenti", "POST", "/api/payment/donate", false},
		{"sottoscrizioni realtime", "POST", "/api/realtime", false},
		{"file token", "POST", "/api/files/token", false},
		{"challenge con metodo sbagliato", "GET", "/api/integrity/challenge", false},
		{"path non pulito non e` esente", "GET", "/api/files/../collections/stamp/records", false},

		{"challenge", "POST", "/api/integrity/challenge", true},
		{"attest", "POST", "/api/integrity/ios/attest", true},
		{"download file", "GET", "/api/files/stamp/abc/img.png", true},
		{"download file HEAD", "HEAD", "/api/files/stamp/abc/img.png", true},
		{"apertura SSE", "GET", "/api/realtime", true},
		{"health", "GET", "/api/health", true},
		{"preflight CORS", "OPTIONS", "/api/collections/stamp/records", true},

		{"fuori da /api: ical", "GET", "/ical/xyz", true},
		{"fuori da /api: links", "GET", "/links/stamp/abc", true},
		{"fuori da /api: static", "GET", "/static/a.png", true},
		{"fuori da /api: well-known", "GET", "/.well-known/assetlinks.json", true},
	}

	for _, s := range surfaces {
		t.Run("free/"+s.name, func(t *testing.T) {
			expect(t, g.gate.Authorize(ctx, sub(LevelFree), noProof(s.method, s.path)), 0, "")
		})
		t.Run("livello vuoto/"+s.name, func(t *testing.T) {
			expect(t, g.gate.Authorize(ctx, sub(""), noProof(s.method, s.path)), 0, "")
		})
		t.Run("blocked/"+s.name, func(t *testing.T) {
			// blocked: respinto su qualsiasi richiesta autenticata, esenti incluse,
			// con o senza prova, salvo l'allowlist (TestAuthorizeBlockedAllowlist).
			if s.name == "auth refresh" { // unica superficie della tabella nell'allowlist
				expect(t, g.gate.Authorize(ctx, sub(LevelBlocked), noProof(s.method, s.path)), 0, "")
				return
			}
			expect(t, g.gate.Authorize(ctx, sub(LevelBlocked), noProof(s.method, s.path)), 403, CodeAccountBlocked)
			expect(t, g.gate.Authorize(ctx, sub(LevelBlocked), g.androidReq(t, s.method, s.path, "good")), 403, CodeAccountBlocked)
		})
		t.Run("watched senza prova/"+s.name, func(t *testing.T) {
			if s.exempt {
				expect(t, g.gate.Authorize(ctx, sub(LevelWatched), noProof(s.method, s.path)), 0, "")
			} else {
				expect(t, g.gate.Authorize(ctx, sub(LevelWatched), noProof(s.method, s.path)), 428, CodeIntegrityRequire)
			}
		})
		t.Run("watched con prova android/"+s.name, func(t *testing.T) {
			expect(t, g.gate.Authorize(ctx, sub(LevelWatched), g.androidReq(t, s.method, s.path, "good")), 0, "")
		})
		t.Run("watched con prova ios/"+s.name, func(t *testing.T) {
			expect(t, g.gate.Authorize(ctx, sub(LevelWatched), g.iosReq(t, s.method, s.path)), 0, "")
		})
	}
}

// Le route delle app esterne (chiave ex_keys, nessun utente) passano senza
// prova per qualunque livello, anche con la chiave sk_ nell'header.
func TestAuthorizeExternalAppRoutes(t *testing.T) {
	g := newGateEnv(t)
	ctx := context.Background()

	routes := []struct{ method, path string }{
		{"POST", "/api/cs/exapp/valid"},
		{"POST", "/api/cs/exapp/request"},
		{"POST", "/api/cs/exapp/store-user-ticket"},
		{"POST", "/api/cs/exapp/delete-user-ticket"},
		{"POST", "/api/cs/exapp/export/request"},
		{"POST", "/api/cs/exapp/export/fetch"},
		{"GET", "/api/cs/members-hashed"},
		{"GET", "/api/cs/members-snapshots"},
		{"GET", "/api/cs/members-snapshots/2026-01-01.json.gz"},
	}
	for _, r := range routes {
		for _, level := range []Level{LevelFree, LevelWatched, LevelBlocked} {
			req := noProof(r.method, r.path)
			req.Header.Set("Authorization", "Bearer sk_0123456789abcdef")
			expect(t, g.gate.Authorize(ctx, Subject{UserID: testUser, Level: level}, req), 0, "")
			// senza utente (e.Auth nil) il middleware non chiama nemmeno il gate,
			// ma anche senza header la route resta esente: la chiave la valida l'handler.
			expect(t, g.gate.Authorize(ctx, Subject{UserID: testUser, Level: level}, noProof(r.method, r.path)), 0, "")
		}
	}

	// Un path che SOMIGLIA a quelli esenti non lo e`.
	for _, p := range []string{
		"/api/cs/exapp",
		"/api/cs/exapp/valid/extra",
		"/api/cs/members-hashed/x",
		"/api/cs/members-snapshotsX",
		"/api/cs/../collections/stamp/records",
		"/api/cs/exapp/../me",
		"/api/cs/exapp/export",
		"/api/cs/exapp/export/decide", // dell'utente, non della chiave
		"/api/cs/exapp/export/fetch/x",
	} {
		expect(t, g.gate.Authorize(ctx, Subject{UserID: testUser, Level: LevelWatched}, noProof("GET", p)), 428, CodeIntegrityRequire)
	}
	// Una route non esente con un bearer sk_ non guadagna niente.
	req := noProof("GET", "/api/collections/stamp/records")
	req.Header.Set("Authorization", "Bearer sk_0123456789abcdef")
	expect(t, g.gate.Authorize(ctx, Subject{UserID: testUser, Level: LevelWatched}, req), 428, CodeIntegrityRequire)
}

// /mcp per un watched e` rifiutato con 403 integrity_failed, anche con una
// prova perfetta (non c'e` modo di arrivarci); free passa, blocked e` bloccato.
func TestAuthorizeMCP(t *testing.T) {
	g := newGateEnv(t)
	ctx := context.Background()
	for _, p := range []string{"/mcp", "/mcp/"} {
		expect(t, g.gate.Authorize(ctx, Subject{UserID: testUser, Level: LevelWatched}, noProof("POST", p)), 403, CodeIntegrityFailed)
		expect(t, g.gate.Authorize(ctx, Subject{UserID: testUser, Level: LevelWatched}, g.androidReq(t, "POST", p, "good")), 403, CodeIntegrityFailed)
		expect(t, g.gate.Authorize(ctx, Subject{UserID: testUser, Level: LevelBlocked}, noProof("POST", p)), 403, CodeAccountBlocked)
		expect(t, g.gate.Authorize(ctx, Subject{UserID: testUser, Level: LevelFree}, noProof("POST", p)), 0, "")
	}
}

// Casi di prova Android non valida o incompleta.
func TestAuthorizeAndroidFailures(t *testing.T) {
	g := newGateEnv(t)
	ctx := context.Background()
	watched := Subject{UserID: testUser, Level: LevelWatched}
	const method, path = "GET", "/api/collections/stamp_users/records"

	t.Run("token assente", func(t *testing.T) {
		req := g.androidReq(t, method, path, "good")
		req.Header.Del(HeaderToken)
		expect(t, g.gate.Authorize(ctx, watched, req), 428, CodeIntegrityRequire)
	})
	t.Run("challenge assente", func(t *testing.T) {
		req := g.androidReq(t, method, path, "good")
		req.Header.Del(HeaderChallenge)
		expect(t, g.gate.Authorize(ctx, watched, req), 428, CodeIntegrityRequire)
	})
	t.Run("platform assente", func(t *testing.T) {
		req := g.androidReq(t, method, path, "good")
		req.Header.Del(HeaderPlatform)
		expect(t, g.gate.Authorize(ctx, watched, req), 428, CodeIntegrityRequire)
	})
	t.Run("challenge inventata", func(t *testing.T) {
		req := g.androidReq(t, method, path, "good")
		req.Header.Set(HeaderChallenge, "mai-emessa")
		expect(t, g.gate.Authorize(ctx, watched, req), 428, CodeIntegrityRequire)
	})
	t.Run("challenge di un altro utente", func(t *testing.T) {
		req := g.androidReq(t, method, path, "good")
		expect(t, g.gate.Authorize(ctx, Subject{UserID: "altro", Level: LevelWatched}, req), 428, CodeIntegrityRequire)
		// il proprietario la puo` ancora usare
		expect(t, g.gate.Authorize(ctx, watched, req), 0, "")
	})
	t.Run("challenge monouso", func(t *testing.T) {
		req := g.androidReq(t, method, path, "good")
		expect(t, g.gate.Authorize(ctx, watched, req), 0, "")
		expect(t, g.gate.Authorize(ctx, watched, req), 428, CodeIntegrityRequire)
	})
	t.Run("prova per un altro path", func(t *testing.T) {
		req := g.androidReq(t, method, "/api/collections/stamp_users/records", "good")
		req.Path = "/api/collections/stamp/records"
		expect(t, g.gate.Authorize(ctx, watched, req), 403, CodeIntegrityFailed)
	})
	t.Run("prova per un altro metodo", func(t *testing.T) {
		req := g.androidReq(t, "GET", path, "good")
		req.Method = "DELETE"
		expect(t, g.gate.Authorize(ctx, watched, req), 403, CodeIntegrityFailed)
	})
	t.Run("verdetto negativo", func(t *testing.T) {
		expect(t, g.gate.Authorize(ctx, watched, g.androidReq(t, method, path, "bad-device")), 403, CodeIntegrityFailed)
	})
	t.Run("decode fallito", func(t *testing.T) {
		expect(t, g.gate.Authorize(ctx, watched, g.androidReq(t, method, path, "boh")), 403, CodeIntegrityFailed)
	})
	t.Run("la challenge si consuma anche se la verifica fallisce", func(t *testing.T) {
		req := g.androidReq(t, method, path, "bad-device")
		expect(t, g.gate.Authorize(ctx, watched, req), 403, CodeIntegrityFailed)
		req.Header.Set(HeaderToken, "good")
		currentHash = RequestHash(ClientData(req.Header.Get(HeaderChallenge), method, path))
		expect(t, g.gate.Authorize(ctx, watched, req), 428, CodeIntegrityRequire)
	})
	t.Run("piattaforma sconosciuta", func(t *testing.T) {
		req := g.androidReq(t, method, path, "good")
		req.Header.Set(HeaderPlatform, "symbian")
		expect(t, g.gate.Authorize(ctx, watched, req), 403, CodeIntegrityFailed)
	})
	t.Run("piattaforma case-insensitive", func(t *testing.T) {
		req := g.androidReq(t, method, path, "good")
		req.Header.Set(HeaderPlatform, "Android")
		expect(t, g.gate.Authorize(ctx, watched, req), 0, "")
	})
}

// Casi di prova iOS.
func TestAuthorizeIOSFailures(t *testing.T) {
	g := newGateEnv(t)
	ctx := context.Background()
	watched := Subject{UserID: testUser, Level: LevelWatched}
	const method, path = "GET", "/api/collections/stamp_users/records"

	t.Run("key id assente -> attest_key_required", func(t *testing.T) {
		req := g.iosReq(t, method, path)
		req.Header.Del(HeaderKeyID)
		expect(t, g.gate.Authorize(ctx, watched, req), 428, CodeAttestKeyRequire)
	})
	t.Run("key id sconosciuto -> attest_key_required", func(t *testing.T) {
		req := g.iosReq(t, method, path)
		req.Header.Set(HeaderKeyID, "sconosciuta")
		expect(t, g.gate.Authorize(ctx, watched, req), 428, CodeAttestKeyRequire)
	})
	t.Run("key id di un altro utente -> attest_key_required", func(t *testing.T) {
		req := g.iosReq(t, method, path)
		expect(t, g.gate.Authorize(ctx, Subject{UserID: "altro", Level: LevelWatched}, req), 428, CodeIntegrityRequire) // challenge non sua
		registerKey(t, g.gate.IOS, "altro", "key-altro", 0)
		req = g.iosReq(t, method, path)
		req.Header.Set(HeaderKeyID, "key-altro")
		expect(t, g.gate.Authorize(ctx, watched, req), 428, CodeAttestKeyRequire)
	})
	t.Run("asserzione assente", func(t *testing.T) {
		req := g.iosReq(t, method, path)
		req.Header.Del(HeaderAssertion)
		expect(t, g.gate.Authorize(ctx, watched, req), 428, CodeIntegrityRequire)
	})
	t.Run("asserzione non base64", func(t *testing.T) {
		req := g.iosReq(t, method, path)
		req.Header.Set(HeaderAssertion, "%%%")
		expect(t, g.gate.Authorize(ctx, watched, req), 403, CodeIntegrityFailed)
	})
	t.Run("asserzione per un altro path", func(t *testing.T) {
		req := g.iosReq(t, method, path)
		req.Path = "/api/collections/stamp/records"
		expect(t, g.gate.Authorize(ctx, watched, req), 403, CodeIntegrityFailed)
	})
	t.Run("replay dell'asserzione con una nuova challenge", func(t *testing.T) {
		req := g.iosReq(t, method, path)
		expect(t, g.gate.Authorize(ctx, watched, req), 0, "")

		// stessa asserzione, nuova challenge: la firma non quadra piu'.
		replay := noProof(method, path)
		replay.Header = req.Header.Clone()
		replay.Header.Set(HeaderChallenge, g.challenge(t))
		expect(t, g.gate.Authorize(ctx, watched, replay), 403, CodeIntegrityFailed)
	})
	t.Run("asserzione con counter gia` usato", func(t *testing.T) {
		// L'ultima asserzione accettata ha usato g.counter: rigenerarne una con
		// lo stesso contatore e` un replay anche con una challenge nuova.
		g.counter--
		expect(t, g.gate.Authorize(ctx, watched, g.iosReq(t, method, path)), 403, CodeIntegrityFailed)
	})
	t.Run("asserzione con counter fuori finestra", func(t *testing.T) {
		g.counter += CounterWindowSize
		expect(t, g.gate.Authorize(ctx, watched, g.iosReq(t, method, path)), 0, "")
		g.counter -= CounterWindowSize + 1 // il prossimo e` CounterWindowSize sotto il massimo
		expect(t, g.gate.Authorize(ctx, watched, g.iosReq(t, method, path)), 403, CodeIntegrityFailed)
	})
}

// Config mancante: i watched falliscono CHIUSI, i free non se ne accorgono.
func TestAuthorizeFailClosedWithoutConfig(t *testing.T) {
	ctx := context.Background()
	const method, path = "GET", "/api/collections/stamp_users/records"
	g := newGateEnv(t)
	g.gate.Android = nil
	g.gate.IOS = nil

	watched := Subject{UserID: testUser, Level: LevelWatched}
	free := Subject{UserID: testUser, Level: LevelFree}
	blocked := Subject{UserID: testUser, Level: LevelBlocked}

	req := g.androidReq(t, method, path, "good")
	expect(t, g.gate.Authorize(ctx, watched, req), 403, CodeIntegrityFailed)

	req = g.iosReq(t, method, path)
	expect(t, g.gate.Authorize(ctx, watched, req), 403, CodeIntegrityFailed)

	// senza prova: sempre 428 (il client chiede challenge, poi prova e prende 403)
	expect(t, g.gate.Authorize(ctx, watched, noProof(method, path)), 428, CodeIntegrityRequire)

	expect(t, g.gate.Authorize(ctx, free, noProof(method, path)), 0, "")
	expect(t, g.gate.Authorize(ctx, blocked, noProof(method, path)), 403, CodeAccountBlocked)

	// Gate privo anche dello store delle challenge: nessuna prova puo` passare.
	empty := &Gate{}
	expect(t, empty.Authorize(ctx, watched, g.androidReq(t, method, path, "good")), 428, CodeIntegrityRequire)
}

func TestAndroidNotConfiguredPieces(t *testing.T) {
	ctx := context.Background()
	const method, path = "GET", "/api/cs/file-link"
	watched := Subject{UserID: testUser, Level: LevelWatched}

	g := newGateEnv(t)
	g.gate.Android.CertDigests = nil
	expect(t, g.gate.Authorize(ctx, watched, g.androidReq(t, method, path, "good")), 403, CodeIntegrityFailed)

	g = newGateEnv(t)
	g.gate.Android.Decoder = nil
	expect(t, g.gate.Authorize(ctx, watched, g.androidReq(t, method, path, "good")), 403, CodeIntegrityFailed)
}

func TestLevelOf(t *testing.T) {
	cases := map[string]Level{"": LevelFree, "free": LevelFree, "watched": LevelWatched, "blocked": LevelBlocked, "boh": LevelFree}
	for in, want := range cases {
		if got := LevelOf(stringGetter(in)); got != want {
			t.Errorf("LevelOf(%q) = %q, want %q", in, got, want)
		}
	}
	if LevelOf(nil) != LevelFree {
		t.Error("nil deve valere free")
	}
}

type stringGetter string

func (s stringGetter) GetString(string) string { return string(s) }

func TestRequiresProof(t *testing.T) {
	cases := []struct {
		method, path string
		want         bool
	}{
		{"GET", "/api/cs/me", true},
		{"POST", "/api/cs/exapp/export/decide", true},
		{"POST", "/api/cs/exapp/export/request", false},
		{"POST", "/api/cs/exapp/export/fetch", false},
		{"POST", "/api/realtime", true},
		{"GET", "/api/realtime", false},
		{"POST", "/api/integrity/challenge", false},
		{"POST", "/api/cs/exapp/valid", false},
		{"GET", "/ical/x", false},
		{"POST", "/mcp", false},
	}
	for _, c := range cases {
		if got := RequiresProof(c.method, c.path); got != c.want {
			t.Errorf("RequiresProof(%s %s) = %v", c.method, c.path, got)
		}
	}
}

func TestRejectionBody(t *testing.T) {
	b := required("x").Body()
	if b["status"] != 428 || b["code"] != CodeIntegrityRequire || b["message"] == "" {
		t.Errorf("body %v", b)
	}
	if len(b) != 3 {
		t.Errorf("il body deve avere solo status/message/code: %v", b)
	}
	_ = elliptic.P256
}

// Allowlist dei blocked: le route del flusso di conferma delle app esterne
// passano, tutto il resto e` 403 account_blocked. Per i watched le stesse
// route richiedono la prova come ogni altra.
func TestAuthorizeBlockedAllowlist(t *testing.T) {
	g := newGateEnv(t)
	ctx := context.Background()
	blocked := Subject{UserID: testUser, Level: LevelBlocked}
	watched := Subject{UserID: testUser, Level: LevelWatched}
	self := "/api/collections/users/records/" + testUser

	allowed := []struct{ method, path string }{
		{"POST", "/api/collections/users/auth-refresh"},
		{"GET", self},
		{"HEAD", self},
		{"GET", "/api/cs/me"},
		{"GET", "/api/collections/users_devices/records"},
		{"POST", "/api/collections/users_devices/records"},
		{"PATCH", "/api/collections/users_devices/records/d1"},
		{"DELETE", "/api/collections/users_devices/records/d1"},
		{"GET", "/api/collections/ex_apps/records"},
		{"GET", "/api/collections/ex_apps/records/app1"},
		{"GET", "/api/collections/ex_granted_permissions/records"},
		{"POST", "/api/collections/ex_granted_permissions/records"},
		{"GET", "/api/collections/ex_granted_permissions/records/p1"},
		{"PATCH", "/api/collections/ex_granted_permissions/records/p1"},
		{"DELETE", "/api/collections/ex_granted_permissions/records/p1"},
		{"POST", "/api/cs/exapp/export/decide"},
		{"GET", "/api/collections/user_notifications/records"},
		{"GET", "/api/collections/user_notifications/records/n1"},
		{"PATCH", "/api/collections/user_notifications/records/n1"},
		{"DELETE", "/api/collections/user_notifications/records/n1"},
	}
	for _, a := range allowed {
		req := noProof(a.method, a.path)
		expect(t, g.gate.Authorize(ctx, blocked, req), 0, "")
		// i watched sulle stesse route: prova obbligatoria
		expect(t, g.gate.Authorize(ctx, watched, noProof(a.method, a.path)), 428, CodeIntegrityRequire)
		expect(t, g.gate.Authorize(ctx, watched, g.androidReq(t, a.method, a.path, "good")), 0, "")
	}

	denied := []struct{ method, path string }{
		// metodi non in allowlist sulle stesse collection
		{"POST", "/api/collections/ex_apps/records"},
		{"PATCH", "/api/collections/ex_apps/records/app1"},
		{"DELETE", "/api/collections/ex_apps/records/app1"},
		{"GET", "/api/collections/users/auth-refresh"},
		{"PATCH", self},
		{"DELETE", self},
		{"POST", "/api/cs/me"},
		{"GET", "/api/cs/exapp/export/decide"},
		{"POST", "/api/collections/user_notifications/records"},
		// il record users di un ALTRO utente
		{"GET", "/api/collections/users/records/altro-utente"},
		{"GET", "/api/collections/users/records"},
		// collection del servizio che a un sospeso non spettano
		{"GET", "/api/collections/stamp/records"},
		{"GET", "/api/collections/stamp_users/records"},
		{"GET", "/api/collections/view_medals/records"},
		{"GET", "/api/collections/events/records"},
		{"POST", "/api/batch"},
		{"POST", "/api/realtime"},
		{"GET", "/api/cs/file-link"},
		{"POST", "/api/payment/donate"},
		{"POST", "/api/integrity/challenge"},
		{"GET", "/api/files/stamp/abc/img.png"},
		{"GET", "/ical/xyz"},
		{"POST", "/mcp"},
		// varianti che PocketBase risolverebbe lo stesso: restano fuori
		{"GET", "/api/collections/USERS_DEVICES/records"},
		{"GET", "/api/collections/_pb_users_auth_/records/" + testUser},
		{"GET", "/api/collections/users_devices/records/a/b"},
		{"GET", "/api/collections/users_devices/records/"},
		{"GET", "/api/collections/users_devices/records/../../stamp/records"},
		{"GET", "/api/collections/users/records/" + testUser + "/x"},
	}
	for _, d := range denied {
		expect(t, g.gate.Authorize(ctx, blocked, noProof(d.method, d.path)), 403, CodeAccountBlocked)
	}

	// Un expand sulle route permesse le trasformerebbe in un varco verso
	// collection che al sospeso non spettano: rifiutato.
	for _, a := range allowed {
		req := noProof(a.method, a.path)
		req.Query = url.Values{"expand": {"stamp_users_via_user.stamp"}}
		expect(t, g.gate.Authorize(ctx, blocked, req), 403, CodeAccountBlocked)
	}
	// filter, sort, page, fields... non sono un problema.
	req := noProof("GET", "/api/collections/ex_granted_permissions/records")
	req.Query = url.Values{"filter": {"user='x'"}, "perPage": {"50"}}
	expect(t, g.gate.Authorize(ctx, blocked, req), 0, "")

	// Il livello free non e` toccato dall'allowlist e i blocked senza id no.
	if BlockedAllowed("", "GET", "/api/cs/me", nil) {
		t.Error("senza userID non si autorizza niente")
	}
}
