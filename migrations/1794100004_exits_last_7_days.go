package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"

	"mensadb/tools/memberchanges"
)

// Le uscite del registro (left, moved_out) gli admin della sede le vedono
// solo per gli ultimi 7 giorni; i super tutte. Vedi
// memberchanges.ChangesReadRule.

func init() {
	m.Register(func(app core.App) error {
		return setChangesReadRule(app, memberchanges.ChangesReadRule)
	}, func(app core.App) error {
		return setChangesReadRule(app, memberchanges.ChangesReadRuleBefore7Days)
	})
}
