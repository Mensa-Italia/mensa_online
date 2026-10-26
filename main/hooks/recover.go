package hooks

import (
	"runtime/debug"

	"github.com/pocketbase/pocketbase/core"
)

// recoverAsyncHook protegge le goroutine lanciate dagli hook.
//
// PocketBase installa un panic recover solo sulla catena HTTP: una goroutine
// nuda che va in panic porta giu` l'intero processo. Gli hook async degli
// stamp facevano esattamente questo (per esempio chiudendo un blob nil per un
// evento senza copertina), quindi un singolo record malformato poteva
// riavviare il backend invece di far fallire la sola operazione.
func recoverAsyncHook(app core.App, name string) {
	if r := recover(); r != nil {
		app.Logger().Error("panic in async hook",
			"hook", name,
			"panic", r,
			"stack", string(debug.Stack()),
		)
	}
}
