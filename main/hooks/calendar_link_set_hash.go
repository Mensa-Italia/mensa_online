package hooks

import (
	"github.com/pocketbase/pocketbase/core"
	"mensadb/tools/generic"
)

func CalendarSetHash(e *core.RecordEvent) error {
	e.Record.Set("hash", generic.RandomHash())
	_ = e.App.Save(e.Record)
	return e.Next()
}
