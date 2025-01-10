package main

import (
	"github.com/pocketbase/pocketbase/core"
)

func ForceUpdateStateManagersHandler(e *core.RequestEvent) error {
	go updateStateManagers()
	return e.String(200, "OK")
}
