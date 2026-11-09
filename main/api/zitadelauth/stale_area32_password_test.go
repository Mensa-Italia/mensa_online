package zitadelauth_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
	"github.com/pocketbase/pocketbase/tools/router"

	"mensadb/main/api/zitadelauth"
	_ "mensadb/migrations"
	"mensadb/tools/dbtools"
)

// Con Go 1.27 serve GOEXPERIMENT=nojsonv2 (vedi migrations).

func migratedApp(t *testing.T) core.App {
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

func createUser(t *testing.T, app core.App, id string) *core.Record {
	t.Helper()
	users, _ := app.FindCollectionByNameOrId("users")
	r := core.NewRecord(users)
	r.Id = id
	r.SetEmail(id + "@example.com")
	r.SetPassword("password-di-prova")
	r.Set("username", "user_"+id)
	if err := app.Save(r); err != nil {
		t.Fatal(err)
	}
	return r
}

// run esegue il middleware su una richiesta con e.Auth = auth e dice se ha
// lasciato passare (next chiamato) e con quale errore.
func run(t *testing.T, app core.App, auth *core.Record) (passed bool, err error) {
	t.Helper()
	e := &core.RequestEvent{App: app}
	e.Auth = auth
	var hk hook.Hook[*core.RequestEvent]
	hk.Bind(zitadelauth.RejectStaleArea32Password())
	err = hk.Trigger(e, func(*core.RequestEvent) error { passed = true; return nil })
	return passed, err
}

func TestStaleArea32PasswordGetsUnauthorized(t *testing.T) {
	app := migratedApp(t)
	user := createUser(t, app, "usera0000000001")

	if passed, err := run(t, app, user); !passed || err != nil {
		t.Fatalf("socio non segnato: passed=%v err=%v", passed, err)
	}

	dbtools.MarkArea32PasswordRejected(app, user.Email())
	marked, err := app.FindRecordById("users", user.Id)
	if err != nil {
		t.Fatal(err)
	}
	if !dbtools.Area32PasswordRejected(marked) {
		t.Fatal("il segno non e` stato salvato")
	}

	passed, err := run(t, app, marked)
	if passed {
		t.Fatal("un socio segnato non deve passare")
	}
	var apiErr *router.ApiError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusUnauthorized {
		t.Fatalf("errore = %v, want 401", err)
	}

	// Login giusto su Area32: il segno se ne va e si passa di nuovo.
	dbtools.ClearArea32PasswordRejected(marked)
	if err := app.Save(marked); err != nil {
		t.Fatal(err)
	}
	fresh, _ := app.FindRecordById("users", user.Id)
	if passed, err := run(t, app, fresh); !passed || err != nil {
		t.Fatalf("dopo la pulizia: passed=%v err=%v", passed, err)
	}
}

func TestStaleArea32PasswordIgnoresAnonymousAndUnknownEmails(t *testing.T) {
	app := migratedApp(t)

	if passed, err := run(t, app, nil); !passed || err != nil {
		t.Fatalf("richiesta anonima: passed=%v err=%v", passed, err)
	}

	// Un'email che non corrisponde a nessuno non deve fallire ne' segnare altri.
	other := createUser(t, app, "userb0000000002")
	dbtools.MarkArea32PasswordRejected(app, "nessuno@example.com")
	fresh, _ := app.FindRecordById("users", other.Id)
	if dbtools.Area32PasswordRejected(fresh) {
		t.Fatal("segnato un socio con un'altra email")
	}
}

func TestArea32PasswordRejectedFieldIsHidden(t *testing.T) {
	app := migratedApp(t)
	users, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatal(err)
	}
	f, ok := users.Fields.GetByName(dbtools.Area32PasswordRejectedField).(*core.DateField)
	if !ok {
		t.Fatal("users.area32_password_rejected_at mancante o non data")
	}
	if !f.Hidden {
		t.Error("il campo deve essere hidden: lo scrive solo il server")
	}
}
