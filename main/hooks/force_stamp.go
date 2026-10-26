package hooks

import "github.com/pocketbase/pocketbase/core"

func ForceStampGen(e *core.RequestEvent) error {
	return e.Error(400, "This endpoint is not available", nil)
}
