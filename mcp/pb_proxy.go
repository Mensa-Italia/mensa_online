package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// pbServerAddr e` l'indirizzo TCP su cui PocketBase ascolta (es. "0.0.0.0:8090").
// Settato all'OnServe da SetServerAddr, usato per i loopback HTTP che fanno i
// tool MCP per "passare per la porta giusta" e applicare automaticamente
// listRule / viewRule delle collection.
var (
	pbServerAddrMu sync.RWMutex
	pbServerAddr   string
)

// SetServerAddr salva l'address del server HTTP PB. Chiamato dal main su
// OnServe (e.Server.Addr) prima di registrare il tool MCP.
func SetServerAddr(addr string) {
	pbServerAddrMu.Lock()
	defer pbServerAddrMu.Unlock()
	pbServerAddr = normalizeLoopback(addr)
}

func getServerAddr() string {
	pbServerAddrMu.RLock()
	defer pbServerAddrMu.RUnlock()
	return pbServerAddr
}

// normalizeLoopback trasforma "0.0.0.0:N" o ":N" in "127.0.0.1:N" cosi`
// http.Get funziona anche se PB ascolta su tutte le interfacce. Se l'address
// non ha host, assume 127.0.0.1.
func normalizeLoopback(addr string) string {
	if addr == "" {
		return ""
	}
	if strings.HasPrefix(addr, ":") {
		return "127.0.0.1" + addr
	}
	if strings.HasPrefix(addr, "0.0.0.0:") {
		return "127.0.0.1:" + strings.TrimPrefix(addr, "0.0.0.0:")
	}
	return addr
}

// resolveUserFromClaims trova il record `users` corrispondente all'utente
// Zitadel autenticato. Vedi resolveUserFromClaimsCtx — questa wrapper non
// prova il fallback su /oidc/v1/userinfo perche` manca il bearer; e` utile
// solo per code path che gia` hanno l'email nei claims.
func resolveUserFromClaims(app core.App, c *Claims) (*core.Record, error) {
	return resolveUserFromClaimsCtx(context.Background(), app, c, "")
}

// resolveUserFromClaimsCtx fa il mapping Zitadel → users PB. Strategie:
//
//   0. lookup in user_zitadel_auth (DB cache) per claims.Subject — HOT PATH
//      che salta tutte le chiamate a Zitadel dopo il primo login dell'utente.
//   1. claims.Email su users.email — canonico se Zitadel mette email nei claim
//   2. /oidc/v1/userinfo — Zitadel di default mette email solo nell'id_token,
//      non nell'access_token; con userinfo la riceviamo dietro al bearer.
//   3. claims.Subject come users.id — federazione SSO con id propagato
//   4. email (claim o userinfo) come members_registry.alias_mail/original_mail
//      → l'id del socio, prova quello come users.id
//
// Al primo successo, scrive una riga in user_zitadel_auth per accorciare i
// run successivi.
func resolveUserFromClaimsCtx(ctx context.Context, app core.App, c *Claims, bearer string) (*core.Record, error) {
	if c == nil {
		return nil, fmt.Errorf("no MCP claims in request context")
	}

	// Strategia 0: lookup nel DB cache user_zitadel_auth.
	if c.Subject != "" {
		if u, ok := lookupCachedUser(app, c.Subject); ok {
			return u, nil
		}
	}

	tried := []string{}
	email := c.Email

	tryUserByEmail := func(e string) *core.Record {
		if e == "" {
			return nil
		}
		recs, err := app.FindRecordsByFilter("users",
			"email = {:e}", "", 1, 0, dbx.Params{"e": e},
		)
		if err == nil && len(recs) > 0 {
			return recs[0]
		}
		return nil
	}

	tryUserByMembersRegistry := func(e string) *core.Record {
		if e == "" {
			return nil
		}
		for _, field := range []string{"alias_mail", "original_mail"} {
			recs, err := app.FindRecordsByFilter("members_registry",
				field+" = {:e}", "", 1, 0, dbx.Params{"e": e},
			)
			if err == nil && len(recs) > 0 {
				if u, err := app.FindRecordById("users", recs[0].Id); err == nil && u != nil {
					return u
				}
			}
		}
		return nil
	}

	// Strategia 1: email gia` nei claims.
	if email != "" {
		tried = append(tried, "users.email(claims)")
		if rec := tryUserByEmail(email); rec != nil {
			persistMapping(app, c.Subject, rec.Id, email)
			return rec, nil
		}
	}

	// Strategia 2: chiama Zitadel /oidc/v1/userinfo. Zitadel mette l'email
	// nell'id_token ma NON nell'access_token (config-dipendente). userinfo
	// con scope `email` autorizzato la ritorna sempre.
	if email == "" && bearer != "" {
		tried = append(tried, "userinfo")
		ui, err := fetchUserinfo(ctx, bearer)
		if err != nil {
			app.Logger().Warn("[mcp] userinfo fetch fallita", "err", err)
		} else if ui.Email != "" {
			email = ui.Email
			if rec := tryUserByEmail(email); rec != nil {
				persistMapping(app, c.Subject, rec.Id, email)
				return rec, nil
			}
		}
	}

	// Strategia 3: id Zitadel = users.id (federazione SSO con id propagato).
	if c.Subject != "" {
		tried = append(tried, "users.id")
		if rec, err := app.FindRecordById("users", c.Subject); err == nil && rec != nil {
			persistMapping(app, c.Subject, rec.Id, email)
			return rec, nil
		}
	}

	// Strategia 4: email → members_registry.{alias_mail,original_mail} → users.
	if email != "" {
		tried = append(tried, "members_registry")
		if rec := tryUserByMembersRegistry(email); rec != nil {
			persistMapping(app, c.Subject, rec.Id, email)
			return rec, nil
		}
	}

	app.Logger().Warn("[mcp] user resolution failed",
		"email", email, "subject", c.Subject, "tried", strings.Join(tried, ","))
	return nil, fmt.Errorf("MCP user non risolvibile: il token Zitadel "+
		"(email=%q, sub=%q) non corrisponde a nessun account utente nel "+
		"sistema. Probabili cause: (a) /oidc/v1/userinfo non ha ritornato "+
		"email (scope email non autorizzato sul client?), (b) l'utente non "+
		"si e` mai loggato nell'app Mensa Italia, (c) email diversa da "+
		"quella registrata in app. Tentati: %s",
		email, c.Subject, strings.Join(tried, ", "))
}

// userinfoResponse e` un subset utile dei campi che Zitadel ritorna sul
// /oidc/v1/userinfo. La risposta segue lo standard OIDC.
type userinfoResponse struct {
	Sub           string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	GivenName     string `json:"given_name"`
	FamilyName    string `json:"family_name"`
}

// fetchUserinfo chiama Zitadel /oidc/v1/userinfo con il bearer e ritorna
// le claim utente. La cache persistente sta in user_zitadel_auth (DB),
// quindi qui non serve nessuna cache in-memory: questa funzione viene
// chiamata solo al primo login di ogni utente.
func fetchUserinfo(ctx context.Context, bearer string) (*userinfoResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, userinfoURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("userinfo http: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("userinfo http %d: %s", resp.StatusCode, string(body))
	}
	var ui userinfoResponse
	if err := json.NewDecoder(resp.Body).Decode(&ui); err != nil {
		return nil, fmt.Errorf("userinfo decode: %w", err)
	}
	return &ui, nil
}

// lookupCachedUser cerca un mapping (zitadel_sub → users PB) in
// user_zitadel_auth e ritorna il record users se presente.
func lookupCachedUser(app core.App, sub string) (*core.Record, bool) {
	if sub == "" {
		return nil, false
	}
	recs, err := app.FindRecordsByFilter("user_zitadel_auth",
		"zitadel_sub = {:s}", "", 1, 0, dbx.Params{"s": sub},
	)
	if err != nil || len(recs) == 0 {
		return nil, false
	}
	userID := recs[0].GetString("user")
	if userID == "" {
		return nil, false
	}
	u, err := app.FindRecordById("users", userID)
	if err != nil || u == nil {
		return nil, false
	}
	return u, true
}

// persistMapping upserta (zitadel_sub, user, email) in user_zitadel_auth.
// Idempotente: in caso di conflict (unique sui sub) aggiorna il record
// esistente. Errori loggati ma non propagati: la risoluzione e` gia` andata
// a buon fine e il chiamante non deve fallire per un problema di cache.
func persistMapping(app core.App, sub, userID, email string) {
	if sub == "" || userID == "" {
		return
	}
	col, err := app.FindCollectionByNameOrId("user_zitadel_auth")
	if err != nil {
		app.Logger().Warn("[mcp] user_zitadel_auth collection mancante", "err", err)
		return
	}
	existing, _ := app.FindFirstRecordByFilter(col,
		"zitadel_sub = {:s}", dbx.Params{"s": sub})
	rec := existing
	if rec == nil {
		rec = core.NewRecord(col)
		rec.Set("zitadel_sub", sub)
	}
	rec.Set("user", userID)
	if email != "" {
		rec.Set("email", email)
	}
	if err := app.Save(rec); err != nil {
		app.Logger().Warn("[mcp] persist user_zitadel_auth fallito", "sub", sub, "err", err)
	}
}


// pbCall esegue una request HTTP verso PocketBase (loopback) impersonando
// l'utente autenticato via MCP. Cosi` le rule esistenti su listRule/viewRule
// si applicano in automatico — niente reimplementazione lato MCP.
//
// Ritorna lo status code HTTP e il body (json bytes) cosi` chi chiama puo`
// scegliere se rilanciare o trasformare l'errore.
func pbCall(ctx context.Context, app core.App, claims *Claims, method, path string, query url.Values, body any) (int, []byte, error) {
	addr := getServerAddr()
	if addr == "" {
		return 0, nil, fmt.Errorf("internal: pb server addr non inizializzato")
	}
	bearer, _ := BearerFromContext(ctx)
	user, err := resolveUserFromClaimsCtx(ctx, app, claims, bearer)
	if err != nil {
		return 0, nil, fmt.Errorf("auth: %w", err)
	}
	token, err := user.NewAuthToken()
	if err != nil {
		return 0, nil, fmt.Errorf("auth token: %w", err)
	}

	endpoint := "http://" + addr + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}

	var reqBody io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return 0, nil, fmt.Errorf("marshal body: %w", err)
		}
		reqBody = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, reqBody)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("pb call: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	rawBody, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20)) // 16 MB cap
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("read pb body: %w", err)
	}
	return resp.StatusCode, rawBody, nil
}

// pbCollectionList wrappa pbCall sul listing standard di una collection PB.
// query supporta filter, sort, page, perPage, expand, fields (formato PB).
func pbCollectionList(ctx context.Context, app core.App, claims *Claims, collection string, q url.Values) (int, []byte, error) {
	return pbCall(ctx, app, claims, http.MethodGet,
		"/api/collections/"+collection+"/records", q, nil)
}

// pbCollectionGet wrappa pbCall sul fetch by-id standard.
func pbCollectionGet(ctx context.Context, app core.App, claims *Claims, collection, id string, q url.Values) (int, []byte, error) {
	return pbCall(ctx, app, claims, http.MethodGet,
		"/api/collections/"+collection+"/records/"+id, q, nil)
}

// rawJSON riempie l'output MCP con il body restituito da PB cosi` com'e`
// (json). Se status >= 400, lo wrappa in un errore.
func rawJSON(status int, body []byte) (string, error) {
	if status >= 400 {
		return "", fmt.Errorf("pb error %d: %s", status, truncate(body, 500))
	}
	return string(body), nil
}

func truncate(b []byte, max int) string {
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "…"
}

