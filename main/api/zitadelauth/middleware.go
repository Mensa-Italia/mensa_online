package zitadelauth

import (
	"mensadb/tools/dbtools"
	"mensadb/tools/zauth"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

const (
	// Priority pi`u bassa di pbLoadAuthToken: questo middleware gira prima
	// del loader nativo di PocketBase. Se valida un JWT Zitadel imposta
	// e.Auth e il loader nativo skippa (vede e.Auth != nil); se fallisce
	// lascia passare al loader nativo che proverra` il token come PB-token.
	MiddlewarePriority = apis.DefaultLoadAuthTokenMiddlewarePriority - 1
	MiddlewareID       = "zitadelLoadAuthToken"
)

// LoadAuth ritorna un middleware globale che intercetta gli access token
// Zitadel come bearer e popola e.Auth con il record PB users associato.
//
// Fallback: se il token non e' un JWT Zitadel (es. token PB nativo di
// versioni vecchie dell'app) o se la verifica fallisce, il middleware
// chiama e.Next() senza errore: il loader nativo PocketBase prosegue e
// prova il token come token PB standard.
func LoadAuth() *hook.Handler[*core.RequestEvent] {
	return &hook.Handler[*core.RequestEvent]{
		Id:       MiddlewareID,
		Priority: MiddlewarePriority,
		Func: func(e *core.RequestEvent) error {
			if e.Auth != nil {
				return e.Next()
			}

			token := extractBearer(e)
			if token == "" || !zauth.LooksLikeZitadelJWT(token) {
				return e.Next()
			}

			claims, err := zauth.VerifyAccessToken(e.Request.Context(), token)
			if err != nil {
				e.App.Logger().Debug("zitadel auth: token verify failed", "err", err)
				return e.Next()
			}

			sub := claims.Subject
			if sub == "" {
				return e.Next()
			}

			email, _ := claims.Claims["email"].(string)
			record, err := findUserByZitadelSub(e.App, sub, email)
			if err != nil || record == nil {
				e.App.Logger().Debug("zitadel auth: no PB user for sub", "sub", sub, "err", err)
				return e.Next()
			}

			e.Auth = record
			return e.Next()
		},
	}
}

func extractBearer(e *core.RequestEvent) string {
	raw := e.Request.Header.Get("Authorization")
	if raw == "" {
		return ""
	}
	return strings.TrimPrefix(raw, "Bearer ")
}

// findUserByZitadelSub risolve il record users a partire dal sub Zitadel.
// Strategia, in ordine di costo:
//  1. cache locale user_zitadel_auth (mapping sub -> users.id)
//  2. metadata Zitadel "membership_id" sul sub (chiamata gRPC, autoritativa)
//  3. lookup PB per email/preferred_username del JWT
// Ad ogni successo via 2 o 3 popoliamo lazy la cache.
func findUserByZitadelSub(app core.App, sub, email string) (*core.Record, error) {
	if mapping, err := app.FindFirstRecordByFilter(
		"user_zitadel_auth", "zitadel_sub = {:s}", dbx.Params{"s": sub},
	); err == nil && mapping != nil {
		userID := mapping.GetString("user")
		if userID != "" {
			if rec, err := app.FindRecordById("users", userID); err == nil && rec != nil {
				return rec, nil
			}
		}
	}

	// Fallback autoritativo: i metadati Zitadel del nostro user contengono
	// "membership_id" valorizzato con l'id PB (lo settiamo noi quando creiamo
	// l'utente Zitadel via zauth.CreateUser).
	if meta := zauth.GetUserMetadata(sub); meta != nil {
		if pbID := strings.TrimSpace(meta["membership_id"]); pbID != "" {
			if rec, err := app.FindRecordById("users", pbID); err == nil && rec != nil {
				dbtools.UpsertUserZitadelAuth(app, sub, rec.Id, email)
				return rec, nil
			}
		}
	}

	if email == "" {
		return nil, nil
	}
	user, err := app.FindFirstRecordByFilter(
		"users", "email = {:e}", dbx.Params{"e": strings.ToLower(email)},
	)
	if err != nil || user == nil {
		return user, err
	}
	dbtools.UpsertUserZitadelAuth(app, sub, user.Id, email)
	return user, nil
}
