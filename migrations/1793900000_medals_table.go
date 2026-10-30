package migrations

import (
	"errors"
	"fmt"

	"mensadb/tools/medals"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// view_medals da vista a tabella (tools/medals).
//
// La vista calcolava a ogni lettura i contatori delle medaglie di tutti i
// soci (33 kB di SQL, e la leggeva anche la classifica soci). Qui si
// cancella e al suo posto nasce una collection base con lo stesso nome, gli
// stessi campi (stessi tipi e nomi), gli stessi id (id del socio) e le
// stesse regole API, cosi' l'app continua a leggere lo stesso endpoint. La
// riempie il cron di tools/medals ogni quarto d'ora, prima delle
// classifiche; qui si tenta un primo riempimento, senza far fallire la
// migrazione se la query non riesce (la tabella resta vuota fino al primo
// cron). Quel primo riempimento scrive anche lo stato delle notifiche
// (medals_notify_state) con i metalli di adesso, in silenzio: il giorno del
// rilascio nessuno riceve niente per cio' che aveva gia'.
//
// Differenze di schema rispetto alla vista, invisibili all'API: app_since
// non e' piu' un autodate (che sulla creazione di una riga si
// riscriverebbe con l'ora di adesso) ma una data semplice, e first_stamp_at
// e' una data invece di un json.
//
// Idempotente: la vista si cancella solo se e' ancora una vista, le
// collection si creano solo se mancano.

// Le regole di lettura della vista, tali e quali: solo la riga del socio
// loggato, e non per i client non ufficiali.
const medalsReadRule = `(
  @request.headers.user_agent !~ "jackoverfull" &&
  @request.headers.user_agent !~ "unknown version" &&
  @request.headers.user_agent !~ "direct" &&
  id = @request.auth.id
)`

func init() {
	m.Register(upMedalsTable, func(app core.App) error {
		return errors.New("rollback della tabella delle medaglie non supportato: la vista va ricreata dall'export")
	})
}

func upMedalsTable(app core.App) error {
	if existing, err := app.FindCollectionByNameOrId(medals.Table); err == nil && existing.IsView() {
		if err := app.Delete(existing); err != nil {
			return fmt.Errorf("cancellazione vista %s: %w", medals.Table, err)
		}
	}

	if existing, _ := app.FindCollectionByNameOrId(medals.Table); existing == nil {
		c := core.NewBaseCollection(medals.Table)
		if id, ok := c.Fields.GetByName("id").(*core.TextField); ok {
			id.Min = 1
			id.Max = 30
			id.AutogeneratePattern = ""
		}
		rule := medalsReadRule
		c.ListRule = &rule
		c.ViewRule = &rule

		c.Fields.Add(
			&core.DateField{Name: "app_since"},
			&core.DateField{Name: "expire_membership"},
			&core.BoolField{Name: "is_membership_active"},
			&core.BoolField{Name: "renewed_ahead"},
			&core.NumberField{Name: "stamps_total", OnlyInt: true},
			&core.DateField{Name: "first_stamp_at"},
		)
		for _, name := range []string{
			"stamps_distinct_months", "stamps_night", "stamps_morning", "stamps_halloween", "stamps_new_year",
			"stamps_valentine", "stamps_pi_day", "stamps_birthday", "stamps_first_day", "stamps_late",
			"stamps_max_same_day", "stamps_max_month_streak", "stamps_max_seasons_in_year", "stamps_pioneer",
			"stamps_distinct_regions", "stamps_national", "stamps_out_of_region",
			"tickets_total", "events_created", "events_created_national", "event_registrations",
			"payments_succeeded", "donations", "boutique_orders", "devices",
		} {
			c.Fields.Add(&core.NumberField{Name: name, OnlyInt: true})
		}
		c.Fields.Add(
			&core.BoolField{Name: "calendar_linked"},
			&core.NumberField{Name: "calendar_regions", OnlyInt: true},
		)
		if err := app.Save(c); err != nil {
			return fmt.Errorf("creazione %s: %w", medals.Table, err)
		}
	}

	if err := medals.EnsureStateCollection(app); err != nil {
		return err
	}

	// Primo riempimento: se non riesce, la tabella resta vuota fino al
	// primo cron.
	if err := medals.Refresh(app); err != nil {
		app.Logger().Warn("[medals] primo riempimento non riuscito, ci pensa il cron", "err", err)
	}
	return nil
}
