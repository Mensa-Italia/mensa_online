package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Aggiorna le view referenti per leggere i dati anagrafici direttamente da
// members_registry (e non piu` joinando users). Conseguenza dello swap di
// target della relation effettuato in 1779001900: ora le link table
// puntano a members_registry, quindi possiamo prendere name/image/email
// senza passare da users.
//
// Vantaggio: vediamo anche i referenti che non hanno mai installato l'app.
func init() {
	m.Register(func(app core.App) error {
		// Drop + recreate per cambiare i JOIN: PB non lascia editare la view
		// in-place quando cambiano colonne sorgente.
		for _, name := range []string{"view_local_office_admins", "view_local_office_assistants"} {
			if col, err := app.FindCollectionByNameOrId(name); err == nil {
				if err := app.Delete(col); err != nil {
					return err
				}
			}
		}

		empty := ""

		admins := core.NewViewCollection("view_local_office_admins")
		admins.ViewQuery = `SELECT
  loa.id AS id,
  lo.id AS local_office,
  lo.name AS local_office_name,
  lo.region AS region,
  mr.id AS user,
  mr.name AS name,
  mr.image AS image,
  mr.alias_mail AS email,
  loa.is_the_officer AS is_the_officer
FROM local_offices_admins loa
JOIN local_offices lo ON lo.id = loa.local_office
JOIN members_registry mr ON mr.id = loa.user
WHERE mr.is_active = 1`
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
  mr.id AS user,
  mr.name AS name,
  mr.image AS image,
  mr.alias_mail AS email
FROM local_offices_test_assistants lota
JOIN local_offices lo ON lo.id = lota.local_office
JOIN members_registry mr ON mr.id = lota.user
WHERE mr.is_active = 1`
		assistants.ListRule = &empty
		assistants.ViewRule = &empty
		return app.Save(assistants)
	}, func(app core.App) error {
		// In rollback non torniamo a JOIN su users: lasciamo che la down della
		// migration 1779001100 ricrei le view originali se necessario.
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
