package cs

import (
	"github.com/pocketbase/pocketbase/core"
	"mensadb/tools/dbtools"
)

func ForceUpdateAddonsHandler(e *core.RequestEvent) error {
	if !dbtools.RequireSuperuser(e) {
		return e.String(401, "Unauthorized")
	}
	go dbtools.RemoteUpdateAddons(e.App)
	return e.String(200, "OK")
}
