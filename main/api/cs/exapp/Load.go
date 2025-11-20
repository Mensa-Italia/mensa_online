package exapp

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
)

func Load(e *router.RouterGroup[*core.RequestEvent]) {
	e.POST("/request", externalAppRequireConfirmation)
	e.POST("/valid", checkValidity)
	e.POST("/store-user-purchase", StoreUserPurchase)
}
