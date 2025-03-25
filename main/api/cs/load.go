package cs

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
	"mensadb/main/api/cs/exapp"
	"mensadb/main/api/cs/keys"
	"mensadb/main/api/cs/sign_payload"
	"mensadb/main/api/cs/verify_signature"
)

func Load(e *router.RouterGroup[*core.RequestEvent]) {
	keys.Load(e.Group("/keys"))
	sign_payload.Load(e.Group("/sign-payload"))
	verify_signature.Load(e.Group("/verify-signature"))
	exapp.Load(e.Group("/exapp"))

	e.POST("/auth-with-area", AuthWithAreaHandler)
	e.POST("/send-update-notify", SendUpdateNotifyHandler)
	e.GET("/force-update-addons", ForceUpdateAddonsHandler)
	e.GET("/force-notification", forceNotification)
	e.GET("/force-update-state-managers", ForceUpdateStateManagersHandler)
	e.GET("/force-update-docs", ForceUpdateDocsHandler)
	e.Any("/example", example)
}
