package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		groups := core.NewBaseCollection("org_chart_groups")
		groups.Fields.Add(&core.TextField{
			Name:     "title",
			Required: true,
			Max:      255,
		})
		groups.Fields.Add(&core.NumberField{
			Name: "order",
		})
		groups.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})
		groups.Fields.Add(&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true})
		// Lettura aperta a tutti gli utenti autenticati.
		authedRule := "@request.auth.id != ''"
		groups.ListRule = &authedRule
		groups.ViewRule = &authedRule
		if err := app.Save(groups); err != nil {
			return err
		}

		members := core.NewBaseCollection("org_chart_members")
		members.Fields.Add(&core.RelationField{
			Name:          "group",
			Required:      true,
			MaxSelect:     1,
			CollectionId:  groups.Id,
			CascadeDelete: true,
		})
		members.Fields.Add(&core.RelationField{
			Name:         "user",
			Required:     true,
			MaxSelect:    1,
			CollectionId: "_pb_users_auth_",
		})
		members.Fields.Add(&core.TextField{
			Name:     "role",
			Required: true,
			Max:      255,
		})
		members.Fields.Add(&core.BoolField{Name: "inactive"})
		members.Fields.Add(&core.NumberField{Name: "order"})
		members.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})
		members.Fields.Add(&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true})
		members.ListRule = &authedRule
		members.ViewRule = &authedRule
		members.AddIndex("idx_org_chart_members_group", false, "`group`", "")
		members.AddIndex("idx_org_chart_members_user", false, "`user`", "")
		return app.Save(members)
	}, func(app core.App) error {
		if col, err := app.FindCollectionByNameOrId("org_chart_members"); err == nil {
			_ = app.Delete(col)
		}
		if col, err := app.FindCollectionByNameOrId("org_chart_groups"); err == nil {
			_ = app.Delete(col)
		}
		return nil
	})
}
