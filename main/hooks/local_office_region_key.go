package hooks

import (
	"github.com/pocketbase/pocketbase/core"

	"mensadb/tools/regionkey"
)

// LocalOfficeSetRegionKey tiene local_offices.region_key allineata a region
// (tools/regionkey): e' un campo derivato, si riscrive a ogni create e
// update, anche quelli di tools/localofficesync che crea o rinomina le sedi.
// Gira prima del save, cosi' il valore e' committato insieme al resto.
func LocalOfficeSetRegionKey(e *core.RecordEvent) error {
	if e.Record.Collection().Fields.GetByName("region_key") != nil {
		e.Record.Set("region_key", regionkey.Of(e.Record.GetString("region")))
	}
	return e.Next()
}
