package cs

import (
	"github.com/pocketbase/pocketbase/core"
	"mensadb/tools/dbtools"
)

func ForceUpdateStateManagersHandler(e *core.RequestEvent) error {
	if !dbtools.RequireSuperuser(e) {
		return e.String(401, "Unauthorized")
	}
	go dbtools.RefreshUserStatesManagersPowers(e.App)
	return e.String(200, "OK")
}
