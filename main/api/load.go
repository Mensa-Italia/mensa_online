package api

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
	"mensadb/main/api/cs"
	"mensadb/main/api/position"
)

func Load(e *router.RouterGroup[*core.RequestEvent]) {
	position.Load(e.Group("/position"))
	cs.Load(e.Group("/cs"))
}
