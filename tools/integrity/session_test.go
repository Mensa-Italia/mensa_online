package integrity

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func sessionReq(method, path, session, platform string) Request {
	h := http.Header{}
	h.Set(HeaderPlatform, platform)
	h.Set(HeaderSession, session)
	return Request{Method: method, Path: path, Header: h}
}

func TestSession(t *testing.T) {
	g := newGateEnv(t)
	g.gate.Sessions = NewSessionStore()
	ctx := context.Background()
	watched := Subject{UserID: testUser, Level: LevelWatched}
	const events = "/api/collections/events/records"

	grant, rej := g.gate.Check(ctx, watched, g.androidReq(t, "GET", events, "good"))
	expect(t, rej, 0, "")
	if grant.Session == "" || grant.TTL != SessionTTL {
		t.Fatalf("attesa una sessione dopo una prova Android valida, ottenuto %+v", grant)
	}
	s := grant.Session

	t.Run("vale al posto della prova", func(t *testing.T) {
		for range 3 {
			grant, rej := g.gate.Check(ctx, watched, sessionReq("GET", events, s, PlatformAndroid))
			expect(t, rej, 0, "")
			if grant.Session != "" {
				t.Fatal("una richiesta con sessione non deve emetterne un'altra")
			}
		}
		expect(t, g.gate.Authorize(ctx, watched, sessionReq("PATCH", "/api/collections/users/records/x", s, PlatformAndroid)), 0, "")
	})
	t.Run("vale anche su timbri, medaglie e realtime", func(t *testing.T) {
		for _, r := range [][2]string{
			{"GET", "/api/collections/stamp/records"},
			{"GET", "/api/collections/stamp_users/records/abc"},
			{"GET", "/api/collections/view_medals/records"},
			{"POST", "/api/realtime"},
		} {
			expect(t, g.gate.Authorize(ctx, watched, sessionReq(r[0], r[1], s, PlatformAndroid)), 0, "")
		}
	})
	t.Run("di un altro utente", func(t *testing.T) {
		other := Subject{UserID: "altro", Level: LevelWatched}
		expect(t, g.gate.Authorize(ctx, other, sessionReq("GET", events, s, PlatformAndroid)), 428, CodeIntegrityRequire)
	})
	t.Run("inventata", func(t *testing.T) {
		expect(t, g.gate.Authorize(ctx, watched, sessionReq("GET", events, "boh", PlatformAndroid)), 428, CodeIntegrityRequire)
		expect(t, g.gate.Authorize(ctx, watched, sessionReq("GET", events, "boh", PlatformIOS)), 428, CodeIntegrityRequire)
	})
	t.Run("scaduta", func(t *testing.T) {
		now := time.Now()
		g.gate.Sessions.now = func() time.Time { return now.Add(SessionTTL) }
		defer func() { g.gate.Sessions.now = time.Now }()
		expect(t, g.gate.Authorize(ctx, watched, sessionReq("GET", events, s, PlatformAndroid)), 428, CodeIntegrityRequire)
	})
	t.Run("anche ios riceve una sessione", func(t *testing.T) {
		grant, rej := g.gate.Check(ctx, watched, g.iosReq(t, "GET", events))
		expect(t, rej, 0, "")
		if grant.Session == "" {
			t.Fatal("attesa una sessione dopo un'asserzione App Attest valida")
		}
		expect(t, g.gate.Authorize(ctx, watched, sessionReq("GET", "/api/collections/stamp/records", grant.Session, PlatformIOS)), 0, "")
	})
	t.Run("senza store nessuna sessione", func(t *testing.T) {
		g.gate.Sessions = nil
		defer func() { g.gate.Sessions = NewSessionStore() }()
		grant, rej := g.gate.Check(ctx, watched, g.androidReq(t, "GET", events, "good"))
		expect(t, rej, 0, "")
		if grant.Session != "" {
			t.Fatal("sessione emessa senza store")
		}
		expect(t, g.gate.Authorize(ctx, watched, sessionReq("GET", events, s, PlatformAndroid)), 428, CodeIntegrityRequire)
	})
}

func TestDecodeLimiter(t *testing.T) {
	g := newGateEnv(t)
	g.gate.Decodes = NewDecodeLimiter()
	now := time.Now()
	g.gate.Decodes.now = func() time.Time { return now }
	ctx := context.Background()
	watched := Subject{UserID: testUser, Level: LevelWatched}
	const path = "/api/collections/stamp/records"

	for range DecodesPerMinutePerUser {
		expect(t, g.gate.Authorize(ctx, watched, g.androidReq(t, "GET", path, "good")), 0, "")
	}
	expect(t, g.gate.Authorize(ctx, watched, g.androidReq(t, "GET", path, "good")), 403, CodeIntegrityFailed)

	// un altro utente ha il suo tetto
	other := Subject{UserID: "altro", Level: LevelWatched}
	if !g.gate.Decodes.Allow(other.UserID) {
		t.Fatal("il tetto di un utente non deve toccare gli altri")
	}

	now = now.Add(time.Minute)
	expect(t, g.gate.Authorize(ctx, watched, g.androidReq(t, "GET", path, "good")), 0, "")
}

// Le app iOS 22.2.18 mandano la prova completa a ogni richiesta, e venti
// asserzioni generate in parallelo con la stessa chiave in parte falliscono.
// Dopo la prima buona il token di login basta, come su Android.
func TestIOSVerifiedToken(t *testing.T) {
	g := newGateEnv(t)
	g.gate.Sessions = NewSessionStore()
	ctx := context.Background()
	watched := Subject{UserID: testUser, Level: LevelWatched}
	const events = "/api/collections/events/records"

	withAuth := func(r Request, tok string) Request {
		r.Header.Set("Authorization", tok)
		return r
	}

	expect(t, g.gate.Authorize(ctx, watched, withAuth(noProof("GET", events), "tok-app")), 428, CodeIntegrityRequire)
	expect(t, g.gate.Authorize(ctx, watched, withAuth(g.iosReq(t, "GET", events), "tok-app")), 0, "")

	expect(t, g.gate.Authorize(ctx, watched, withAuth(noProof("GET", "/api/collections/stamp/records"), "tok-app")), 0, "")
	expect(t, g.gate.Authorize(ctx, watched, withAuth(noProof("GET", events), "tok-script")), 428, CodeIntegrityRequire)
}

// Il limitatore e` lo stesso per le decode (un minuto) e per i report (un'ora):
// finestra fissa, tetto per utente. Che DecodeLimiter resti un alias lo prova
// TestDecodeLimiter, che usa NewDecodeLimiter() come Gate.Decodes.
func TestUserLimiter(t *testing.T) {
	if l := NewDecodeLimiter(); l.max != DecodesPerMinutePerUser || l.per != time.Minute {
		t.Fatalf("NewDecodeLimiter: %d ogni %v", l.max, l.per)
	}

	l := NewUserLimiter(ReportsPerHourPerUser, time.Hour)
	now := time.Now()
	l.now = func() time.Time { return now }

	for i := range ReportsPerHourPerUser {
		if !l.Allow("u1") {
			t.Fatalf("la richiesta %d doveva passare", i+1)
		}
	}
	if l.Allow("u1") {
		t.Fatal("oltre il tetto orario deve dire no")
	}
	// altri utenti, altri tetti
	if !l.Allow("u2") {
		t.Fatal("il tetto di un utente non deve toccare gli altri")
	}
	// dentro l'ora la finestra non scorre
	now = now.Add(59 * time.Minute)
	if l.Allow("u1") {
		t.Fatal("a 59 minuti la finestra e` ancora la stessa")
	}
	// a un'ora si riparte
	now = now.Add(time.Minute)
	if !l.Allow("u1") {
		t.Fatal("a un'ora la finestra e` nuova")
	}
}

// Le finestre scadute degli altri utenti si buttano: la mappa non cresce per
// sempre (con finestre da un'ora conta).
func TestUserLimiterForgetsExpiredWindows(t *testing.T) {
	l := NewUserLimiter(1, time.Hour)
	now := time.Now()
	l.now = func() time.Time { return now }
	for _, u := range []string{"a", "b", "c"} {
		l.Allow(u)
	}
	if len(l.byUsr) != 3 {
		t.Fatalf("attese 3 finestre, %d", len(l.byUsr))
	}
	now = now.Add(time.Hour)
	l.Allow("d")
	if len(l.byUsr) != 1 {
		t.Fatalf("le finestre scadute andavano buttate: %d", len(l.byUsr))
	}
}
