package migrations

import (
	"errors"
	"fmt"
	"strings"

	"mensadb/tools/memberchanges"
	"mensadb/tools/officestats"
	"mensadb/tools/regionkey"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Il gruppo locale: la pagina della sede regionale nell'app, con un'area
// riservata a segretario, co-segretari e amministratori.
//
//   - local_offices.region_key: la chiave di regione accorpata
//     (tools/regionkey), riempita qui per le sedi che ci sono e tenuta
//     allineata da un hook (main/hooks); l'app la usa per trovare la sede del
//     socio. Gli admin della sede non la possono cambiare dall'API, come
//     name, region e slug;
//   - local_offices_admins: creazione e cancellazione solo superuser. La
//     regola di creazione era aperta (""): qualunque socio poteva mettersi
//     admin di qualunque sede, e con l'area segreteria avrebbe letto mail e
//     statistiche. La riempie tools/localofficesync, che salva dall'app;
//   - members_registry_changes: i movimenti del registro soci
//     (tools/memberchanges), leggibili dagli admin della sede e dai super;
//   - local_office_stats e local_office_stats_monthly: le statistiche delle
//     sedi (tools/officestats), la prima leggibile da ogni socio loggato, la
//     seconda dagli admin della sede e dai super.
//
// In fondo un primo calcolo delle statistiche, a miglior sforzo: se non
// riesce ci pensa il cron. Il backfill dei movimenti dagli snapshot S3 non
// gira qui (scarica centinaia di file): parte all'avvio del server, in
// background.
//
// Idempotente: campi e collection si aggiungono solo se mancano, le regole
// si riscrivono uguali.

func init() {
	m.Register(upLocalGroup, func(app core.App) error {
		return errors.New("rollback del gruppo locale non supportato: la storia dei movimenti andrebbe persa")
	})
}

func upLocalGroup(app core.App) error {
	if err := localOfficesRegionKey(app); err != nil {
		return err
	}
	if err := closeLocalOfficesAdmins(app); err != nil {
		return err
	}
	if err := memberchanges.EnsureCollection(app); err != nil {
		return err
	}
	if err := officestats.EnsureCollections(app); err != nil {
		return err
	}

	if err := officestats.Refresh(app); err != nil {
		app.Logger().Warn("[officestats] primo calcolo non riuscito, ci pensa il cron", "err", err)
	}
	return nil
}

// localOfficesRegionKey aggiunge local_offices.region_key se manca, la
// riempie per ogni sede e la toglie dai campi che gli admin della sede
// possono scrivere.
func localOfficesRegionKey(app core.App) error {
	c, err := app.FindCollectionByNameOrId("local_offices")
	if err != nil {
		return err
	}
	changed := false
	if c.Fields.GetByName("region_key") == nil {
		c.Fields.Add(&core.TextField{Name: "region_key"})
		changed = true
	}
	const guard = "@request.body.region_key:isset = false"
	if c.UpdateRule != nil && *c.UpdateRule != "" && !strings.Contains(*c.UpdateRule, guard) {
		rule := *c.UpdateRule + " && " + guard
		c.UpdateRule = &rule
		changed = true
	}
	if changed {
		if err := app.Save(c); err != nil {
			return fmt.Errorf("local_offices.region_key: %w", err)
		}
	}

	var offices []struct {
		ID     string `db:"id"`
		Region string `db:"region"`
	}
	if err := app.DB().NewQuery(`SELECT id, COALESCE(region, '') AS region FROM local_offices`).All(&offices); err != nil {
		return fmt.Errorf("local_offices: %w", err)
	}
	// Un UPDATE per sede, senza passare dai record: nessun hook, nessun
	// reindex della ricerca, sono una ventina di righe.
	for _, o := range offices {
		if _, err := app.DB().NewQuery(`UPDATE local_offices SET region_key = {:k} WHERE id = {:id}`).
			Bind(dbx.Params{"k": regionkey.Of(o.Region), "id": o.ID}).Execute(); err != nil {
			return fmt.Errorf("local_offices %s: region_key: %w", o.ID, err)
		}
	}
	return nil
}

// closeLocalOfficesAdmins mette creazione e cancellazione di
// local_offices_admins a solo superuser. Lettura invariata.
func closeLocalOfficesAdmins(app core.App) error {
	c, err := app.FindCollectionByNameOrId("local_offices_admins")
	if err != nil {
		return err
	}
	if c.CreateRule == nil && c.DeleteRule == nil {
		return nil
	}
	c.CreateRule = nil
	c.DeleteRule = nil
	if err := app.Save(c); err != nil {
		return fmt.Errorf("local_offices_admins: regole: %w", err)
	}
	return nil
}
