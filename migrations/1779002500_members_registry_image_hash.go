package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Aggiunge image_hash alla collezione members_registry: SHA256 esadecimale
// dei bytes dell'immagine attualmente salvata. Usato dal cron di sync per
// evitare di sovrascrivere il file (e quindi cambiarne il nome interno,
// invalidando la cache client) quando i bytes scaricati sono identici a
// quelli gia` storati.
func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("members_registry")
		if err != nil {
			return err
		}
		if col.Fields.GetByName("image_hash") == nil {
			col.Fields.Add(&core.TextField{Name: "image_hash", Max: 100})
		}
		return app.Save(col)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("members_registry")
		if err != nil {
			return nil
		}
		if f := col.Fields.GetByName("image_hash"); f != nil {
			col.Fields.RemoveByName("image_hash")
		}
		return app.Save(col)
	})
}
