package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Apre list/view a "qualsiasi socio autenticato" su tutte le tabelle
// del cluster local_offices (admin, assistenti, links). Le view pubbliche
// (view_local_office_*) restano accessibili anche senza auth e non vengono
// toccate qui.
//
// Le write rule sono lasciate intatte (gestite altrove con regole
// per-gruppo).
func init() {
	m.Register(func(app core.App) error {
		rule := "@request.auth.id != \"\""
		for _, name := range []string{
			"local_offices_admins",
			"local_offices_test_assistants",
			"local_offices_links",
		} {
			col, err := app.FindCollectionByNameOrId(name)
			if err != nil {
				return err
			}
			col.ListRule = &rule
			col.ViewRule = &rule
			if err := app.Save(col); err != nil {
				return err
			}
		}
		return nil
	}, func(app core.App) error {
		empty := ""
		for _, name := range []string{
			"local_offices_admins",
			"local_offices_test_assistants",
			"local_offices_links",
		} {
			col, err := app.FindCollectionByNameOrId(name)
			if err != nil {
				continue
			}
			col.ListRule = &empty
			col.ViewRule = &empty
			if err := app.Save(col); err != nil {
				return err
			}
		}
		return nil
	})
}
