package migrations

import (
	"testing"

	"mensadb/tools/leaderboard"

	"github.com/pocketbase/pocketbase/core"
)

// Su un DB nuovo le tre classifiche devono essere tabelle (non piu' viste),
// con le regole di sola lettura per soci autenticati, e lo storico deve
// esistere. Ricalcolo e snapshot girano anche senza dati.
func TestLeaderboardTablesReplaceViews(t *testing.T) {
	app := newMigratedApp(t)

	for _, name := range []string{leaderboard.MembersTable, leaderboard.RegionsTable, leaderboard.SecretariesTable} {
		c, err := app.FindCollectionByNameOrId(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if c.IsView() {
			t.Errorf("%s e' ancora una vista", name)
		}
		if c.ListRule == nil || c.ViewRule == nil {
			t.Errorf("%s: regole di lettura mancanti", name)
		} else if *c.ListRule != *c.ViewRule {
			t.Errorf("%s: list e view rule diverse", name)
		}
		for _, field := range []string{"name", "position", "score", "previous_position", "previous_score"} {
			if c.Fields.GetByName(field) == nil {
				t.Errorf("%s: campo %s mancante", name, field)
			}
		}
	}

	members, _ := app.FindCollectionByNameOrId(leaderboard.MembersTable)
	if members.ListRule == nil || *members.ListRule != `@request.auth.id != "" && (position <= 10 || id = @request.auth.id)` {
		t.Errorf("regola soci inattesa: %v", members.ListRule)
	}

	history, err := app.FindCollectionByNameOrId(leaderboard.HistoryTable)
	if err != nil {
		t.Fatal(err)
	}
	if history.ListRule != nil {
		t.Error("lo storico deve restare solo superuser")
	}

	if err := leaderboard.Refresh(app); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if err := leaderboard.Snapshot(app); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	// Idempotente: ripetere non fallisce e non duplica lo storico.
	if err := leaderboard.Snapshot(app); err != nil {
		t.Fatalf("secondo snapshot: %v", err)
	}
	if err := leaderboard.Refresh(app); err != nil {
		t.Fatalf("secondo refresh: %v", err)
	}
	if err := upLeaderboardTables(app); err != nil {
		t.Fatalf("seconda up: %v", err)
	}
	// L'opt-out immediato non deve fallire su un socio senza riga.
	if err := leaderboard.Hide(app, "0000"); err != nil {
		t.Fatalf("hide senza riga: %v", err)
	}
}

// Lo schema di produzione contiene ancora le tre viste: su un DB migrato
// non devono essere reimportate al posto delle tabelle.
func TestProductionSchemaSkipsViewsTurnedTables(t *testing.T) {
	app := newMigratedApp(t)
	toImport, err := collectionsToImport(app, productionSchema)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range toImport {
		switch c["name"] {
		case leaderboard.MembersTable, leaderboard.RegionsTable, leaderboard.SecretariesTable:
			t.Errorf("%v verrebbe reimportata come vista", c["name"])
		}
	}
	for _, name := range []string{leaderboard.MembersTable, leaderboard.RegionsTable, leaderboard.SecretariesTable} {
		c, _ := app.FindCollectionByNameOrId(name)
		if c == nil || c.Type != core.CollectionTypeBase {
			t.Errorf("%s non e' una collection base", name)
		}
	}
}
