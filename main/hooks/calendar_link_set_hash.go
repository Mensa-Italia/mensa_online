package hooks

import (
	"github.com/pocketbase/pocketbase/core"
	"mensadb/tools/generic"
)

func CalendarSetHash(e *core.RecordEvent) error {
	e.Record.Set("hash", generic.RandomHash())
	if err := e.App.Save(e.Record); err != nil {
		e.App.Logger().Error("save record failed", "collection", e.Record.Collection().Name, "id", e.Record.Id, "err", err)
	}
	return e.Next()
}
