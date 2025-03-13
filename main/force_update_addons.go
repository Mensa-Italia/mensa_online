package main

import (
	"github.com/pocketbase/pocketbase/core"
	"mensadb/tools/dbtools"
)

func ForceUpdateAddonsHandler(e *core.RequestEvent) error {
	go dbtools.RemoteUpdateAddons(e.App)
	return e.String(200, "OK")
}
