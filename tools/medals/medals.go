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
)

const (
	// Table e' la collection dei contatori, un tempo la vista.
	Table = "view_medals"
	// StateTable tiene l'ultimo metallo visto per socio e medaglia.
	StateTable = "medals_notify_state"
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

// EnsureStateCollection crea medals_notify_state se manca. Nessuna regola
// API: solo superuser.
func EnsureStateCollection(app core.App) error {
	if existing, _ := app.FindCollectionByNameOrId(StateTable); existing != nil {
		return nil
	}
	tiers := make([]string, 0, int(TierPlatinum)+1)
	for t := TierNone; t <= TierPlatinum; t++ {
		tiers = append(tiers, t.String())
	}
	c := core.NewBaseCollection(StateTable)
	c.Fields.Add(
		&core.TextField{Name: "user", Required: true},
		&core.TextField{Name: "medal", Required: true},
		// Ultimo metallo visto, anche se piu' basso del precedente.
		&core.SelectField{Name: "tier", Values: tiers, MaxSelect: 1, Required: true},
		// Quando e' stato visto l'ultimo avanzamento (vuoto per le righe
		// scritte in silenzio): vale anche a interruttore spento.
		&core.DateField{Name: "sent_at"},
		&core.AutodateField{Name: "created", OnCreate: true},
		&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
	)
	c.AddIndex("idx_medals_notify_state_key", true, "user, medal", "")
	if err := app.Save(c); err != nil {
		return fmt.Errorf("creazione %s: %w", StateTable, err)
	}
	return nil
}

// Refresh riesegue la query dei contatori e riscrive la tabella in una sola
// transazione: le righe esistenti si aggiornano per id (solo se qualcosa e'
// cambiato), quelle di soci non piu' presenti si cancellano. Nella stessa
// transazione si valutano i metalli e si aggiorna lo stato; gli avanzamenti
// vanno poi a AfterRefresh.
func Refresh(app core.App) error {
	changes, err := refresh(app, time.Now())
	if err != nil {
		return err
	}
	if AfterRefresh != nil && len(changes) > 0 {
		AfterRefresh(app, changes)
	}
	return nil
}

func refresh(app core.App, now time.Time) ([]Change, error) {
	refreshMu.Lock()
	defer refreshMu.Unlock()
	started := time.Now()
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
		if err := tx.DB().NewQuery(medalsSQL).All(&rows); err != nil {
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
			record, err := upsert(tx, collection, records[id], id, row)
			if err != nil {
				return err
			}
			userChanges, err := track(tx, state, id, statsOf(record), now)
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
	app.Logger().Info("[medals] medagliere ricalcolato",
		"users", users, "changes", len(changes), "query", queryDur.String(), "in", time.Since(started).String())
	return changes, nil
}

// upsert scrive una riga: nuova se manca, altrimenti salvata solo se un
// campo e' cambiato (di norma quasi nessuno, a ogni giro).
func upsert(tx core.App, collection *core.Collection, record *core.Record, id string, row dbx.NullStringMap) (*core.Record, error) {
	dirty := false
	if record == nil {
		record = core.NewRecord(collection)
		record.Id = id
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
	Tier  string `db:"tier"`
}

type stateIndex struct {
	byUser map[string]map[string]stateRow
}

func loadState(tx core.App) (*stateIndex, error) {
	var rows []stateRow
	if err := tx.DB().NewQuery(`SELECT id, user, medal, tier FROM ` + StateTable).All(&rows); err != nil {
		return nil, fmt.Errorf("%s: %w", StateTable, err)
	}
	idx := &stateIndex{byUser: map[string]map[string]stateRow{}}
	for _, r := range rows {
		if idx.byUser[r.User] == nil {
			idx.byUser[r.User] = map[string]stateRow{}
		}
		idx.byUser[r.User][r.Medal] = r
	}
	return idx, nil
}

// track confronta il metallo di ogni medaglia con il massimo visto per il
// socio, aggiorna lo stato e ritorna gli avanzamenti. Le righe senza stato
// si scrivono in silenzio. Lo stato tiene il metallo piu' alto raggiunto e
// non scende mai: i contatori possono calare (tessera scaduta e rinnovata,
// timbri che smettono di valere quando cambia la quota della regione) e
// risalire, e una medaglia gia' annunciata non si annuncia due volte.
func track(tx core.App, state *stateIndex, user string, stats Stats, now time.Time) ([]Change, error) {
	var changes []Change
	known := state.byUser[user]
	stamp := types.NowDateTime().String()
	for _, rule := range Rules {
		tier := rule.Tier(stats, now)
		old, ok := known[rule.ID]
		switch {
		case !ok:
			_, err := tx.DB().Insert(StateTable, dbx.Params{
				"id": core.GenerateDefaultRandomId(), "user": user, "medal": rule.ID, "tier": tier.String(),
				"sent_at": "", "created": stamp, "updated": stamp,
			}).Execute()
			if err != nil {
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
