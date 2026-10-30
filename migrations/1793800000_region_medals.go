package migrations

import (
	"errors"
	"fmt"

	"mensadb/tools/regionmedals"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Medaglie di regione (tools/regionmedals): dieci traguardi annuali per
// sede, tre metalli ciascuno. Una riga per sede, anno e medaglia, tenuta
// dal cron dopo ogni ricalcolo delle classifiche: il metallo massimo
// raggiunto nell'anno non scende mai, e per ogni metallo si ricorda quando
// e chi c'era (witnesses, gli id dei soci attivi della regione).
//
// Lettura per ogni utente autenticato, scrittura solo superuser (il
// server): i dati sono della regione, non del socio, e l'app li mostra
// nel medagliere. Idempotente: la collection si crea solo se manca. Si
// tenta un primo riempimento senza far fallire la migrazione, come per le
// classifiche.
func init() {
	m.Register(upRegionMedals, func(app core.App) error {
		return errors.New("rollback delle medaglie di regione non supportato: lo storico degli sblocchi andrebbe perso")
	})
}

func upRegionMedals(app core.App) error {
	if existing, _ := app.FindCollectionByNameOrId(regionmedals.Table); existing == nil {
		localOffices, err := app.FindCollectionByNameOrId("local_offices")
		if err != nil {
			return err
		}

		medals := make([]string, 0, len(regionmedals.Medals))
		for _, d := range regionmedals.Medals {
			medals = append(medals, d.Key)
		}
		tiers := append([]string{regionmedals.TierNone}, regionmedals.Tiers...)

		c := core.NewBaseCollection(regionmedals.Table)
		readRule := `@request.auth.id != ""`
		c.ListRule = &readRule
		c.ViewRule = &readRule
		c.Fields.Add(
			&core.RelationField{Name: "local_office", CollectionId: localOffices.Id, MaxSelect: 1, Required: true, CascadeDelete: true},
			// Il nome della sede e la regione normalizzata come in
			// view_leaderboard_regioni.region_key.
			&core.TextField{Name: "region"},
			&core.TextField{Name: "region_key"},
			&core.NumberField{Name: "year", OnlyInt: true, Required: true},
			&core.SelectField{Name: "medal", Values: medals, MaxSelect: 1, Required: true},
			// Il numero che si confronta con le soglie; count ed extra sono
			// le cifre di contorno (vedi tools/regionmedals/medals.go).
			&core.NumberField{Name: "value"},
			&core.NumberField{Name: "count"},
			&core.NumberField{Name: "extra"},
			// Le soglie {"bronze":..,"silver":..,"gold":..}, il minimo di
			// persone e se adesso e' soddisfatto.
			&core.JSONField{Name: "levels", MaxSize: 1 << 20},
			&core.NumberField{Name: "minimum"},
			&core.BoolField{Name: "eligible"},
			// Il metallo che i dati danno adesso e il massimo dell'anno.
			&core.SelectField{Name: "current_tier", Values: tiers, MaxSelect: 1},
			&core.SelectField{Name: "tier", Values: tiers, MaxSelect: 1},
			// Quando e con chi: {"bronze": "<data>"} e {"bronze": ["<id socio>", ...]}.
			&core.JSONField{Name: "unlocked", MaxSize: 1 << 20},
			&core.JSONField{Name: "witnesses", MaxSize: 8 << 20},
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)
		c.AddIndex("idx_leaderboard_region_medals_key", true, "local_office, year, medal", "")
		c.AddIndex("idx_leaderboard_region_medals_year", false, "year", "")
		if err := app.Save(c); err != nil {
			return fmt.Errorf("creazione %s: %w", regionmedals.Table, err)
		}
	}

	// Primo riempimento: se non riesce (per esempio senza le classifiche
	// riempite), la collection resta vuota fino al primo cron.
	if err := regionmedals.Refresh(app); err != nil {
		app.Logger().Warn("[regionmedals] primo riempimento non riuscito, ci pensa il cron", "err", err)
	}
	return nil
}
