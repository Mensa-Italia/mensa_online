package migrations

import (
	"slices"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Export dei dati verso le app esterne, con consenso monouso.
//
//   - ex_keys.permissions: due permessi nuovi, EXPORT_STAMPS ed EXPORT_MEDALS.
//     Sono due perche' timbri e medaglie sono due richieste e due consensi
//     distinti: una chiave abilitata ai timbri non estrae le medaglie.
//     Attenzione: `permissions` e` un select a scelta singola (maxSelect = 1),
//     quindi una chiave porta UN permesso solo. Per un partner che vuole
//     entrambi i dati servono due chiavi, oppure alzare maxSelect dalla
//     dashboard: la migrazione non lo tocca perche' passare da single a multi
//     cambia il formato salvato di tutte le chiavi esistenti.
//   - ex_export_requests: una riga per richiesta di export (pending ->
//     approved -> consumed, oppure denied / expired). Nessuna regola API =
//     solo superuser: la gestisce il codice di main/api/cs/exapp, e il
//     passaggio approved -> consumed e` un UPDATE condizionale atomico.
//
// Idempotente: ogni pezzo si crea solo se manca.

const exportRequestsTable = "ex_export_requests"

var exportPermissions = []string{"EXPORT_STAMPS", "EXPORT_MEDALS"}

func init() {
	m.Register(upExExportRequests, downExExportRequests)
}

func upExExportRequests(app core.App) error {
	keys, err := app.FindCollectionByNameOrId("ex_keys")
	if err != nil {
		return err
	}
	if perms, ok := keys.Fields.GetByName("permissions").(*core.SelectField); ok {
		changed := false
		for _, p := range exportPermissions {
			if !slices.Contains(perms.Values, p) {
				perms.Values = append(perms.Values, p)
				changed = true
			}
		}
		if changed {
			if err := app.Save(keys); err != nil {
				return err
			}
		}
	}

	if existing, _ := app.FindCollectionByNameOrId(exportRequestsTable); existing != nil {
		return nil
	}

	exApps, err := app.FindCollectionByNameOrId("ex_apps")
	if err != nil {
		return err
	}
	users, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		return err
	}

	c := core.NewBaseCollection(exportRequestsTable)
	// Nessuna regola API: solo superuser.
	c.Fields.Add(
		&core.RelationField{Name: "ex_app", CollectionId: exApps.Id, MaxSelect: 1, Required: true, CascadeDelete: true},
		&core.RelationField{Name: "user", CollectionId: users.Id, MaxSelect: 1, Required: true, CascadeDelete: true},
		&core.SelectField{Name: "scope", Values: []string{"stamps", "medals"}, MaxSelect: 1, Required: true},
		&core.SelectField{
			Name:      "status",
			Values:    []string{"pending", "approved", "denied", "consumed", "expired"},
			MaxSelect: 1,
			Required:  true,
		},
		// URL di callback del partner: ci va l'app a dire l'esito. Lo salviamo
		// solo per tracciare la richiesta.
		&core.TextField{Name: "callme_url", Max: 2048},
		&core.DateField{Name: "expires_at", Required: true},
		&core.DateField{Name: "decided_at"},
		&core.DateField{Name: "consumed_at"},
		&core.AutodateField{Name: "created", OnCreate: true},
		&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
	)
	c.AddIndex("idx_ex_export_requests_user_status", false, "user, status", "")
	c.AddIndex("idx_ex_export_requests_status_expires", false, "status, expires_at", "")
	return app.Save(c)
}

func downExExportRequests(app core.App) error {
	if c, _ := app.FindCollectionByNameOrId(exportRequestsTable); c != nil {
		if err := app.Delete(c); err != nil {
			return err
		}
	}

	keys, err := app.FindCollectionByNameOrId("ex_keys")
	if err != nil {
		return nil
	}
	if perms, ok := keys.Fields.GetByName("permissions").(*core.SelectField); ok {
		kept := make([]string, 0, len(perms.Values))
		for _, v := range perms.Values {
			if !slices.Contains(exportPermissions, v) {
				kept = append(kept, v)
			}
		}
		if len(kept) != len(perms.Values) {
			perms.Values = kept
			return app.Save(keys)
		}
	}
	return nil
}
