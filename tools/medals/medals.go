// Package medals tiene il medagliere dei soci come tabella vera invece che
// come vista, e avvisa chi prende una medaglia nuova o la vede salire di
// livello.
//
// view_medals era una vista SQL: una riga per socio con una trentina di
// contatori (timbri, mesi, regioni, eventi, dispositivi...), ricalcolata a
// ogni lettura e dentro la query della classifica soci. Ora la stessa query
// (sql/view_medals.sql, identica a quella che era nella vista) gira dal cron
// ogni quarto d'ora, prima delle classifiche che la leggono, e il risultato
// finisce nella collection base view_medals, con lo stesso nome, gli stessi
// campi, gli stessi id (id del socio) e le stesse regole API di prima:
// l'app continua a leggere lo stesso endpoint.
//
// Le medaglie vere le calcola l'app dai contatori (MedalRules.kt); qui
// c'e' una copia delle regole (rules.go) che serve solo a capire quando un
// metallo cambia. Dopo ogni ricalcolo si confronta il metallo di ogni
// medaglia di ogni socio con l'ultimo visto, tenuto in medals_notify_state
// (una riga per socio e medaglia, solo superuser):
//
//   - una riga che non c'e' ancora viene scritta in silenzio. Vale per il
//     primo giro dopo il rilascio, per un socio nuovo e per una medaglia
//     aggiunta in seguito alle regole: niente raffica di notifiche per
//     cio' che c'era gia';
//   - un metallo piu' alto di quello salvato e' un avanzamento (Change) e
//     lo si salva;
//   - uno uguale o piu' basso (un contatore che scende, l'iscrizione che
//     scade) si salva e basta, cosi' la prossima salita avvisa di nuovo.
//
// Gli avanzamenti vanno a AfterRefresh, che li gira a
// tools/leaderboardnotify; l'interruttore e il filtro dei destinatari
// stanno li'. Lo stato si aggiorna comunque, a interruttore spento.
//
// Stagione. Le medaglie sono di due famiglie (Rule.Family). Le stagionali
// contano solo la stagione in corso (dal 1 ottobre, tools/season): i
// contatori di view_medals sono limitati alla finestra, lo stato delle
// notifiche ha una riga per socio, medaglia e stagione, e a ottobre le righe
// della stagione nuova non esistono ancora, quindi si seminano in silenzio
// (nessuna raffica). Le permanenti si tengono a vita, con stagione 0. Alla
// chiusura di una stagione FinalizeMedals ne scrive il metallo finale di
// ogni socio in medals_history, le annate che l'app mostra.
package medals

import (
	"database/sql"
	_ "embed"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"

	"mensadb/tools/season"
)

const (
	// Table e' la collection dei contatori, un tempo la vista.
	Table = "view_medals"
	// StateTable tiene l'ultimo metallo visto per socio, medaglia e stagione.
	StateTable = "medals_notify_state"
	// HistoryTable tiene il metallo finale di ogni stagione chiusa.
	HistoryTable = "medals_history"
)

//go:embed sql/view_medals.sql
var medalsSQL string

// Change e' un avanzamento: il socio ha preso la medaglia (From = TierNone)
// o l'ha vista salire di livello.
type Change struct {
	UserID string
	Medal  string
	Title  string
	From   Tier
	To     Tier
}

// AfterRefresh, se impostato, riceve gli avanzamenti di ogni ricalcolo
// riuscito, a transazione chiusa e fuori dal mutex. Lo imposta main/crons
// all'avvio (notifiche di tools/leaderboardnotify).
var AfterRefresh func(app core.App, changes []Change)

// Un ricalcolo alla volta: il cron e il primo riempimento della migrazione
// potrebbero sovrapporsi.
var refreshMu sync.Mutex

// Colonne che restano testo, e quelle che sono date o booleani: tutte le
// altre sono numeri.
var (
	dateColumns = map[string]bool{"app_since": true, "expire_membership": true, "first_stamp_at": true}
	boolColumns = map[string]bool{"is_membership_active": true, "renewed_ahead": true, "calendar_linked": true}
)

// StateIndex e' il nome dell'indice unico di medals_notify_state: socio,
// medaglia e stagione.
const StateIndex = "idx_medals_notify_state_key"

// EnsureStateCollection crea medals_notify_state se manca. Nessuna regola
// API: solo superuser.
func EnsureStateCollection(app core.App) error {
	if existing, _ := app.FindCollectionByNameOrId(StateTable); existing != nil {
		return nil
	}
	c := core.NewBaseCollection(StateTable)
	c.Fields.Add(
		&core.TextField{Name: "user", Required: true},
		&core.TextField{Name: "medal", Required: true},
		// Stagione della riga; 0 per le medaglie permanenti.
		&core.NumberField{Name: "season", OnlyInt: true},
		// Ultimo metallo visto, anche se piu' basso del precedente.
		&core.SelectField{Name: "tier", Values: tierNames(), MaxSelect: 1, Required: true},
		// Quando e' stato visto l'ultimo avanzamento (vuoto per le righe
		// scritte in silenzio): vale anche a interruttore spento.
		&core.DateField{Name: "sent_at"},
		&core.AutodateField{Name: "created", OnCreate: true},
		&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
	)
	c.AddIndex(StateIndex, true, "user, medal, season", "")
	if err := app.Save(c); err != nil {
		return fmt.Errorf("creazione %s: %w", StateTable, err)
	}
	return nil
}

func tierNames() []string {
	tiers := make([]string, 0, int(TierPlatinum)+1)
	for t := TierNone; t <= TierPlatinum; t++ {
		tiers = append(tiers, t.String())
	}
	return tiers
}

// EnsureHistoryCollection crea medals_history se manca: una riga per socio,
// stagione e medaglia stagionale con il metallo finale e il valore del
// contatore. Ogni socio legge solo le sue.
func EnsureHistoryCollection(app core.App) error {
	if existing, _ := app.FindCollectionByNameOrId(HistoryTable); existing != nil {
		return nil
	}
	c := core.NewBaseCollection(HistoryTable)
	rule := `user = @request.auth.id`
	c.ListRule = &rule
	c.ViewRule = &rule
	c.Fields.Add(
		&core.TextField{Name: "user", Required: true},
		&core.NumberField{Name: "season", OnlyInt: true, Required: true},
		&core.TextField{Name: "medal", Required: true},
		&core.SelectField{Name: "tier", Values: tierNames(), MaxSelect: 1, Required: true},
		&core.NumberField{Name: "value"},
		&core.AutodateField{Name: "created", OnCreate: true},
	)
	c.AddIndex("idx_medals_history_key", true, "user, season, medal", "")
	c.AddIndex("idx_medals_history_season", false, "season", "")
	if err := app.Save(c); err != nil {
		return fmt.Errorf("creazione %s: %w", HistoryTable, err)
	}
	return nil
}

// Refresh riesegue la query dei contatori della stagione in corso e riscrive
// la tabella in una sola transazione: le righe esistenti si aggiornano per
// id (solo se qualcosa e' cambiato), quelle di soci non piu' presenti si
// cancellano. Nella stessa transazione si valutano i metalli e si aggiorna
// lo stato; gli avanzamenti vanno poi a AfterRefresh.
func Refresh(app core.App) error {
	return RefreshAt(app, time.Now())
}

// RefreshAt e' Refresh con l'orologio passato da fuori: la stagione e'
// quella di now.
func RefreshAt(app core.App, now time.Time) error {
	changes, err := refresh(app, now, false)
	if err != nil {
		return err
	}
	if AfterRefresh != nil && len(changes) > 0 {
		AfterRefresh(app, changes)
	}
	return nil
}

// ReseedAt ricalcola come RefreshAt ma riporta lo stato delle medaglie
// stagionali della stagione in corso al metallo che i contatori danno
// adesso, anche se piu' basso, e non avvisa nessuno. Serve una volta, alla
// migrazione che introduce la stagione: lo stato di prima teneva i metalli
// a vita, che per una stagione sola sono troppo alti.
func ReseedAt(app core.App, now time.Time) error {
	_, err := refresh(app, now, true)
	return err
}

func refresh(app core.App, now time.Time, reseed bool) ([]Change, error) {
	refreshMu.Lock()
	defer refreshMu.Unlock()
	started := time.Now()
	current := season.Current(now)
	var (
		changes  []Change
		queryDur time.Duration
		users    int
	)
	err := app.RunInTransaction(func(tx core.App) error {
		collection, err := tx.FindCollectionByNameOrId(Table)
		if err != nil {
			return err
		}

		var rows []dbx.NullStringMap
		queryStart := time.Now()
		if err := tx.DB().NewQuery(medalsSQL).Bind(season.Params(current)).All(&rows); err != nil {
			return fmt.Errorf("query: %w", err)
		}
		queryDur = time.Since(queryStart)

		existing, err := tx.FindAllRecords(collection)
		if err != nil {
			return err
		}
		records := make(map[string]*core.Record, len(existing))
		for _, r := range existing {
			records[r.Id] = r
		}

		state, err := loadState(tx)
		if err != nil {
			return err
		}

		seen := make(map[string]bool, len(rows))
		for _, row := range rows {
			id := row["id"].String
			if id == "" {
				continue
			}
			seen[id] = true
			record, err := upsert(tx, collection, records[id], id, current, row)
			if err != nil {
				return err
			}
			userChanges, err := track(tx, state, id, statsOf(record), now, current, reseed)
			if err != nil {
				return fmt.Errorf("stato %s: %w", id, err)
			}
			changes = append(changes, userChanges...)
		}
		users = len(seen)

		for id, record := range records {
			if !seen[id] {
				if err := tx.Delete(record); err != nil {
					return fmt.Errorf("delete %s: %w", id, err)
				}
			}
		}
		for user := range state.byUser {
			if !seen[user] {
				if _, err := tx.DB().NewQuery(`DELETE FROM ` + StateTable + ` WHERE user = {:u}`).
					Bind(dbx.Params{"u": user}).Execute(); err != nil {
					return fmt.Errorf("stato %s: %w", user, err)
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if reseed {
		changes = nil
	}
	app.Logger().Info("[medals] medagliere ricalcolato", "season", season.Label(current),
		"users", users, "changes", len(changes), "query", queryDur.String(), "in", time.Since(started).String())
	return changes, nil
}

// upsert scrive una riga: nuova se manca, altrimenti salvata solo se un
// campo e' cambiato (di norma quasi nessuno, a ogni giro).
func upsert(tx core.App, collection *core.Collection, record *core.Record, id string, current int, row dbx.NullStringMap) (*core.Record, error) {
	dirty := false
	if record == nil {
		record = core.NewRecord(collection)
		record.Id = id
		dirty = true
	}
	// La stagione della riga: a ottobre cambia per tutti, con i contatori.
	if record.GetInt("season") != current {
		record.Set("season", current)
		dirty = true
	}
	for column, value := range row {
		if column == "id" || collection.Fields.GetByName(column) == nil {
			continue
		}
		before := record.GetString(column)
		record.Set(column, cellValue(column, value))
		if record.GetString(column) != before {
			dirty = true
		}
	}
	if dirty {
		if err := tx.SaveNoValidate(record); err != nil {
			return nil, fmt.Errorf("save %s: %w", id, err)
		}
	}
	return record, nil
}

func cellValue(column string, value sql.NullString) any {
	switch {
	case dateColumns[column]:
		return strings.TrimSpace(value.String)
	case boolColumns[column]:
		s := strings.TrimSpace(value.String)
		return s == "1" || strings.EqualFold(s, "true")
	}
	if !value.Valid {
		return 0
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(value.String), 64)
	if err != nil {
		return 0
	}
	return n
}

// statsOf legge i contatori di una riga salvata.
func statsOf(r *core.Record) Stats {
	return Stats{
		AppSince:             r.GetDateTime("app_since").Time(),
		IsMembershipActive:   r.GetBool("is_membership_active"),
		StampsTotal:          r.GetInt("stamps_total"),
		StampsDistinctMonths: r.GetInt("stamps_distinct_months"),
		StampsNight:          r.GetInt("stamps_night"),
		StampsMorning:        r.GetInt("stamps_morning"),
		StampsHalloween:      r.GetInt("stamps_halloween"),
		StampsNewYear:        r.GetInt("stamps_new_year"),
		StampsValentine:      r.GetInt("stamps_valentine"),
		StampsPiDay:          r.GetInt("stamps_pi_day"),
		StampsBirthday:       r.GetInt("stamps_birthday"),
		StampsFirstDay:       r.GetInt("stamps_first_day"),
		StampsLate:           r.GetInt("stamps_late"),
		StampsMaxSameDay:     r.GetInt("stamps_max_same_day"),
		StampsMaxMonthStreak: r.GetInt("stamps_max_month_streak"),
		StampsMaxSeasons:     r.GetInt("stamps_max_seasons_in_year"),
		StampsPioneer:        r.GetInt("stamps_pioneer"),
		StampsDistinctRegion: r.GetInt("stamps_distinct_regions"),
		StampsNational:       r.GetInt("stamps_national"),
		StampsOutOfRegion:    r.GetInt("stamps_out_of_region"),
		EventsCreated:        r.GetInt("events_created"),
		EventsCreatedNat:     r.GetInt("events_created_national"),
		EventRegistrations:   r.GetInt("event_registrations"),
		Devices:              r.GetInt("devices"),
		CalendarLinked:       r.GetBool("calendar_linked"),
		CalendarRegions:      r.GetInt("calendar_regions"),
	}
}

type stateRow struct {
	ID   string `db:"id"`
	User string `db:"user"`
	// Medal e' l'id della regola.
	Medal string `db:"medal"`
	// Season e' la stagione della riga, 0 per le medaglie permanenti.
	Season int    `db:"season"`
	Tier   string `db:"tier"`
}

type stateKey struct {
	medal  string
	season int
}

type stateIndex struct {
	byUser map[string]map[stateKey]stateRow
}

func loadState(tx core.App) (*stateIndex, error) {
	var rows []stateRow
	if err := tx.DB().NewQuery(`SELECT id, user, medal, CAST(COALESCE(season, 0) AS INTEGER) AS season, tier FROM ` + StateTable).All(&rows); err != nil {
		return nil, fmt.Errorf("%s: %w", StateTable, err)
	}
	idx := &stateIndex{byUser: map[string]map[stateKey]stateRow{}}
	for _, r := range rows {
		if idx.byUser[r.User] == nil {
			idx.byUser[r.User] = map[stateKey]stateRow{}
		}
		idx.byUser[r.User][stateKey{r.Medal, r.Season}] = r
	}
	return idx, nil
}

// track confronta il metallo di ogni medaglia con il massimo visto per il
// socio, aggiorna lo stato e ritorna gli avanzamenti. Il confronto e' dentro
// la stagione: le stagionali si confrontano con la riga della stagione in
// corso, le permanenti con quella a stagione 0. Le righe senza stato si
// scrivono in silenzio, e a ottobre la stagione nuova non ha ancora righe: si
// semina tutto senza avvisare nessuno. Lo stato tiene il metallo piu' alto
// raggiunto nella stagione e non scende mai: i contatori possono calare
// (tessera scaduta e rinnovata, timbri che smettono di valere quando cambia
// la quota della regione) e risalire, e una medaglia gia' annunciata non si
// annuncia due volte. Con reseed lo stato delle stagionali segue invece i
// contatori anche verso il basso, senza avvisi.
func track(tx core.App, state *stateIndex, user string, stats Stats, now time.Time, current int, reseed bool) ([]Change, error) {
	var changes []Change
	known := state.byUser[user]
	stamp := types.NowDateTime().String()
	for _, rule := range Rules {
		tier := rule.Tier(stats, now)
		key := stateKey{rule.ID, rule.StateSeason(current)}
		old, ok := known[key]
		switch {
		case !ok:
			_, err := tx.DB().Insert(StateTable, dbx.Params{
				"id": core.GenerateDefaultRandomId(), "user": user, "medal": rule.ID, "season": key.season, "tier": tier.String(),
				"sent_at": "", "created": stamp, "updated": stamp,
			}).Execute()
			if err != nil {
				return nil, err
			}
		case reseed && rule.Family == Stagionale:
			if tier == ParseTier(old.Tier) {
				continue
			}
			if _, err := tx.DB().Update(StateTable, dbx.Params{"tier": tier.String(), "updated": stamp},
				dbx.HashExp{"id": old.ID}).Execute(); err != nil {
				return nil, err
			}
		case tier <= ParseTier(old.Tier):
		default:
			params := dbx.Params{"tier": tier.String(), "updated": stamp, "sent_at": stamp}
			changes = append(changes, Change{UserID: user, Medal: rule.ID, Title: rule.Title, From: ParseTier(old.Tier), To: tier})
			if _, err := tx.DB().Update(StateTable, params, dbx.HashExp{"id": old.ID}).Execute(); err != nil {
				return nil, err
			}
		}
	}
	return changes, nil
}

// FinalizeMedals scrive in medals_history il metallo finale della stagione s
// per ogni socio con almeno un timbro valido in quella stagione: una riga
// per medaglia stagionale, anche con metallo "none" (l'app puo' mostrare
// "non ottenuta"), con il valore del contatore. Rifa' il calcolo da zero con
// la finestra della stagione, non legge view_medals (che e' della stagione in
// corso), ed e' idempotente: le righe della stagione si sostituiscono. Si
// chiama al cambio di stagione per quella appena chiusa, e una volta dalla
// migrazione per la prima.
func FinalizeMedals(app core.App, s int) error {
	started := time.Now()
	refreshMu.Lock()
	defer refreshMu.Unlock()
	written, users := 0, 0
	err := app.RunInTransaction(func(tx core.App) error {
		collection, err := tx.FindCollectionByNameOrId(Table)
		if err != nil {
			return err
		}
		var rows []dbx.NullStringMap
		if err := tx.DB().NewQuery(medalsSQL).Bind(season.Params(s)).All(&rows); err != nil {
			return fmt.Errorf("query: %w", err)
		}
		if _, err := tx.DB().NewQuery(`DELETE FROM ` + HistoryTable + ` WHERE season = {:s}`).
			Bind(dbx.Params{"s": s}).Execute(); err != nil {
			return fmt.Errorf("pulizia %s: %w", HistoryTable, err)
		}
		now := time.Now()
		stamp := types.NowDateTime().String()
		for _, row := range rows {
			id := row["id"].String
			if id == "" {
				continue
			}
			record := core.NewRecord(collection)
			for column, value := range row {
				if column != "id" && collection.Fields.GetByName(column) != nil {
					record.Set(column, cellValue(column, value))
				}
			}
			stats := statsOf(record)
			if stats.StampsTotal <= 0 {
				continue
			}
			users++
			for _, rule := range Rules {
				if rule.Family != Stagionale {
					continue
				}
				_, err := tx.DB().Insert(HistoryTable, dbx.Params{
					"id": core.GenerateDefaultRandomId(), "user": id, "season": s, "medal": rule.ID,
					"tier": rule.Tier(stats, now).String(), "value": rule.Value(stats, now), "created": stamp,
				}).Execute()
				if err != nil {
					return fmt.Errorf("%s/%s: %w", id, rule.ID, err)
				}
				written++
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	app.Logger().Info("[medals] annata dei soci archiviata", "season", season.Label(s),
		"users", users, "rows", written, "in", time.Since(started).String())
	return nil
}

// ShadowSeason crea nella transazione una tabella TEMPORANEA chiamata
// view_medals con i contatori della stagione s, al posto di quella vera per
// la durata della transazione (SQLite cerca prima fra le tabelle
// temporanee). Serve a chi deve far girare, per una stagione chiusa, le
// query che leggono view_medals (la classifica dei soci). Va richiamata la
// funzione ritornata prima di chiudere la transazione.
func ShadowSeason(tx core.App, s int) (drop func(), err error) {
	drop = func() {
		_, _ = tx.DB().NewQuery(`DROP TABLE IF EXISTS temp.` + Table).Execute()
	}
	drop()
	if _, err := tx.DB().NewQuery("CREATE TEMP TABLE " + Table + " AS\n" + medalsSQL).Bind(season.Params(s)).Execute(); err != nil {
		drop()
		return func() {}, fmt.Errorf("contatori della stagione %d: %w", s, err)
	}
	return drop, nil
}
