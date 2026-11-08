package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"

	"mensadb/tools/officestats"
)

// Le cifre di tutta l'associazione (association_stats e
// association_stats_monthly, vedi tools/officestats) per il consiglio e i
// super; e org_chart_members leggibile con il login, come org_chart_groups:
// l'organigramma e' pensato per i soci, e la regola del consiglio
// (officestats.BoardReadRule) si appoggia a quella collection. Idempotente.

func init() {
	m.Register(func(app core.App) error {
		if err := officestats.EnsureCollections(app); err != nil {
			return err
		}
		return setOrgChartMembersReadRule(app, `@request.auth.id != ""`)
	}, func(app core.App) error {
		for _, name := range []string{officestats.AssociationTable, officestats.AssociationMonthlyTable} {
			if c, _ := app.FindCollectionByNameOrId(name); c != nil {
				if err := app.Delete(c); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func setOrgChartMembersReadRule(app core.App, rule string) error {
	c, err := app.FindCollectionByNameOrId("org_chart_members")
	if err != nil {
		return err
	}
	c.ListRule = &rule
	c.ViewRule = &rule
	return app.Save(c)
}
