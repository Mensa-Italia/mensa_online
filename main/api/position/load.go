package position

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
)

func Load(e *router.RouterGroup[*core.RequestEvent]) {
	e.GET("/state", GetStateHandler)
}
