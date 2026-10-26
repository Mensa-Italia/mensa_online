package migrations

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"
)

// Su un DB che ha gia` lo schema di produzione la migrazione non deve
// toccare nessuna view: salvarle le valida eseguendone la query, e in
// produzione quella query va in timeout.
func TestProductionSchemaSkipsUnchangedViews(t *testing.T) {
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := app.ResetBootstrapState(); err != nil {
			t.Error(err)
		}
	}()

	if err := app.RunAllMigrations(); err != nil {
		t.Fatal(err)
	}

	toImport, err := collectionsToImport(app, productionSchema)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range toImport {
		if c["type"] == core.CollectionTypeView {
			t.Errorf("la view %v verrebbe reimportata", c["name"])
		}
	}
	if len(toImport) == 0 {
		t.Fatal("nessuna collection da importare: le base e auth devono restare")
	}
}

func TestSameRule(t *testing.T) {
	empty, auth := "", "@request.auth.id != ''"
	cases := []struct {
		existing *string
		imported any
		want     bool
	}{
		{nil, nil, true},
		{nil, "", false},
		{&empty, nil, false},
		{&empty, "", true},
		{&auth, "@request.auth.id != ''", true},
		{&auth, "id = @request.auth.id", false},
	}
	for i, c := range cases {
		if got := sameRule(c.existing, c.imported); got != c.want {
			t.Errorf("caso %d: got %v, want %v", i, got, c.want)
		}
	}
}
