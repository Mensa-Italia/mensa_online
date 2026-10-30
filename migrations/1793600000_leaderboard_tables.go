package migrations

import (
	"errors"
	"fmt"

	"mensadb/tools/leaderboard"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Le tre classifiche da viste a tabelle, piu' lo storico.
//
// view_leaderboard, view_leaderboard_regioni e view_leaderboard_segretari
// erano viste SQL ricalcolate a ogni richiesta (0,6-1,9 s l'una, vedi
// tools/leaderboard). Qui si cancellano e al loro posto nascono collection
// base con lo stesso nome, gli stessi campi, gli stessi id (numero tessera,
// id sede) e le stesse regole API, cosi' l'app continua a leggere gli stessi
// endpoint senza cambiare. Le riempie il cron di tools/leaderboard ogni
// quarto d'ora; qui si tenta un primo riempimento, senza far fallire la
// migrazione se la query non riesce (le tabelle restano vuote fino al
// primo cron).
//
// Due campi in piu' per riga, previous_position e previous_score, con la
// posizione e i punti dell'ultimo snapshot giornaliero (leaderboard_history,
// una riga per soggetto e giorno, solo superuser): l'app ci fa le frecce.
//
// La regola della classifica soci cambia rispetto alla vista: prima
// `position <= 10 || id = @request.auth.id` non chiedeva il login, e un GET
// anonimo leggeva nome e numero tessera dei primi dieci. Ora, come le altre
// due, `@request.auth.id != "" && (...)`.
//
// Idempotente: le viste si cancellano solo se sono ancora viste, le tabelle
// si creano solo se mancano.

func init() {
	m.Register(upLeaderboardTables, func(app core.App) error {
		return errors.New("rollback delle tabelle della classifica non supportato: le viste vanno ricreate dall'export")
	})
}

func upLeaderboardTables(app core.App) error {
	// Prima quelle che dipendono dalle altre: segretari e soci leggono regioni.
	for _, name := range []string{leaderboard.SecretariesTable, leaderboard.MembersTable, leaderboard.RegionsTable} {
		existing, err := app.FindCollectionByNameOrId(name)
		if err == nil && existing.IsView() {
			if err := app.Delete(existing); err != nil {
				return fmt.Errorf("cancellazione vista %s: %w", name, err)
			}
		}
	}

	localOffices, err := app.FindCollectionByNameOrId("local_offices")
	if err != nil {
		return err
	}

	authOnly := `@request.auth.id != ""`
	membersRule := authOnly + ` && (position <= 10 || id = @request.auth.id)`
	regionsRule := authOnly + ` && (score >= 3 || (@collection.local_offices_admins.user ?= @request.auth.id && @collection.local_offices_admins.local_office ?= id))`
	secretariesRule := authOnly + ` && (score >= 3 || (@collection.local_offices_admins.user ?= @request.auth.id && @collection.local_offices_admins.local_office ?= local_office))`

	regionNumbers := []string{
		"position", "score", "participation", "community_pct", "national_points", "national_events",
		"effective_attendees", "distinct_attendees", "core_pct", "reach_pct", "events_total", "events_scoring",
		"attendance_total", "members_active", "avg_share_pct",
	}

	if err := ensureBoard(app, leaderboard.RegionsTable, regionsRule, func(c *core.Collection) {
		c.Fields.Add(
			&core.TextField{Name: "name"},
			&core.TextField{Name: "region"},
			&core.TextField{Name: "region_key"},
		)
		addNumbers(c, regionNumbers)
	}); err != nil {
		return err
	}

	if err := ensureBoard(app, leaderboard.SecretariesTable, secretariesRule, func(c *core.Collection) {
		c.Fields.Add(
			&core.TextField{Name: "name"},
			&core.RelationField{Name: "local_office", CollectionId: localOffices.Id, MaxSelect: 1},
			&core.TextField{Name: "region"},
		)
		addNumbers(c, regionNumbers)
	}); err != nil {
		return err
	}

	if err := ensureBoard(app, leaderboard.MembersTable, membersRule, func(c *core.Collection) {
		c.Fields.Add(&core.TextField{Name: "name"})
		addNumbers(c, []string{
			"position", "score", "stamps_points", "medal_points", "events_points", "stamps_total", "stamps_distinct_regions",
		})
	}); err != nil {
		return err
	}

	if existing, _ := app.FindCollectionByNameOrId(leaderboard.HistoryTable); existing == nil {
		h := core.NewBaseCollection(leaderboard.HistoryTable)
		// Nessuna regola API: solo superuser. L'app legge previous_* dalle
		// tabelle vive, lo storico serve a quelle e a grafici futuri.
		h.Fields.Add(
			&core.SelectField{Name: "board", Values: []string{"members", "regions", "secretaries"}, MaxSelect: 1, Required: true},
			&core.TextField{Name: "subject", Required: true},
			// Giorno dello snapshot, "YYYY-MM-DD": la chiave dello storico.
			&core.TextField{Name: "day", Required: true, Min: 10, Max: 10},
			&core.TextField{Name: "name"},
			&core.NumberField{Name: "position"},
			&core.NumberField{Name: "score"},
			&core.AutodateField{Name: "created", OnCreate: true},
		)
		h.AddIndex("idx_leaderboard_history_key", true, "board, subject, day", "")
		h.AddIndex("idx_leaderboard_history_day", false, "board, day", "")
		if err := app.Save(h); err != nil {
			return fmt.Errorf("creazione %s: %w", leaderboard.HistoryTable, err)
		}
	}

	// Primo riempimento: se non riesce (per esempio senza dati, o per un
	// timeout), le tabelle restano vuote fino al primo cron.
	if err := leaderboard.Refresh(app); err != nil {
		app.Logger().Warn("[leaderboard] primo riempimento non riuscito, ci pensa il cron", "err", err)
	}
	return nil
}

// ensureBoard crea la tabella di una classifica se manca: id libero (numero
// tessera o id sede, non autogenerato), regole di sola lettura, campi
// aggiunti da fill, piu' previous_position e previous_score.
func ensureBoard(app core.App, name, readRule string, fill func(c *core.Collection)) error {
	if existing, _ := app.FindCollectionByNameOrId(name); existing != nil {
		return nil
	}
	c := core.NewBaseCollection(name)
	if id, ok := c.Fields.GetByName("id").(*core.TextField); ok {
		id.Min = 1
		id.Max = 30
		id.AutogeneratePattern = ""
	}
	c.ListRule = &readRule
	c.ViewRule = &readRule
	fill(c)
	addNumbers(c, []string{"previous_position", "previous_score"})
	if err := app.Save(c); err != nil {
		return fmt.Errorf("creazione %s: %w", name, err)
	}
	return nil
}

func addNumbers(c *core.Collection, names []string) {
	for _, n := range names {
		c.Fields.Add(&core.NumberField{Name: n})
	}
}
