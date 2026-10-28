package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	jwt "github.com/golang-jwt/jwt/v4"
	"github.com/pocketbase/pocketbase/core"

	_ "mensadb/migrations"
)

// Con Go 1.27 serve GOEXPERIMENT=nojsonv2 (vedi migrations): usa un DB vero.
func TestAccessGuard(t *testing.T) {
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.ResetBootstrapState() })
	if err := app.RunAllMigrations(); err != nil {
		t.Fatal(err)
	}

	users, _ := app.FindCollectionByNameOrId("users")
	mk := func(id, level string) {
		r := core.NewRecord(users)
		r.Id = id
		r.SetEmail(id + "@example.com")
		r.SetPassword("password-di-prova")
		r.Set("username", "user_"+id)
		r.Set("name", id)
		r.Set("access_level", level)
		if err := app.Save(r); err != nil {
			t.Fatal(err)
		}
	}
	mk("userfree0000001", "free")
	mk("userwatch000001", "watched")
	mk("userblock000001", "blocked")

	reached := false
	guard := newAccessGuard(app, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))

	do := func(email, sub string) (int, map[string]any) {
		reached = false
		req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		if email != "" || sub != "" {
			claims := &Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: sub}, Email: email}
			req = req.WithContext(context.WithValue(req.Context(), claimsKey, claims))
		}
		rec := httptest.NewRecorder()
		guard.ServeHTTP(rec, req)
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		return rec.Code, body
	}

	if code, _ := do("userfree0000001@example.com", "s1"); code != 200 || !reached {
		t.Errorf("free: %d reached=%v", code, reached)
	}
	if code, body := do("userwatch000001@example.com", "s2"); code != 403 || body["code"] != "integrity_failed" || reached {
		t.Errorf("watched: %d %v reached=%v", code, body, reached)
	}
	if code, body := do("userblock000001@example.com", "s3"); code != 403 || body["code"] != "account_blocked" || reached {
		t.Errorf("blocked: %d %v reached=%v", code, body, reached)
	}
	// utente non risolvibile / nessun claim: decide il resto della catena
	if code, _ := do("sconosciuto@example.com", "s4"); code != 200 || !reached {
		t.Errorf("non risolvibile: %d reached=%v", code, reached)
	}
	if code, _ := do("", ""); code != 200 || !reached {
		t.Errorf("senza claims: %d reached=%v", code, reached)
	}
}
