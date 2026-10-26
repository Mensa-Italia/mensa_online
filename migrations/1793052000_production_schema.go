package migrations

import (
	_ "embed"
	"encoding/json"
	"errors"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// production_schema.json e` l'export delle collection di produzione
// (Dashboard -> Settings -> Export collections) al 26/10/2026. Sostituisce
// la vecchia baseline e le migrazioni incrementali che ci stavano sopra:
// in produzione erano gia` tutte applicate, e le righe che restano in
// _migrations per file che non esistono piu` vengono ignorate dal runner.
//
// Su un DB nuovo crea tutto lo schema; su produzione e` un no-op, perche`
// lo schema e` gia` quello. deleteMissing=false: una collection che non
// sta nell'export non viene toccata.
//
// Le modifiche successive vanno in nuove migrazioni accanto a questa.
//
// Unica differenza che resta fra un DB nuovo e produzione: gli id di
// users.name e users.avatar. Produzione e` nata con una PocketBase che li
// chiamava users_name/users_avatar, un DB nuovo li ha gia` creati con altri
// id, e l'import tiene il campo esistente (con le sue opzioni) per non
// droppare la colonna. alignFieldOptions riporta comunque le opzioni a
// quelle di produzione, lasciando l'id com'e`.

//go:embed production_schema.json
var productionSchema []byte

func init() {
	m.Register(func(app core.App) error {
		if err := app.ImportCollectionsByMarshaledJSON(productionSchema, false); err != nil {
			return err
		}
		return alignFieldOptions(app, productionSchema)
	}, func(app core.App) error {
		return errors.New("rollback della migrazione dello schema di produzione non supportato")
	})
}

// alignFieldOptions applica le opzioni dei campi di schema ai campi con lo
// stesso nome e tipo ma id diverso, che l'import ha lasciato come erano.
func alignFieldOptions(app core.App, schema []byte) error {
	var collections []struct {
		ID     string            `json:"id"`
		Fields []json.RawMessage `json:"fields"`
	}
	if err := json.Unmarshal(schema, &collections); err != nil {
		return err
	}

	for _, c := range collections {
		collection, err := app.FindCollectionByNameOrId(c.ID)
		if err != nil {
			return err
		}

		changed := false
		for _, raw := range c.Fields {
			var imported struct {
				ID   string `json:"id"`
				Name string `json:"name"`
				Type string `json:"type"`
			}
			if err := json.Unmarshal(raw, &imported); err != nil {
				return err
			}

			field := collection.Fields.GetByName(imported.Name)
			if field == nil || field.GetId() == imported.ID || field.Type() != imported.Type {
				continue
			}

			id := field.GetId()
			if err := json.Unmarshal(raw, field); err != nil {
				return err
			}
			field.SetId(id)
			changed = true
		}

		if changed {
			if err := app.Save(collection); err != nil {
				return err
			}
		}
	}

	return nil
}
