package migrations

import (
	"fmt"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Cambia il target della relation org_chart_members.user da "users" a
// "members_registry". I valori esistenti (id socio = id tessera) restano
// validi perche` entrambe le collection sono chiavate sull'id tessera.
// In-place: nessuna perdita di dati.
//
// Implementazione: PocketBase il validator di schema BLOCCA il cambio del
// collectionId su un relation field esistente (errore "The relation
// collection cannot be changed."). Aggiriamo aggiornando direttamente
// _collections.fields via SQL e ricaricando la cache.
func init() {
	m.Register(func(app core.App) error {
		return swapRelationTarget(app, "org_chart_members", "user", "members_registry")
	}, func(app core.App) error {
		return swapRelationTarget(app, "org_chart_members", "user", "users")
	})
}

func swapRelationTarget(app core.App, collection, fieldName, newTarget string) error {
	col, err := app.FindCollectionByNameOrId(collection)
	if err != nil {
		return err
	}
	target, err := app.FindCollectionByNameOrId(newTarget)
	if err != nil {
		return fmt.Errorf("target collection %q not found: %w", newTarget, err)
	}

	idx := -1
	for i, f := range col.Fields {
		if f.GetName() == fieldName {
			idx = i
			break
		}
	}
	if idx == -1 {
		return fmt.Errorf("field %q not found on %s", fieldName, collection)
	}

	// json_set sostituisce solo il valore di collectionId, lasciando intatti
	// gli altri attributi del field.
	path := fmt.Sprintf("$[%d].collectionId", idx)
	_, err = app.DB().NewQuery(
		"UPDATE _collections SET fields = json_set(fields, {:path}, {:newId}) WHERE id = {:colId}",
	).Bind(dbx.Params{
		"path":  path,
		"newId": target.Id,
		"colId": col.Id,
	}).Execute()
	if err != nil {
		return fmt.Errorf("update _collections: %w", err)
	}

	app.ReloadCachedCollections()
	return nil
}
