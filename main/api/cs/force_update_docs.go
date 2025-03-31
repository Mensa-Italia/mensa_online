package cs

import (
	"github.com/pocketbase/pocketbase/core"
	"mensadb/tools/dbtools"
)

func ForceUpdateDocsHandler(e *core.RequestEvent) error {
	go dbtools.RemoteRetrieveDocumentsFromArea32(e.App)
	return e.String(200, "OK")
}
