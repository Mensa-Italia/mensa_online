package main

import (
	"github.com/pocketbase/pocketbase/core"
)

func ForceUpdateAddonsHandler(e *core.RequestEvent) error {
	go updateAddonsData()
	return e.String(200, "OK")
}
