package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Aggiunge data_hash su members_registry: SHA256 esadecimale dei campi
// anagrafici (name, city, birthdate, state, area, original_mail,
// alias_mail, full_data, is_active, full_profile_link). Usato insieme
// a image_hash dal cron di sync per saltare app.Save quando nulla e`
// cambiato, evitando di toccare i timestamp updated e di sporcare il
// changefeed PB.
func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("members_registry")
		if err != nil {
			return err
		}
		if col.Fields.GetByName("data_hash") == nil {
			col.Fields.Add(&core.TextField{Name: "data_hash", Max: 100})
		}
		return app.Save(col)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("members_registry")
		if err != nil {
			return nil
		}
		if f := col.Fields.GetByName("data_hash"); f != nil {
			col.Fields.RemoveByName("data_hash")
		}
		return app.Save(col)
	})
}
