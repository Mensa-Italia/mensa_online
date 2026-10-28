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

func TestAndroidSession(t *testing.T) {
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
	t.Run("non vale su timbri, medaglie e realtime", func(t *testing.T) {
		for _, r := range [][2]string{
			{"GET", "/api/collections/stamp/records"},
			{"GET", "/api/collections/stamp_users/records/abc"},
			{"GET", "/api/collections/view_medals/records"},
			{"POST", "/api/realtime"},
		} {
			expect(t, g.gate.Authorize(ctx, watched, sessionReq(r[0], r[1], s, PlatformAndroid)), 428, CodeIntegrityRequire)
		}
	})
	t.Run("di un altro utente", func(t *testing.T) {
		other := Subject{UserID: "altro", Level: LevelWatched}
		expect(t, g.gate.Authorize(ctx, other, sessionReq("GET", events, s, PlatformAndroid)), 428, CodeIntegrityRequire)
	})
	t.Run("inventata o su ios", func(t *testing.T) {
		expect(t, g.gate.Authorize(ctx, watched, sessionReq("GET", events, "boh", PlatformAndroid)), 428, CodeIntegrityRequire)
		expect(t, g.gate.Authorize(ctx, watched, sessionReq("GET", events, s, PlatformIOS)), 428, CodeIntegrityRequire)
	})
	t.Run("scaduta", func(t *testing.T) {
		now := time.Now()
		g.gate.Sessions.now = func() time.Time { return now.Add(SessionTTL) }
		defer func() { g.gate.Sessions.now = time.Now }()
		expect(t, g.gate.Authorize(ctx, watched, sessionReq("GET", events, s, PlatformAndroid)), 428, CodeIntegrityRequire)
	})
	t.Run("ios non riceve sessioni", func(t *testing.T) {
		grant, rej := g.gate.Check(ctx, watched, g.iosReq(t, "GET", events))
		expect(t, rej, 0, "")
		if grant.Session != "" {
			t.Fatal("sessione emessa a iOS")
		}
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

func TestSensitivePath(t *testing.T) {
	cases := map[[2]string]bool{
		{"GET", "/api/collections/stamp/records"}:                true,
		{"GET", "/api/collections/stamp"}:                        true,
		{"DELETE", "/api/collections/stamp_users/records/x"}:     true,
		{"GET", "/api/collections/view_medals/records"}:          true,
		{"POST", "/api/realtime"}:                                true,
		{"GET", "/api/collections/../collections/stamp/records"}: true,
		{"GET", "/api/collections/stampa/records"}:               false,
		{"GET", "/api/collections/events/records"}:               false,
		{"GET", "/api/realtime"}:                                 false,
		{"GET", "/api/cs/me"}:                                    false,
	}
	for in, want := range cases {
		if got := SensitivePath(in[0], in[1]); got != want {
			t.Errorf("SensitivePath(%s %s) = %v, atteso %v", in[0], in[1], got, want)
		}
	}
}

// App 22.2.18: niente sessione, prova completa a ogni richiesta. Dopo la
// prima prova valida conta il token di login: le richieste con lo stesso
// token passano senza decode, quelle con un token diverso no.
func TestVerifiedAuthToken(t *testing.T) {
	g := newGateEnv(t)
	g.gate.Sessions = NewSessionStore()
	ctx := context.Background()
	watched := Subject{UserID: testUser, Level: LevelWatched}
	const events = "/api/collections/events/records"
	withAuth := func(r Request, auth string) Request {
		r.Header.Set("Authorization", auth)
		return r
	}

	// prima del token verificato, un token Play Integrity che non si decodifica fallisce
	expect(t, g.gate.Authorize(ctx, watched, withAuth(g.androidReq(t, "GET", events, "boh"), "Bearer A")), 403, CodeIntegrityFailed)

	expect(t, g.gate.Authorize(ctx, watched, withAuth(g.androidReq(t, "GET", events, "good"), "Bearer A")), 0, "")

	t.Run("stesso token: nessuna decode", func(t *testing.T) {
		// "boh" farebbe fallire la decode: se passa, la decode non c'e` stata
		expect(t, g.gate.Authorize(ctx, watched, withAuth(g.androidReq(t, "GET", events, "boh"), "Bearer A")), 0, "")
		expect(t, g.gate.Authorize(ctx, watched, withAuth(noProof("PATCH", "/api/collections/users/records/x"), "Bearer A")), 0, "")
	})
	t.Run("token diverso", func(t *testing.T) {
		expect(t, g.gate.Authorize(ctx, watched, withAuth(noProof("GET", events), "Bearer B")), 428, CodeIntegrityRequire)
	})
	t.Run("altro utente con lo stesso token", func(t *testing.T) {
		other := Subject{UserID: "altro", Level: LevelWatched}
		expect(t, g.gate.Authorize(ctx, other, withAuth(noProof("GET", events), "Bearer A")), 428, CodeIntegrityRequire)
	})
	t.Run("timbri e medaglie vogliono sempre la prova", func(t *testing.T) {
		expect(t, g.gate.Authorize(ctx, watched, withAuth(noProof("GET", "/api/collections/stamp/records"), "Bearer A")), 428, CodeIntegrityRequire)
		expect(t, g.gate.Authorize(ctx, watched, withAuth(g.androidReq(t, "GET", "/api/collections/view_medals/records", "boh"), "Bearer A")), 403, CodeIntegrityFailed)
	})
	t.Run("ios non lo segna", func(t *testing.T) {
		expect(t, g.gate.Authorize(ctx, watched, withAuth(g.iosReq(t, "GET", events), "Bearer C")), 0, "")
		expect(t, g.gate.Authorize(ctx, watched, withAuth(noProof("GET", events), "Bearer C")), 428, CodeIntegrityRequire)
	})
	t.Run("scade con la sessione", func(t *testing.T) {
		now := time.Now()
		g.gate.Sessions.now = func() time.Time { return now.Add(SessionTTL) }
		defer func() { g.gate.Sessions.now = time.Now }()
		expect(t, g.gate.Authorize(ctx, watched, withAuth(noProof("GET", events), "Bearer A")), 428, CodeIntegrityRequire)
	})
}
