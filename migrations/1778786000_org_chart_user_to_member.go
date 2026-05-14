package migrations

import (
	"fmt"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Cambia il target della relation org_chart_members.user da "users" a
// "members_registry". I valori esistenti (id socio = id tessera) restano
// validi perche` entrambe le collection sono chiavate sull'id tessera.
// In-place: nessuna perdita di dati.
func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("org_chart_members")
		if err != nil {
			return err
		}
		members, err := app.FindCollectionByNameOrId("members_registry")
		if err != nil {
			return fmt.Errorf("members_registry not found: %w", err)
		}

		f := col.Fields.GetByName("user")
		if f == nil {
			return fmt.Errorf("field user not found on org_chart_members")
		}
		rf, ok := f.(*core.RelationField)
		if !ok {
			return fmt.Errorf("field user is not a relation field")
		}

		rf.CollectionId = members.Id
		return app.Save(col)
	}, func(app core.App) error {
		// Rollback: ripuntare a users.
		col, err := app.FindCollectionByNameOrId("org_chart_members")
		if err != nil {
			return nil
		}
		users, err := app.FindCollectionByNameOrId("users")
		if err != nil {
			return err
		}
		f := col.Fields.GetByName("user")
		if f == nil {
			return nil
		}
		rf, ok := f.(*core.RelationField)
		if !ok {
			return nil
		}
		rf.CollectionId = users.Id
		return app.Save(col)
	})
}
