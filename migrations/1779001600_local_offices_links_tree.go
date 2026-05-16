package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Estende local_offices_links per supportare sezioni / sottosezioni:
//   - parent: self-relation opzionale → nesting arbitrario (root = parent nullo)
//   - kind:   "section" (contenitore, nessun URL) | "link" (foglia cliccabile)
//
// Le righe esistenti sono link cliccabili: kind default "link".
// L'url smette di essere required cosi` le sezioni possono lasciarlo vuoto.
func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("local_offices_links")
		if err != nil {
			return err
		}

		// parent: self-relation, nessun cascadeDelete (gestiamo l'orfanizzazione
		// a mano dal client se serve, evitiamo cancellazioni a cascata pericolose)
		col.Fields.Add(&core.RelationField{
			Name:         "parent",
			Required:     false,
			MaxSelect:    1,
			CollectionId: col.Id, // self-reference: id della stessa collection
		})

		col.Fields.Add(&core.SelectField{
			Name:      "kind",
			Required:  true,
			MaxSelect: 1,
			Values:    []string{"section", "link"},
		})

		// Rendi url non piu` required (sezioni senza URL).
		if f := col.Fields.GetByName("url"); f != nil {
			if uf, ok := f.(*core.URLField); ok {
				uf.Required = false
			}
		}

		col.AddIndex("idx_local_offices_links_parent", false, "parent", "")
		col.AddIndex("idx_local_offices_links_kind", false, "kind", "")

		if err := app.Save(col); err != nil {
			return err
		}

		// Backfill: tutte le righe esistenti diventano "link" (sono link
		// cliccabili creati prima dell'introduzione del concetto sezione).
		records, err := app.FindAllRecords("local_offices_links")
		if err != nil {
			return err
		}
		for _, rec := range records {
			if rec.GetString("kind") == "" {
				rec.Set("kind", "link")
				if err := app.Save(rec); err != nil {
					return err
				}
			}
		}
		return nil
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("local_offices_links")
		if err != nil {
			return nil
		}
		col.Fields.RemoveByName("parent")
		col.Fields.RemoveByName("kind")
		if f := col.Fields.GetByName("url"); f != nil {
			if uf, ok := f.(*core.URLField); ok {
				uf.Required = true
			}
		}
		return app.Save(col)
	})
}
