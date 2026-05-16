package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Sessioni di test pubbliche organizzate dai gruppi locali (date in cui
// la segreteria fa sostenere il test di ammissione a piu` candidati
// contemporaneamente). Una riga per ogni data programmata da un local_office.
//
// Permessi:
//   - lettura: chiunque sia autenticato (le date dei test sono informazione
//     pubblica nel circuito soci)
//   - scrittura: solo gli admin (segretari + co-segretari) del local_office
//     che ospita la sessione. Gli assistenti al test NON possono modificare
//     queste date dal momento che non rientrano in local_offices_admins.
func init() {
	m.Register(func(app core.App) error {
		offices, err := app.FindCollectionByNameOrId("local_offices")
		if err != nil {
			return err
		}
		col := core.NewBaseCollection("local_offices_test_dates")

		col.Fields.Add(&core.RelationField{
			Name:          "local_office",
			Required:      true,
			MaxSelect:     1,
			CollectionId:  offices.Id,
			CascadeDelete: true,
		})
		col.Fields.Add(&core.DateField{Name: "date", Required: true})
		col.Fields.Add(&core.TextField{Name: "location", Max: 255})
		col.Fields.Add(&core.TextField{Name: "notes", Max: 2000})
		col.Fields.Add(&core.NumberField{Name: "max_participants"})
		col.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})
		col.Fields.Add(&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true})

		authedRule := "@request.auth.id != \"\""
		adminRule := "@request.auth.id ?= local_office.local_offices_admins_via_local_office.user.id"
		col.ListRule = &authedRule
		col.ViewRule = &authedRule
		col.CreateRule = &adminRule
		col.UpdateRule = &adminRule
		col.DeleteRule = &adminRule

		col.AddIndex("idx_local_offices_test_dates_office", false, "local_office", "")
		col.AddIndex("idx_local_offices_test_dates_date", false, "date", "")

		return app.Save(col)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("local_offices_test_dates")
		if err != nil {
			return nil
		}
		return app.Delete(col)
	})
}
