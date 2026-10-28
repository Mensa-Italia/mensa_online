package exapp

import (
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
)

func Load(e *router.RouterGroup[*core.RequestEvent]) {
	e.POST("/request", externalAppRequireConfirmation)
	e.POST("/valid", checkValidity)
	e.POST("/store-user-ticket", StoreUserTickets)
	e.POST("/delete-user-ticket", RemoveUserTickets)

	// Export dei dati dell'utente verso l'app esterna, con consenso monouso
	// (vedi export_store.go). request e fetch si autenticano con la chiave
	// sk_ dell'app (come /request), decide e` dell'utente loggato in app.
	export := e.Group("/export")
	export.POST("/request", exportRequestHandler)
	export.POST("/decide", exportDecideHandler).Bind(apis.RequireAuth())
	export.POST("/fetch", exportFetchHandler)
}
