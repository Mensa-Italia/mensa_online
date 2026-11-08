// Package officestats materializza le statistiche dei gruppi locali, per la
// pagina "Gruppo locale" dell'app: una riga per sede in local_office_stats
// (leggibile da ogni socio loggato) e una riga per sede e mese, dall'inizio
// della stagione precedente a oggi, in local_office_stats_monthly (solo
// admin della sede e super): i grafici dell'area segreteria confrontano la
// stagione con quella prima.
//
// Si ricalcola nella catena del refresh delle classifiche, ogni quarto
// d'ora (main/crons, leaderboard.AfterRefresh), in una sola transazione:
// le righe si aggiornano per id, quelle che non servono piu' (una sede
// sparita, i mesi usciti dalla finestra) si cancellano.
//
// Le regole, con region_key la chiave della sede (tools/regionkey):
//
//   - soci attivi: members_registry attivi con l'area accorpata uguale alla
//     chiave;
//   - nuovi e usciti: i movimenti di members_registry_changes della sede
//     (tools/memberchanges), joined e moved_in contro left e moved_out;
//   - eventi della regione: events con local_office la sede oppure con
//     l'indirizzo (positions.state accorpato) nella regione, when_start
//     valorizzato;
//   - presenze: i timbratori come nelle classifiche, ogni timbro preso nella
//     stagione va all'edizione con quel nome piu' vicina nel tempo, senza
//     l'organizzatore di quell'edizione e senza 5366;
//   - convenzioni attive: deals is_local, is_active, non scadute, con
//     l'indirizzo nella regione.
//
// I soci attivi a fine mese si ricostruiscono all'indietro: i soci di
// adesso meno il saldo (nuovi - usciti) dei movimenti dei mesi dopo. Dove
// i movimenti non ci sono, la curva resta piatta.
package officestats

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"

	"mensadb/tools/memberchanges"
	"mensadb/tools/regionkey"
	"mensadb/tools/season"
)

const (
	// Table ha una riga per sede, id = id della sede.
	Table = "local_office_stats"
	// MonthlyTable ha una riga per sede e mese della stagione, id =
	// <id sede>_<AAAA-MM>.
	MonthlyTable = "local_office_stats_monthly"
)

// Un ricalcolo alla volta: la catena del cron e l'hook dell'opt-out
// possono sovrapporsi.
var refreshMu sync.Mutex

var statsNumbers = []string{
	"members_active", "members_new_7d", "members_new_30d", "members_left_30d",
	"events_season", "events_upcoming", "attendance_season", "deals_active",
}

var monthlyNumbers = []string{
	"events", "attendance", "distinct_attendees", "members_new", "members_left", "members_active",
}

// EnsureCollections crea le due tabelle se mancano. Idempotente.
func EnsureCollections(app core.App) error {
	offices, err := app.FindCollectionByNameOrId("local_offices")
	if err != nil {
		return err
	}
	authOnly := `@request.auth.id != ""`
	if err := ensureTable(app, Table, authOnly, func(c *core.Collection) {
		c.Fields.Add(
			&core.RelationField{Name: "local_office", CollectionId: offices.Id, MaxSelect: 1},
			&core.TextField{Name: "region_key"},
		)
		addNumbers(c, statsNumbers)
		c.Fields.Add(
			&core.TextField{Name: "season"},
			&core.DateField{Name: "computed_at"},
		)
	}); err != nil {
		return err
	}
	return ensureTable(app, MonthlyTable, memberchanges.AdminReadRule, func(c *core.Collection) {
		c.Fields.Add(
			&core.RelationField{Name: "local_office", CollectionId: offices.Id, MaxSelect: 1},
			&core.TextField{Name: "region_key"},
			&core.TextField{Name: "month"},
			&core.TextField{Name: "season"},
		)
		addNumbers(c, monthlyNumbers)
		c.AddIndex("idx_local_office_stats_monthly_office_month", false, "local_office, month", "")
	})
}

// ensureTable crea una tabella materializzata se manca: id libero (non
// autogenerato), sola lettura con readRule, scrittura solo superuser.
func ensureTable(app core.App, name, readRule string, fill func(c *core.Collection)) error {
	if existing, _ := app.FindCollectionByNameOrId(name); existing != nil {
		return nil
	}
	c := core.NewBaseCollection(name)
	if id, ok := c.Fields.GetByName("id").(*core.TextField); ok {
		id.Min = 1
		id.Max = 40
		id.Pattern = ""
		id.AutogeneratePattern = ""
	}
	c.ListRule = &readRule
	c.ViewRule = &readRule
	fill(c)
	if err := app.Save(c); err != nil {
		return fmt.Errorf("creazione %s: %w", name, err)
	}
	return nil
}

func addNumbers(c *core.Collection, names []string) {
	for _, n := range names {
		c.Fields.Add(&core.NumberField{Name: n, OnlyInt: true})
	}
}

// Refresh ricalcola le statistiche di adesso.
func Refresh(app core.App) error {
	return RefreshAt(app, time.Now())
}

// RefreshAt ricalcola le statistiche con l'orologio passato da fuori: la
// stagione e' quella di now.
func RefreshAt(app core.App, now time.Time) error {
	refreshMu.Lock()
	defer refreshMu.Unlock()
	started := time.Now()

	current := season.Current(now)
	in, err := load(app, now, current)
	if err != nil {
		return err
	}
	stats, monthly := compute(in, now, current)

	err = app.RunInTransaction(func(tx core.App) error {
		if err := write(tx, Table, stats); err != nil {
			return err
		}
		return write(tx, MonthlyTable, monthly)
	})
	if err != nil {
		return err
	}
	app.Logger().Info("[officestats] statistiche delle sedi ricalcolate", "season", season.Label(current),
		"offices", len(stats), "months", len(monthly), "in", time.Since(started).String())
	return nil
}

type office struct {
	id, key string
}

type event struct {
	id, office, key string
	when            time.Time
}

type change struct {
	office, kind string
	day          time.Time
}

// input e' tutto quello che serve al calcolo, letto in poche query.
type input struct {
	offices   []office
	active    map[string]int      // chiave di regione -> soci attivi
	deals     map[string]int      // chiave di regione -> convenzioni attive
	events    []event             // eventi dall'inizio della stagione precedente in poi
	attendees map[string][]string // id evento -> timbratori
	changes   []change            // movimenti dall'inizio della stagione precedente in poi
}

func load(app core.App, now time.Time, current int) (*input, error) {
	in := &input{active: map[string]int{}, deals: map[string]int{}, attendees: map[string][]string{}}
	// la finestra: dall'inizio della stagione precedente alla fine di questa
	windowStart, _ := season.Bounds(current - 1)
	_, windowEnd := season.Bounds(current)
	params := dbx.Params{"start": windowStart.Format(sqlLayout), "end": windowEnd.Format(sqlLayout)}
	nowValue, err := types.ParseDateTime(now)
	if err != nil {
		return nil, err
	}

	records, err := app.FindAllRecords("local_offices")
	if err != nil {
		return nil, fmt.Errorf("sedi: %w", err)
	}
	for _, r := range records {
		in.offices = append(in.offices, office{id: r.Id, key: memberchanges.OfficeKey(r)})
	}
	sort.Slice(in.offices, func(i, j int) bool { return in.offices[i].id < in.offices[j].id })

	var counts []struct {
		Key string `db:"region_key"`
		N   int    `db:"n"`
	}
	if err := app.DB().NewQuery(`
		SELECT ` + regionkey.RegistryAreaSQL("mr.area") + ` AS region_key, COUNT(*) AS n
		FROM members_registry mr
		WHERE mr.is_active AND mr.area NOT IN ('', 'NaN')
		GROUP BY 1
	`).All(&counts); err != nil {
		return nil, fmt.Errorf("soci attivi: %w", err)
	}
	for _, c := range counts {
		in.active[c.Key] = c.N
	}

	counts = nil
	if err := app.DB().NewQuery(`
		SELECT ` + regionkey.PositionStateSQL("p.state") + ` AS region_key, COUNT(*) AS n
		FROM deals d
		JOIN positions p ON p.id = d.position
		WHERE d.is_local AND d.is_active
		  AND (d.ending IS NULL OR d.ending = '' OR d.ending > {:now})
		  AND p.state IS NOT NULL AND p.state != ''
		GROUP BY 1
	`).Bind(dbx.Params{"now": nowValue.String()}).All(&counts); err != nil {
		return nil, fmt.Errorf("convenzioni: %w", err)
	}
	for _, c := range counts {
		in.deals[c.Key] = c.N
	}

	var events []struct {
		ID     string `db:"id"`
		Office string `db:"local_office"`
		Key    string `db:"region_key"`
		When   string `db:"when_start"`
	}
	if err := app.DB().NewQuery(`
		SELECT e.id AS id, COALESCE(e.local_office, '') AS local_office,
		  COALESCE(` + regionkey.PositionStateSQL("p.state") + `, '') AS region_key,
		  e.when_start AS when_start
		FROM events e
		LEFT JOIN positions p ON p.id = e.position
		WHERE e.when_start != '' AND e.when_start >= {:start}
	`).Bind(params).All(&events); err != nil {
		return nil, fmt.Errorf("eventi: %w", err)
	}
	for _, e := range events {
		when, err := types.ParseDateTime(e.When)
		if err != nil || when.IsZero() {
			continue
		}
		in.events = append(in.events, event{id: e.ID, office: e.Office, key: e.Key, when: when.Time()})
	}

	// Timbratori: la stessa sottoquery delle classifiche
	// (tools/leaderboard/sql/regioni.sql).
	var stampers []struct {
		Event string `db:"event_id"`
		User  string `db:"user"`
	}
	if err := app.DB().NewQuery(`
		SELECT event_id, user
		FROM (
		  SELECT e.id AS event_id, e.owner, su.user,
		    ROW_NUMBER() OVER (PARTITION BY su.id
		                       ORDER BY abs(julianday(su.created) - julianday(e.when_start))) AS nearest
		  FROM stamp_users su
		  JOIN stamp st ON st.id = su.stamp
		  JOIN events e ON e.name = st.description
		  WHERE su.user != '' AND su.user != '5366'
		    AND su.created >= {:start} AND su.created < {:end}
		)
		WHERE nearest = 1 AND owner != user
	`).Bind(params).All(&stampers); err != nil {
		return nil, fmt.Errorf("timbratori: %w", err)
	}
	for _, s := range stampers {
		in.attendees[s.Event] = append(in.attendees[s.Event], s.User)
	}

	// Movimenti dall'inizio della stagione o degli ultimi 30 giorni, quale
	// viene prima.
	since := memberchanges.Day(now).AddDate(0, 0, -31)
	if windowStart.Before(since) {
		since = windowStart
	}
	sinceValue, err := types.ParseDateTime(since)
	if err != nil {
		return nil, err
	}
	var changes []struct {
		Office string `db:"local_office"`
		Kind   string `db:"kind"`
		Day    string `db:"day"`
	}
	if err := app.DB().NewQuery(`
		SELECT local_office, kind, day
		FROM ` + memberchanges.Table + `
		WHERE local_office != '' AND day >= {:since}
	`).Bind(dbx.Params{"since": sinceValue.String()}).All(&changes); err != nil {
		return nil, fmt.Errorf("movimenti: %w", err)
	}
	for _, c := range changes {
		day, err := types.ParseDateTime(c.Day)
		if err != nil || day.IsZero() {
			continue
		}
		in.changes = append(in.changes, change{office: c.Office, kind: c.Kind, day: day.Time()})
	}
	return in, nil
}

// row e' una riga da scrivere: id e valori dei campi.
type row struct {
	id     string
	values map[string]any
}

func isNew(kind string) bool  { return kind == memberchanges.Joined || kind == memberchanges.MovedIn }
func isLeft(kind string) bool { return kind == memberchanges.Left || kind == memberchanges.MovedOut }

func monthOf(t time.Time) string { return season.Local(t).Format("2006-01") }

// windowMonths sono i mesi (ora di Roma, "AAAA-MM") dall'inizio della
// stagione precedente a current al mese di now compreso: al massimo 24.
func windowMonths(current int, now time.Time) []string {
	start, _ := season.Bounds(current - 1)
	first := season.Local(start)
	last := monthOf(now)
	var out []string
	for m := time.Date(first.Year(), first.Month(), 1, 12, 0, 0, 0, first.Location()); ; m = m.AddDate(0, 1, 0) {
		key := m.Format("2006-01")
		if key > last || len(out) >= 24 {
			break
		}
		out = append(out, key)
	}
	return out
}

// seasonOfMonth e' l'etichetta della stagione di un mese "AAAA-MM".
func seasonOfMonth(month string) string {
	t, err := time.Parse("2006-01", month)
	if err != nil {
		return ""
	}
	return season.Label(season.Of(time.Date(t.Year(), t.Month(), 15, 12, 0, 0, 0, time.UTC)))
}

const sqlLayout = "2006-01-02 15:04:05"

func compute(in *input, now time.Time, current int) (stats, monthly []row) {
	start, end := season.Bounds(current)
	windowStart, _ := season.Bounds(current - 1)
	label := season.Label(current)
	months := windowMonths(current, now)
	today := memberchanges.Day(now)
	cutoff7, cutoff30 := today.AddDate(0, 0, -7), today.AddDate(0, 0, -30)
	computedAt, _ := types.ParseDateTime(now)

	for _, o := range in.offices {
		var (
			seasonEvents, upcoming, attendance int
			new7, new30, left30                int
		)
		monthEvents := map[string]int{}
		monthAttendance := map[string]int{}
		monthDistinct := map[string]map[string]bool{}
		monthNew := map[string]int{}
		monthLeft := map[string]int{}

		for _, e := range in.events {
			if e.office != o.id && (o.key == "" || e.key != o.key) {
				continue
			}
			stampers := in.attendees[e.id]
			if e.when.After(now) {
				upcoming++
			} else if !e.when.Before(start) && e.when.Before(end) {
				seasonEvents++
				attendance += len(stampers)
			}
			if e.when.Before(windowStart) || !e.when.Before(end) {
				continue
			}
			m := monthOf(e.when)
			monthEvents[m]++
			monthAttendance[m] += len(stampers)
			if monthDistinct[m] == nil {
				monthDistinct[m] = map[string]bool{}
			}
			for _, u := range stampers {
				monthDistinct[m][u] = true
			}
		}

		for _, c := range in.changes {
			if c.office != o.id {
				continue
			}
			switch {
			case isNew(c.kind):
				if c.day.After(cutoff7) {
					new7++
				}
				if c.day.After(cutoff30) {
					new30++
				}
			case isLeft(c.kind):
				if c.day.After(cutoff30) {
					left30++
				}
			}
			if !c.day.Before(windowStart) {
				m := monthOf(c.day)
				if isNew(c.kind) {
					monthNew[m]++
				} else if isLeft(c.kind) {
					monthLeft[m]++
				}
			}
		}

		active := 0
		if o.key != "" {
			active = in.active[o.key]
		}
		stats = append(stats, row{id: o.id, values: map[string]any{
			"local_office":      o.id,
			"region_key":        o.key,
			"members_active":    active,
			"members_new_7d":    new7,
			"members_new_30d":   new30,
			"members_left_30d":  left30,
			"events_season":     seasonEvents,
			"events_upcoming":   upcoming,
			"attendance_season": attendance,
			"deals_active":      in.deals[o.key],
			"season":            label,
			"computed_at":       computedAt,
		}})

		// Soci a fine mese, all'indietro dal mese corrente: a fine del mese m
		// c'erano quelli di adesso meno il saldo dei mesi dopo m.
		activeAt := make(map[string]int, len(months))
		net := 0
		for i := len(months) - 1; i >= 0; i-- {
			m := months[i]
			// mai sotto zero: i movimenti possono essere piu' vecchi del
			// registro di adesso (soci cancellati, aree corrette a mano)
			activeAt[m] = max(active-net, 0)
			net += monthNew[m] - monthLeft[m]
		}
		for _, m := range months {
			monthly = append(monthly, row{id: o.id + "_" + m, values: map[string]any{
				"local_office":       o.id,
				"region_key":         o.key,
				"month":              m,
				"season":             seasonOfMonth(m),
				"events":             monthEvents[m],
				"attendance":         monthAttendance[m],
				"distinct_attendees": len(monthDistinct[m]),
				"members_new":        monthNew[m],
				"members_left":       monthLeft[m],
				"members_active":     activeAt[m],
			}})
		}
	}
	return stats, monthly
}

// write riscrive una tabella: upsert per id, cancellazione delle righe che
// non ci sono piu'.
func write(tx core.App, table string, rows []row) error {
	collection, err := tx.FindCollectionByNameOrId(table)
	if err != nil {
		return err
	}
	seen := make(map[string]bool, len(rows))
	for _, r := range rows {
		seen[r.id] = true
		record, err := tx.FindRecordById(collection, r.id)
		if err != nil {
			record = core.NewRecord(collection)
			record.Id = r.id
		}
		for k, v := range r.values {
			record.Set(k, v)
		}
		if err := tx.SaveNoValidate(record); err != nil {
			return fmt.Errorf("%s %s: %w", table, r.id, err)
		}
	}
	existing, err := tx.FindAllRecords(collection)
	if err != nil {
		return err
	}
	for _, record := range existing {
		if !seen[record.Id] {
			if err := tx.Delete(record); err != nil {
				return fmt.Errorf("%s: cancellazione %s: %w", table, record.Id, err)
			}
		}
	}
	return nil
}
