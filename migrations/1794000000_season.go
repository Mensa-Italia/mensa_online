package migrations

import (
	"errors"
	"fmt"
	"time"

	"mensadb/tools/leaderboard"
	"mensadb/tools/medals"
	"mensadb/tools/regionmedals"
	"mensadb/tools/rollover"
	"mensadb/tools/season"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// La stagione, dal 1 ottobre al 30 settembre (tools/season): classifiche e
// medaglie diventano stagionali. Tutte le modifiche di schema in un file.
//
//   - view_leaderboard, view_leaderboard_regioni e view_leaderboard_segretari
//     e view_medals: campo season, che il ricalcolo riempie a ogni giro;
//   - leaderboard_seasons: l'albo d'oro, l'ordine finale di ogni stagione
//     chiusa per soci, regioni e segretari (tools/leaderboard.Finalize);
//   - medals_history: il metallo finale di ogni socio per ogni medaglia
//     stagionale e stagione chiusa (tools/medals.FinalizeMedals);
//   - medals_notify_state: campo season, le righe di adesso passano alla
//     stagione in corso (le permanenti a 0, non hanno annate), l'indice unico
//     diventa (socio, medaglia, stagione);
//   - leaderboard_region_medals: year diventa season (campo aggiunto, copiato
//     e tolto, con gli indici).
//
// In fondo, riempimenti a miglior sforzo, ognuno con il suo log: lo stato
// delle medaglie stagionali riportato ai contatori della stagione (in
// silenzio, nessuna notifica), le medaglie di regione 2025 e della stagione in
// corso ricalcolate da zero (cancellate e rifatte: testimoni i soci attivi di
// adesso, sblocchi adesso; una riga nuova non notifica mai), le tabelle della
// stagione in corso, e l'archivio retroattivo della 2025 (albo d'oro e annata
// delle medaglie) dai dati che ci sono. Se tutto riesce si salva la stagione
// in corso (tools/rollover), altrimenti ci pensa il primo giro del cron.
//
// Idempotente: i campi si aggiungono solo se mancano, le collection si creano
// solo se mancano, il ricalcolo si puo' ripetere.

// firstArchivedSeason e' la prima stagione che si archivia: la 2025/26,
// ricostruita a posteriori dai dati.
const firstArchivedSeason = 2025

func init() {
	m.Register(upSeason, func(app core.App) error {
		return errors.New("rollback della stagione non supportato: lo storico delle stagioni andrebbe perso")
	})
}

func upSeason(app core.App) error {
	now := time.Now()
	current := season.Current(now)

	for _, name := range []string{leaderboard.MembersTable, leaderboard.RegionsTable, leaderboard.SecretariesTable, medals.Table} {
		if err := addSeasonField(app, name); err != nil {
			return err
		}
	}
	if err := leaderboard.EnsureSeasonsCollection(app); err != nil {
		return err
	}
	if err := medals.EnsureHistoryCollection(app); err != nil {
		return err
	}
	if err := seasonNotifyState(app, current); err != nil {
		return err
	}
	if err := seasonRegionMedals(app, current); err != nil {
		return err
	}

	seasonFill(app, now, current)
	return nil
}

// addSeasonField aggiunge il campo season (intero) a una collection, se manca.
func addSeasonField(app core.App, name string) error {
	c, err := app.FindCollectionByNameOrId(name)
	if err != nil {
		return err
	}
	if c.Fields.GetByName("season") != nil {
		return nil
	}
	c.Fields.Add(&core.NumberField{Name: "season", OnlyInt: true})
	if err := app.Save(c); err != nil {
		return fmt.Errorf("%s.season: %w", name, err)
	}
	return nil
}

// seasonNotifyState porta lo stato delle notifiche delle medaglie alla
// stagione: campo season, righe esistenti alla stagione in corso (0 per le
// permanenti), indice unico su (user, medal, season).
func seasonNotifyState(app core.App, current int) error {
	c, err := app.FindCollectionByNameOrId(medals.StateTable)
	if err != nil {
		return err
	}
	if c.Fields.GetByName("season") == nil {
		c.Fields.Add(&core.NumberField{Name: "season", OnlyInt: true})
		if err := app.Save(c); err != nil {
			return fmt.Errorf("%s.season: %w", medals.StateTable, err)
		}
		if _, err := app.DB().NewQuery(`UPDATE ` + medals.StateTable + ` SET season = {:s}`).
			Bind(map[string]any{"s": current}).Execute(); err != nil {
			return fmt.Errorf("%s: righe alla stagione %d: %w", medals.StateTable, current, err)
		}
		for _, rule := range medals.Rules {
			if rule.Family != medals.Permanente {
				continue
			}
			if _, err := app.DB().NewQuery(`UPDATE ` + medals.StateTable + ` SET season = 0 WHERE medal = {:m}`).
				Bind(map[string]any{"m": rule.ID}).Execute(); err != nil {
				return fmt.Errorf("%s: permanente %s: %w", medals.StateTable, rule.ID, err)
			}
		}
		// ricarico: il salvataggio ha cambiato la collection
		if c, err = app.FindCollectionByNameOrId(medals.StateTable); err != nil {
			return err
		}
	}
	// AddIndex sostituisce l'indice che ha lo stesso nome, quello su (user, medal).
	c.AddIndex(medals.StateIndex, true, "user, medal, season", "")
	if err := app.Save(c); err != nil {
		return fmt.Errorf("%s: indice: %w", medals.StateTable, err)
	}
	return nil
}

// seasonRegionMedals aggiunge season alle medaglie di regione: il campo
// nuovo si aggiunge, si copia il valore da year, gli indici passano su
// season; year resta come alias (lo legge l'app 22.4.0). Le righe della 2025 e della
// stagione in corso si cancellano: l'anno di calendario non e' la stagione,
// le rifa' il riempimento in fondo.
func seasonRegionMedals(app core.App, current int) error {
	c, err := app.FindCollectionByNameOrId(regionmedals.Table)
	if err != nil {
		return err
	}
	if c.Fields.GetByName("season") == nil {
		c.Fields.Add(&core.NumberField{Name: "season", OnlyInt: true, Required: true})
		c.RemoveIndex("idx_leaderboard_region_medals_key")
		c.RemoveIndex("idx_leaderboard_region_medals_year")
		if err := app.Save(c); err != nil {
			return fmt.Errorf("%s.season: %w", regionmedals.Table, err)
		}
		if c.Fields.GetByName("year") != nil {
			if _, err := app.DB().NewQuery(`UPDATE ` + regionmedals.Table + ` SET season = year`).Execute(); err != nil {
				return fmt.Errorf("%s: copia di year: %w", regionmedals.Table, err)
			}
		}
		if c, err = app.FindCollectionByNameOrId(regionmedals.Table); err != nil {
			return err
		}
	}
	// year resta, con lo stesso valore di season: l'app 22.4.0 in produzione
	// lo legge ancora. Si toglie quando quella versione non e' piu' in giro.
	c.AddIndex("idx_leaderboard_region_medals_key", true, "local_office, season, medal", "")
	c.AddIndex("idx_leaderboard_region_medals_season", false, "season", "")
	if err := app.Save(c); err != nil {
		return fmt.Errorf("%s: year -> season: %w", regionmedals.Table, err)
	}
	return nil
}

// seasonFill e' il riempimento a miglior sforzo: ogni passo fallito va nel
// log e non ferma la migrazione.
func seasonFill(app core.App, now time.Time, current int) {
	ok := true
	step := func(name string, err error) {
		if err != nil {
			ok = false
			app.Logger().Warn("[season] "+name+" non riuscito, ci pensa il cron", "err", err)
			return
		}
		app.Logger().Info("[season] " + name)
	}

	// Lo stato delle medaglie stagionali segue i contatori della stagione.
	step("medagliere della stagione", medals.ReseedAt(app, now))

	// Le medaglie di regione della 2025 e della stagione in corso si rifanno
	// da zero: prima si cancellano le righe, poi si archivia la 2025 (l'albo
	// d'oro serve al Podio) e si ricalcolano.
	_, err := app.DB().NewQuery(`DELETE FROM ` + regionmedals.Table + ` WHERE season IN ({:a}, {:b})`).
		Bind(map[string]any{"a": firstArchivedSeason, "b": current}).Execute()
	step("medaglie di regione cancellate", err)

	step("albo d'oro "+season.Label(firstArchivedSeason), leaderboard.Finalize(app, firstArchivedSeason))
	step("annata delle medaglie "+season.Label(firstArchivedSeason), medals.FinalizeMedals(app, firstArchivedSeason))

	// Una stagione sola se la 2025 e' anche quella in corso.
	for _, s := range uniqueSeasons(firstArchivedSeason, current) {
		_, err := regionmedals.RefreshSeasonAt(app, s, now)
		step("medaglie di regione "+season.Label(s), err)
	}

	step("classifiche della stagione "+season.Label(current), leaderboard.RecomputeAt(app, now))

	if ok {
		step("stagione salvata", rollover.Store(app, current))
	}
}

func uniqueSeasons(a, b int) []int {
	if a == b {
		return []int{a}
	}
	return []int{a, b}
}
