package migrations

import (
	_ "embed"
	"encoding/json"
	"errors"
	"strings"

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
//
// Le view che esistono gia` con la stessa query e le stesse regole restano
// fuori dall'import. Salvare una view la valida eseguendone la query, e in
// produzione quella di users_notifications_region (join di user_notifications
// con members_registry, ordinata) supera il timeout di 30s delle query: il
// primo deploy di questa migrazione si e` fermato li`. Una view identica non
// ha niente da aggiornare, quindi non serve toccarla.

//go:embed production_schema.json
var productionSchema []byte

func init() {
	m.Register(func(app core.App) error {
		toImport, err := collectionsToImport(app, productionSchema)
		if err != nil {
			return err
		}
		if len(toImport) > 0 {
			if err := app.ImportCollections(toImport, false); err != nil {
				return err
			}
		}
		return alignFieldOptions(app, productionSchema)
	}, func(app core.App) error {
		return errors.New("rollback della migrazione dello schema di produzione non supportato")
	})
}

// collectionsToImport ritorna le collection di schema da importare, cioe`
// tutte tranne le view che esistono gia` identiche (stessa query e stesse
// regole).
func collectionsToImport(app core.App, schema []byte) ([]map[string]any, error) {
	var collections []map[string]any
	if err := json.Unmarshal(schema, &collections); err != nil {
		return nil, err
	}

	toImport := make([]map[string]any, 0, len(collections))
	for _, c := range collections {
		if c["type"] == core.CollectionTypeView {
			id, _ := c["id"].(string)
			existing, err := app.FindCollectionByNameOrId(id)
			if err == nil && sameView(existing, c) {
				continue
			}
		}
		toImport = append(toImport, c)
	}
	return toImport, nil
}

func sameView(existing *core.Collection, imported map[string]any) bool {
	query, _ := imported["viewQuery"].(string)
	return existing.IsView() &&
		existing.Name == imported["name"] &&
		strings.TrimSpace(existing.ViewQuery) == strings.TrimSpace(query) &&
		sameRule(existing.ListRule, imported["listRule"]) &&
		sameRule(existing.ViewRule, imported["viewRule"]) &&
		sameRule(existing.CreateRule, imported["createRule"]) &&
		sameRule(existing.UpdateRule, imported["updateRule"]) &&
		sameRule(existing.DeleteRule, imported["deleteRule"])
}

// sameRule confronta una regola salvata (nil = solo superuser) con quella
// dello schema (null nel JSON).
func sameRule(existing *string, imported any) bool {
	rule, ok := imported.(string)
	if !ok {
		return existing == nil
	}
	return existing != nil && *existing == rule
}

// alignFieldOptions applica le opzioni dei campi di schema ai campi con lo
// stesso nome e tipo ma id diverso, che l'import ha lasciato come erano. Le
// view sono escluse: i loro campi li ricava PocketBase dalla query, e i loro
// id (_clone_*) cambiano a ogni salvataggio.
func alignFieldOptions(app core.App, schema []byte) error {
	var collections []struct {
		ID     string            `json:"id"`
		Type   string            `json:"type"`
		Fields []json.RawMessage `json:"fields"`
	}
	if err := json.Unmarshal(schema, &collections); err != nil {
		return err
	}

	for _, c := range collections {
		if c.Type == core.CollectionTypeView {
			continue
		}

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
