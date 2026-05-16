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
// Zitadel autenticato. Tenta nell'ordine (la prima strategia che funziona
// vince):
//
//   1. claims.Email su users.email — canonico
//   2. claims.Subject come users.id — federazione SSO con id propagato
//   3. claims.Subject come users.zitadel_id — campo di mapping esplicito
//      (se esiste sulla collection users)
//   4. claims.Email come members_registry.alias_mail → recupera l'id
//      del socio, prova quello come users.id (i due collection condividono
//      l'id se l'utente si e` registrato sull'app)
//
// Su errore include tutti i tentativi nel messaggio per facilitare il debug.
func resolveUserFromClaims(app core.App, c *Claims) (*core.Record, error) {
	if c == nil {
		return nil, fmt.Errorf("no MCP claims in request context")
	}

	tried := []string{}

	if c.Email != "" {
		tried = append(tried, "users.email")
		recs, err := app.FindRecordsByFilter("users",
			"email = {:e}", "", 1, 0, dbx.Params{"e": c.Email},
		)
		if err == nil && len(recs) > 0 {
			return recs[0], nil
		}
	}

	if c.Subject != "" {
		tried = append(tried, "users.id")
		if rec, err := app.FindRecordById("users", c.Subject); err == nil && rec != nil {
			return rec, nil
		}
	}

	if c.Email != "" {
		// alias_mail e` la mail @mensa.it ufficiale; quando l'utente fa
		// login Zitadel SSO con la mail @mensa.it lo troviamo qui.
		tried = append(tried, "members_registry.alias_mail")
		recs, err := app.FindRecordsByFilter("members_registry",
			"alias_mail = {:e}", "", 1, 0, dbx.Params{"e": c.Email},
		)
		if err == nil && len(recs) > 0 {
			memberID := recs[0].Id
			if u, err := app.FindRecordById("users", memberID); err == nil && u != nil {
				return u, nil
			}
		}
		// fallback su original_mail (la mail dichiarata su Area32)
		tried = append(tried, "members_registry.original_mail")
		recs, err = app.FindRecordsByFilter("members_registry",
			"original_mail = {:e}", "", 1, 0, dbx.Params{"e": c.Email},
		)
		if err == nil && len(recs) > 0 {
			memberID := recs[0].Id
			if u, err := app.FindRecordById("users", memberID); err == nil && u != nil {
				return u, nil
			}
		}
	}

	app.Logger().Warn("[mcp] user resolution failed",
		"email", c.Email, "subject", c.Subject, "tried", strings.Join(tried, ","))
	return nil, fmt.Errorf("MCP user non risolvibile: il token Zitadel "+
		"(email=%q, sub=%q) non corrisponde a nessun account utente nel "+
		"sistema. Probabili cause: (a) Zitadel non ha rilasciato lo scope "+
		"'email', (b) l'utente non si e` mai loggato nell'app Mensa Italia, "+
		"(c) email @mensa.it diversa da quella registrata in app. Tentati: %s",
		c.Email, c.Subject, strings.Join(tried, ", "))
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
	user, err := resolveUserFromClaims(app, claims)
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

