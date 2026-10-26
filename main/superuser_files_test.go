package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

// I dati di prova sono quelli che PocketBase distribuisce con il pacchetto
// tests: superuser test@example.com, utenti test@example.com in users e in
// clients, tutti con password 1234567890, e un avatar sul record users
// 4q1xlclmfloku33.
const testAvatarURL = "/api/files/_pb_users_auth_/4q1xlclmfloku33/300_1SEi6Q6U72.png"

func authToken(t testing.TB, collection, email string) string {
	t.Helper()

	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()

	record, err := app.FindAuthRecordByEmail(collection, email)
	if err != nil {
		t.Fatal(err)
	}
	token, err := record.NewAuthToken()
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// bindTestFileAuth riproduce il controllo di OnFileDownloadRequest in main.go
// con FILE_LINK_REQUIRE_AUTH=true e S3 spento.
func bindTestFileAuth(app core.App) {
	app.OnFileDownloadRequest().BindFunc(func(e *core.FileDownloadRequestEvent) error {
		if !isFileRequestAuthenticated(e) {
			return e.NotFoundError("", nil)
		}
		return e.Next()
	})
}

func findCookie(res *http.Response) *http.Cookie {
	for _, c := range res.Cookies() {
		if c.Name == superuserFileCookieName {
			return c
		}
	}
	return nil
}

func TestSuperuserFileCookieOnAuth(t *testing.T) {
	scenarios := []tests.ApiScenario{
		{
			Name:            "il login di un superuser lascia il cookie",
			Method:          http.MethodPost,
			URL:             "/api/collections/_superusers/auth-with-password",
			Body:            strings.NewReader(`{"identity":"test@example.com","password":"1234567890"}`),
			BeforeTestFunc:  func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) { bindSuperuserFileCookie(app) },
			ExpectedStatus:  http.StatusOK,
			ExpectedContent: []string{`"token":`},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				c := findCookie(res)
				if c == nil {
					t.Fatal("cookie assente")
					return
				}
				if c.Value == "" || !c.HttpOnly || !c.Secure || c.Path != superuserFileCookiePath || c.SameSite != http.SameSiteStrictMode {
					t.Fatalf("cookie con attributi sbagliati: %+v", c)
				}
				if c.MaxAge <= 0 {
					t.Fatalf("MaxAge = %d", c.MaxAge)
				}
			},
		},
		{
			Name:            "il login di un utente di un'altra collection auth non lascia il cookie",
			Method:          http.MethodPost,
			URL:             "/api/collections/clients/auth-with-password",
			Body:            strings.NewReader(`{"identity":"test@example.com","password":"1234567890"}`),
			BeforeTestFunc:  func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) { bindSuperuserFileCookie(app) },
			ExpectedStatus:  http.StatusOK,
			ExpectedContent: []string{`"token":`},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				if c := findCookie(res); c != nil {
					t.Fatalf("cookie inatteso: %+v", c)
				}
			},
		},
		{
			Name:   "con l'MFA il primo fattore non lascia il cookie",
			Method: http.MethodPost,
			URL:    "/api/collections/_superusers/auth-with-password",
			Body:   strings.NewReader(`{"identity":"test@example.com","password":"1234567890"}`),
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				superusers, err := app.FindCollectionByNameOrId(core.CollectionNameSuperusers)
				if err != nil {
					t.Fatal(err)
				}
				superusers.MFA.Enabled = true
				superusers.OTP.Enabled = true
				if err := app.Save(superusers); err != nil {
					t.Fatal(err)
				}
				bindSuperuserFileCookie(app)
			},
			ExpectedStatus:  http.StatusUnauthorized,
			ExpectedContent: []string{`"mfaId":`},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				if c := findCookie(res); c != nil {
					t.Fatalf("cookie inatteso: %+v", c)
				}
			},
		},
	}

	for _, s := range scenarios {
		s.Test(t)
	}
}

func TestFileDownloadWithSuperuserCookie(t *testing.T) {
	superuserToken := authToken(t, core.CollectionNameSuperusers, "test@example.com")
	userToken := authToken(t, "users", "test@example.com")
	bind := func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) { bindTestFileAuth(app) }

	scenarios := []tests.ApiScenario{
		{
			Name:            "senza credenziali: 404",
			Method:          http.MethodGet,
			URL:             testAvatarURL,
			BeforeTestFunc:  bind,
			ExpectedStatus:  http.StatusNotFound,
			ExpectedContent: []string{`"status":404`},
		},
		{
			Name:               "cookie di un superuser: 200",
			Method:             http.MethodGet,
			URL:                testAvatarURL,
			Headers:            map[string]string{"Cookie": superuserFileCookieName + "=" + superuserToken},
			BeforeTestFunc:     bind,
			ExpectedStatus:     http.StatusOK,
			NotExpectedContent: []string{`"status":404`},
		},
		{
			Name:            "cookie con il token di un utente users: 404",
			Method:          http.MethodGet,
			URL:             testAvatarURL,
			Headers:         map[string]string{"Cookie": superuserFileCookieName + "=" + userToken},
			BeforeTestFunc:  bind,
			ExpectedStatus:  http.StatusNotFound,
			ExpectedContent: []string{`"status":404`},
		},
		{
			Name:            "cookie con un token non valido: 404",
			Method:          http.MethodGet,
			URL:             testAvatarURL,
			Headers:         map[string]string{"Cookie": superuserFileCookieName + "=nope"},
			BeforeTestFunc:  bind,
			ExpectedStatus:  http.StatusNotFound,
			ExpectedContent: []string{`"status":404`},
		},
		{
			Name:    "cookie di un superuser da un IP fuori whitelist: 404",
			Method:  http.MethodGet,
			URL:     testAvatarURL,
			Headers: map[string]string{"Cookie": superuserFileCookieName + "=" + superuserToken},
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				app.Settings().SuperuserIPs = []string{"10.0.0.0/8"}
				bindTestFileAuth(app)
			},
			ExpectedStatus:  http.StatusNotFound,
			ExpectedContent: []string{`"status":404`},
		},
		{
			Name:               "header Authorization di un superuser: 200",
			Method:             http.MethodGet,
			URL:                testAvatarURL,
			Headers:            map[string]string{"Authorization": superuserToken},
			BeforeTestFunc:     bind,
			ExpectedStatus:     http.StatusOK,
			NotExpectedContent: []string{`"status":404`},
		},
	}

	for _, s := range scenarios {
		s.Test(t)
	}
}
