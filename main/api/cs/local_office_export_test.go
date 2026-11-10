package cs

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	_ "mensadb/migrations"
	"mensadb/tools/memberchanges"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/xuri/excelize/v2"
)

// Con Go 1.27 serve GOEXPERIMENT=nojsonv2 (vedi migrations).

const exportOffice = "lopuglia0000001"

// newExportApp prepara un'app con tutte le migrazioni, una sede (Puglia),
// il suo segretario (4001), un socio qualunque (4002), un super (4003) e un
// socio entrato oggi (4005). Ritorna l'app e i token dei tre utenti.
//
// Ogni scenario vuole un'app sua (il router si ricostruisce a ogni
// scenario), ma i token degli header si scrivono prima: segreto dei token e
// tokenKey fissi, cosi' un token vale per tutte le app.
func newExportApp(t testing.TB) (*tests.TestApp, map[string]string) {
	t.Helper()
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}

	save := func(collection, id string, values map[string]any) {
		t.Helper()
		c, err := app.FindCollectionByNameOrId(collection)
		if err != nil {
			t.Fatal(err)
		}
		r := core.NewRecord(c)
		if id != "" {
			r.Id = id
		}
		for k, v := range values {
			r.Set(k, v)
		}
		if err := app.SaveNoValidate(r); err != nil {
			t.Fatalf("%s/%s: %v", collection, id, err)
		}
	}
	save("local_offices", exportOffice, map[string]any{"name": "Puglia", "region": "Basilicata e Puglia", "region_key": "Puglia", "slug": "puglia"})
	save("local_offices", "lolazio00000001", map[string]any{"name": "Lazio", "region": "Lazio", "region_key": "Lazio", "slug": "lazio"})
	save("members_registry", "4001", map[string]any{"name": "Segretario", "area": "Puglia", "is_active": true})
	save("members_registry", "4002", map[string]any{"name": "Socio", "area": "Puglia", "is_active": true})
	save("members_registry", "4003", map[string]any{"name": "Super", "area": "Lazio", "is_active": true})
	save("members_registry", "4005", map[string]any{"name": "Nuovo Socio", "area": "Basilicata", "is_active": true,
		"city": "Matera", "state": "MT", "alias_mail": "nuovo.socio@mensa.it", "original_mail": "privata@example.com"})
	save("local_offices_admins", "", map[string]any{"local_office": exportOffice, "user": "4001", "is_the_officer": true})
	if err := memberchanges.Record(app, []memberchanges.Change{{Member: "4005", Name: "Nuovo Socio", Kind: memberchanges.Joined, RegionKey: "Puglia"}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	// Due usciti della sede: uno di 10 giorni fa, fuori dai 7 che vedono
	// gli admin della sede, e uno di 2 giorni fa.
	save("members_registry", "4006", map[string]any{"name": "Uscito Vecchio", "area": "Puglia", "is_active": false})
	save("members_registry", "4007", map[string]any{"name": "Uscito Recente", "area": "Puglia", "is_active": false})
	for member, ago := range map[string]int{"4006": 10, "4007": 2} {
		name := map[string]string{"4006": "Uscito Vecchio", "4007": "Uscito Recente"}[member]
		if err := memberchanges.Record(app, []memberchanges.Change{{Member: member, Name: name, Kind: memberchanges.Left, RegionKey: "Puglia"}}, time.Now().AddDate(0, 0, -ago)); err != nil {
			t.Fatal(err)
		}
	}

	users, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatal(err)
	}
	users.AuthToken.Secret = "segreto-dei-token-di-prova-0123456789"
	if err := app.Save(users); err != nil {
		t.Fatal(err)
	}
	tokens := map[string]string{}
	for id, powers := range map[string][]string{"4001": nil, "4002": nil, "4003": {"super"}} {
		r := core.NewRecord(users)
		r.Id = id
		r.SetEmail("u" + id + "@example.com")
		r.SetPassword("password-di-prova")
		r.Set("username", "user_"+id)
		r.Set("name", "Utente "+id)
		r.Set("powers", powers)
		r.SetTokenKey("tokenkey-di-prova-0123456789-" + id)
		if err := app.Save(r); err != nil {
			t.Fatal(err)
		}
		token, err := r.NewAuthToken()
		if err != nil {
			t.Fatal(err)
		}
		tokens[id] = token
	}
	return app, tokens
}

// readSheet ritorna le righe del foglio Soci.
func readSheet(t testing.TB, res *http.Response) [][]string {
	t.Helper()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(body, []byte("PK")) {
		t.Fatalf("non e' uno zip: %q", body[:min(len(body), 16)])
	}
	f, err := excelize.OpenReader(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	rows, err := f.GetRows("Soci")
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// sheetMembers ritorna le tessere del foglio Soci, senza intestazione,
// separate da virgola.
func sheetMembers(t testing.TB, res *http.Response) string {
	t.Helper()
	var got []string
	for _, r := range readSheet(t, res)[1:] {
		got = append(got, r[0])
	}
	return strings.Join(got, ",")
}

// La collection members_registry_changes: gli arrivi li vede ogni socio, le
// uscite il segretario della sede solo degli ultimi 7 giorni, il super
// tutte.
func TestMembersRegistryChangesExitsWindow(t *testing.T) {
	first, tokens := newExportApp(t)
	first.Cleanup()
	factory := func(t testing.TB) *tests.TestApp {
		app, _ := newExportApp(t)
		return app
	}
	url := "/api/collections/" + memberchanges.Table + "/records?sort=member&fields=member&perPage=50"
	scenarios := []tests.ApiScenario{
		{
			Name:            "socio qualunque: solo gli arrivi",
			Method:          http.MethodGet,
			URL:             url,
			Headers:         map[string]string{"Authorization": tokens["4002"]},
			ExpectedStatus:  http.StatusOK,
			ExpectedContent: []string{`"totalItems":1`, `"member":"4005"`},
		},
		{
			Name:               "segretario: arrivi e uscite degli ultimi 7 giorni",
			Method:             http.MethodGet,
			URL:                url,
			Headers:            map[string]string{"Authorization": tokens["4001"]},
			ExpectedStatus:     http.StatusOK,
			ExpectedContent:    []string{`"totalItems":2`, `"member":"4005"`, `"member":"4007"`},
			NotExpectedContent: []string{`"member":"4006"`},
		},
		{
			Name:            "super: tutto",
			Method:          http.MethodGet,
			URL:             url,
			Headers:         map[string]string{"Authorization": tokens["4003"]},
			ExpectedStatus:  http.StatusOK,
			ExpectedContent: []string{`"totalItems":3`, `"member":"4005"`, `"member":"4006"`, `"member":"4007"`},
		},
	}
	for _, s := range scenarios {
		s.TestAppFactory = factory
		s.Test(t)
	}
}

func TestLocalOfficeMembersExport(t *testing.T) {
	first, tokens := newExportApp(t)
	first.Cleanup()
	factory := func(t testing.TB) *tests.TestApp {
		app, _ := newExportApp(t)
		return app
	}
	register := func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
		e.Router.GET("/api/cs/local-office/{id}/members-export", LocalOfficeMembersExportHandler).Bind(apis.RequireAuth())
	}
	url := "/api/cs/local-office/" + exportOffice + "/members-export"

	scenarios := []tests.ApiScenario{
		{
			Name:            "senza login",
			Method:          http.MethodGet,
			URL:             url,
			ExpectedStatus:  http.StatusUnauthorized,
			ExpectedContent: []string{`"status":401`},
		},
		{
			Name:            "socio qualunque",
			Method:          http.MethodGet,
			URL:             url,
			Headers:         map[string]string{"Authorization": tokens["4002"]},
			ExpectedStatus:  http.StatusForbidden,
			ExpectedContent: []string{`"status":403`},
		},
		{
			Name:            "segretario di un'altra sede",
			Method:          http.MethodGet,
			URL:             "/api/cs/local-office/lolazio00000001/members-export",
			Headers:         map[string]string{"Authorization": tokens["4001"]},
			ExpectedStatus:  http.StatusForbidden,
			ExpectedContent: []string{`"status":403`},
		},
		{
			Name:            "segretario: nuovi soci",
			Method:          http.MethodGet,
			URL:             url,
			Headers:         map[string]string{"Authorization": tokens["4001"]},
			ExpectedStatus:  http.StatusOK,
			ExpectedContent: []string{"PK"},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				if ct := res.Header.Get("Content-Type"); ct != xlsxContentType {
					t.Errorf("Content-Type: %q", ct)
				}
				cd := res.Header.Get("Content-Disposition")
				if !strings.HasPrefix(cd, `attachment; filename="soci-puglia-new-`) || !strings.HasSuffix(cd, `.xlsx"`) {
					t.Errorf("Content-Disposition: %q", cd)
				}
				rows := readSheet(t, res)
				if len(rows) != 2 {
					t.Fatalf("righe: %v", rows)
				}
				if strings.Join(rows[0], "|") != "Tessera|Nome|Email|Citta'|Provincia|Movimento|Data" {
					t.Errorf("intestazione: %v", rows[0])
				}
				today := memberchanges.Day(time.Now()).Format("02/01/2006")
				want := "4005|Nuovo Socio|nuovo.socio@mensa.it|Matera|MT|Nuovo socio|" + today
				if strings.Join(rows[1], "|") != want {
					t.Errorf("riga: %v, attesa %s", rows[1], want)
				}
			},
		},
		{
			Name:            "segretario: usciti, solo gli ultimi 7 giorni",
			Method:          http.MethodGet,
			URL:             url + "?kind=left",
			Headers:         map[string]string{"Authorization": tokens["4001"]},
			ExpectedStatus:  http.StatusOK,
			ExpectedContent: []string{"PK"},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				if got := sheetMembers(t, res); got != "4007" {
					t.Errorf("usciti: %q, atteso 4007", got)
				}
			},
		},
		{
			Name:            "segretario: usciti, from vecchio non allarga",
			Method:          http.MethodGet,
			URL:             url + "?kind=left&from=" + time.Now().AddDate(0, 0, -60).Format("2006-01-02"),
			Headers:         map[string]string{"Authorization": tokens["4001"]},
			ExpectedStatus:  http.StatusOK,
			ExpectedContent: []string{"PK"},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				if got := sheetMembers(t, res); got != "4007" {
					t.Errorf("usciti: %q, atteso 4007", got)
				}
			},
		},
		{
			Name:            "segretario: usciti, periodo tutto vecchio",
			Method:          http.MethodGet,
			URL:             url + "?kind=left&from=" + time.Now().AddDate(0, 0, -12).Format("2006-01-02") + "&to=" + time.Now().AddDate(0, 0, -9).Format("2006-01-02"),
			Headers:         map[string]string{"Authorization": tokens["4001"]},
			ExpectedStatus:  http.StatusOK,
			ExpectedContent: []string{"PK"},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				if got := sheetMembers(t, res); got != "" {
					t.Errorf("usciti: %q, atteso nessuno", got)
				}
			},
		},
		{
			Name:            "super: usciti, tutti",
			Method:          http.MethodGet,
			URL:             url + "?kind=left",
			Headers:         map[string]string{"Authorization": tokens["4003"]},
			ExpectedStatus:  http.StatusOK,
			ExpectedContent: []string{"PK"},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				if got := sheetMembers(t, res); got != "4006,4007" && got != "4007,4006" {
					t.Errorf("usciti: %q, attesi 4006 e 4007", got)
				}
			},
		},
		{
			Name:            "super: attivi della regione",
			Method:          http.MethodGet,
			URL:             url + "?kind=active",
			Headers:         map[string]string{"Authorization": tokens["4003"]},
			ExpectedStatus:  http.StatusOK,
			ExpectedContent: []string{"PK"},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				rows := readSheet(t, res)
				var got []string
				for _, r := range rows[1:] {
					got = append(got, r[0])
					if len(r) > 5 && r[5] != "Attivo" {
						t.Errorf("movimento: %v", r)
					}
				}
				// per nome: Nuovo Socio, Segretario, Socio
				if strings.Join(got, ",") != "4005,4001,4002" {
					t.Errorf("attivi: %v", got)
				}
				for _, r := range rows {
					if strings.Contains(strings.Join(r, "|"), "privata@example.com") {
						t.Error("la mail originale non si esporta")
					}
				}
			},
		},
		{
			Name:            "kind sconosciuto",
			Method:          http.MethodGet,
			URL:             url + "?kind=tutti",
			Headers:         map[string]string{"Authorization": tokens["4001"]},
			ExpectedStatus:  http.StatusBadRequest,
			ExpectedContent: []string{`"status":400`},
		},
	}
	for _, s := range scenarios {
		s.TestAppFactory = factory
		s.BeforeTestFunc = register
		s.Test(t)
	}
}
