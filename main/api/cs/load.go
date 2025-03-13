package cs

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
	"mensadb/main/api/cs/keys"
)

func Load(e *router.RouterGroup[*core.RequestEvent]) {
	keys.Load(e.Group("/keys"))
}
