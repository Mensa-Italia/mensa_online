package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Due view pubbliche separate per i referenti dei gruppi locali. Il
// validator di PocketBase per le view non accetta UNION ALL, quindi
// abbiamo dovuto spezzare per sorgente.
//
//   - view_local_office_admins:     segretari + cosegretari (con flag is_the_officer)
//   - view_local_office_assistants: assistenti al test
//
// Stessa struttura colonne (id, local_office, local_office_name, region,
// user, name, image, email). Lettura pubblica (no auth). Il client fa due
// query e concatena.
func init() {
	m.Register(func(app core.App) error {
		empty := ""

		admins := core.NewViewCollection("view_local_office_admins")
		admins.ViewQuery = `SELECT
  loa.id AS id,
  lo.id AS local_office,
  lo.name AS local_office_name,
  lo.region AS region,
  u.id AS user,
  mr.name AS name,
  mr.image AS image,
  mr.alias_mail AS email,
  loa.is_the_officer AS is_the_officer
FROM local_offices_admins loa
JOIN local_offices lo ON lo.id = loa.local_office
JOIN users u ON u.id = loa.user
LEFT JOIN members_registry mr ON mr.id = u.id
WHERE mr.is_active IS NULL OR mr.is_active = 1`
		admins.ListRule = &empty
		admins.ViewRule = &empty
		if err := app.Save(admins); err != nil {
			return err
		}

		assistants := core.NewViewCollection("view_local_office_assistants")
		assistants.ViewQuery = `SELECT
  lota.id AS id,
  lo.id AS local_office,
  lo.name AS local_office_name,
  lo.region AS region,
  u.id AS user,
  mr.name AS name,
  mr.image AS image,
  mr.alias_mail AS email
FROM local_offices_test_assistants lota
JOIN local_offices lo ON lo.id = lota.local_office
JOIN users u ON u.id = lota.user
LEFT JOIN members_registry mr ON mr.id = u.id
WHERE mr.is_active IS NULL OR mr.is_active = 1`
		assistants.ListRule = &empty
		assistants.ViewRule = &empty
		return app.Save(assistants)
	}, func(app core.App) error {
		for _, name := range []string{"view_local_office_admins", "view_local_office_assistants"} {
			if col, err := app.FindCollectionByNameOrId(name); err == nil {
				if err := app.Delete(col); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
