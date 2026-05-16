package hooks

import (
	"github.com/pocketbase/pocketbase/core"

	"mensadb/tools/dbtools"
)

// EventsAssignLocalOffice popola events.local_office in base al gruppo
// locale a cui appartiene il creatore (owner). Idempotente: se il campo e`
// gia` valorizzato (es. impostato esplicitamente dal client) non lo tocca.
//
// Gira su OnRecordCreate (prima del save) cosi` il valore e` committato
// insieme al resto del record.
func EventsAssignLocalOffice(e *core.RecordEvent) error {
	rec := e.Record
	if rec.GetString("local_office") != "" {
		return e.Next()
	}
	ownerID := rec.GetString("owner")
	if ownerID == "" {
		return e.Next()
	}
	if officeID := dbtools.ResolveUserLocalOffice(e.App, ownerID); officeID != "" {
		rec.Set("local_office", officeID)
	}
	return e.Next()
}
