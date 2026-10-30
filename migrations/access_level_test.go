package migrations

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"
)

func newMigratedApp(t *testing.T) core.App {
	t.Helper()
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.ResetBootstrapState(); err != nil {
			t.Error(err)
		}
	})
	if err := app.RunAllMigrations(); err != nil {
		t.Fatal(err)
	}
	return app
}

// Dopo tutte le migrazioni il campo esiste, e` nascosto ed e` un select con i
// tre livelli; la collection delle chiavi esiste ed e` solo superuser.
func TestAccessLevelAndAppAttestSchema(t *testing.T) {
	app := newMigratedApp(t)

	users, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatal(err)
	}
	f, ok := users.Fields.GetByName(accessLevelField).(*core.SelectField)
	if !ok {
		t.Fatal("users.access_level mancante o non select")
	}
	if !f.Hidden || f.MaxSelect != 1 {
		t.Errorf("access_level deve essere hidden e single: %+v", f)
	}
	if len(f.Values) != 3 {
		t.Errorf("valori inattesi: %v", f.Values)
	}

	keys, err := app.FindCollectionByNameOrId(appAttestKeysTable)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []*string{keys.ListRule, keys.ViewRule, keys.CreateRule, keys.UpdateRule, keys.DeleteRule} {
		if r != nil {
			t.Error("app_attest_keys deve essere solo superuser (regole nil)")
		}
	}
	for _, name := range []string{"user", "key_id", "public_key", "sign_counter", "environment", "created", "updated"} {
		if keys.Fields.GetByName(name) == nil {
			t.Errorf("campo %s mancante", name)
		}
	}
}

// Idempotente: rieseguire la up non fa nulla e non fallisce; la down toglie
// tutto e si puo` rieseguire a sua volta.
func TestAccessLevelAndAppAttestIdempotentAndDown(t *testing.T) {
	app := newMigratedApp(t)

	if err := upAccessLevelAndAppAttest(app); err != nil {
		t.Fatalf("seconda up: %v", err)
	}
	if err := downAccessLevelAndAppAttest(app); err != nil {
		t.Fatalf("down: %v", err)
	}
	if err := downAccessLevelAndAppAttest(app); err != nil {
		t.Fatalf("seconda down: %v", err)
	}

	// Nessuna view legge piu' access_level (view_leaderboard e' una tabella
	// dalla migrazione 1793600000), quindi la down toglie la colonna. Se una
	// view la leggesse, la lascerebbe: SQLite non toglie una colonna usata.
	users, _ := app.FindCollectionByNameOrId("users")
	if users.Fields.GetByName(accessLevelField) != nil {
		t.Error("access_level ancora presente dopo la down")
	}
	if c, _ := app.FindCollectionByNameOrId(appAttestKeysTable); c != nil {
		t.Error("app_attest_keys ancora presente dopo la down")
	}

	if err := upAccessLevelAndAppAttest(app); err != nil {
		t.Fatalf("up dopo down: %v", err)
	}
}

// ex_export_requests: collection solo superuser, permessi export sulla
// chiave, up idempotente e down funzionante.
func TestExExportRequestsMigration(t *testing.T) {
	app := newMigratedApp(t)

	c, err := app.FindCollectionByNameOrId(exportRequestsTable)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []*string{c.ListRule, c.ViewRule, c.CreateRule, c.UpdateRule, c.DeleteRule} {
		if r != nil {
			t.Error("ex_export_requests deve essere solo superuser")
		}
	}
	for _, name := range []string{"ex_app", "user", "scope", "status", "callme_url", "expires_at", "decided_at", "consumed_at", "created", "updated"} {
		if c.Fields.GetByName(name) == nil {
			t.Errorf("campo %s mancante", name)
		}
	}

	hasPerms := func() bool {
		keys, _ := app.FindCollectionByNameOrId("ex_keys")
		f := keys.Fields.GetByName("permissions").(*core.SelectField)
		n := 0
		for _, v := range f.Values {
			if v == "EXPORT_STAMPS" || v == "EXPORT_MEDALS" {
				n++
			}
		}
		return n == 2
	}
	if !hasPerms() {
		t.Fatal("EXPORT_STAMPS / EXPORT_MEDALS mancanti da ex_keys.permissions")
	}

	if err := upExExportRequests(app); err != nil {
		t.Fatalf("seconda up: %v", err)
	}
	if err := downExExportRequests(app); err != nil {
		t.Fatalf("down: %v", err)
	}
	if x, _ := app.FindCollectionByNameOrId(exportRequestsTable); x != nil || hasPerms() {
		t.Error("down incompleta")
	}
	if err := upExExportRequests(app); err != nil {
		t.Fatalf("up dopo down: %v", err)
	}
}
