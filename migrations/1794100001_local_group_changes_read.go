package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"

	"mensadb/tools/memberchanges"
)

// Gli arrivi del registro (joined, moved_in) diventano leggibili da ogni
// socio loggato: la pagina del gruppo locale mostra a tutti i nuovi soci
// della settimana. Le uscite restano agli admin della sede e ai super. Vedi
// memberchanges.ChangesReadRule.

func init() {
	m.Register(func(app core.App) error {
		return setChangesReadRule(app, memberchanges.ChangesReadRule)
	}, func(app core.App) error {
		return setChangesReadRule(app, memberchanges.AdminReadRule)
	})
}

func setChangesReadRule(app core.App, rule string) error {
	c, err := app.FindCollectionByNameOrId(memberchanges.Table)
	if err != nil {
		return err
	}
	c.ListRule = &rule
	c.ViewRule = &rule
	return app.Save(c)
}
