package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// I "sigs" non sono piu` solo SIG (gruppi tematici nazionali): ospitano
// anche i gruppi locali / club / comunita` regionali. Quando un gruppo e`
// territoriale ha senso linkarlo al local_office competente, cosi` la
// pagina del gruppo locale puo` incrociarli e mostrare le sue community
// (linktree, eventi, sig affiliati).
//
// La relazione e` opzionale: i gruppi nazionali / online restano senza
// local_office.
func init() {
	m.Register(func(app core.App) error {
		sigs, err := app.FindCollectionByNameOrId("sigs")
		if err != nil {
			return err
		}
		offices, err := app.FindCollectionByNameOrId("local_offices")
		if err != nil {
			return err
		}
		sigs.Fields.Add(&core.RelationField{
			Name:         "local_office",
			Required:     false,
			MaxSelect:    1,
			CollectionId: offices.Id,
		})
		sigs.AddIndex("idx_sigs_local_office", false, "local_office", "")
		return app.Save(sigs)
	}, func(app core.App) error {
		sigs, err := app.FindCollectionByNameOrId("sigs")
		if err != nil {
			return nil
		}
		sigs.Fields.RemoveByName("local_office")
		return app.Save(sigs)
	})
}
