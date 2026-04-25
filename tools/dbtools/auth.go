package dbtools

import "github.com/pocketbase/pocketbase/core"

// RequireSuperuser ritorna true se la richiesta è autenticata come superuser PocketBase.
// In caso negativo, NON scrive sulla response: il chiamante decide come rispondere.
func RequireSuperuser(e *core.RequestEvent) bool {
	if e.Auth == nil {
		return false
	}
	return e.Auth.IsSuperuser()
}
