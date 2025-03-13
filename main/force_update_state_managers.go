package main

import (
	"github.com/pocketbase/pocketbase/core"
	"mensadb/tools/dbtools"
)

func ForceUpdateStateManagersHandler(e *core.RequestEvent) error {
	go dbtools.RefreshUserStatesManagersPowers(e.App)
	return e.String(200, "OK")
}
