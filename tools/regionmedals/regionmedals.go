// Package regionmedals calcola le medaglie di regione: dieci traguardi
// stagionali per ogni sede (local_offices), tre metalli ciascuno, che la
// regione conquista come squadra.
//
// La tabella leaderboard_region_medals ha una riga per sede, stagione e
// medaglia. La stagione e' quella di tools/season: dal 1 ottobre al 30
// settembre, ora di Roma, e il campo season e' l'anno in cui comincia. Si
// ricalcola dopo ogni ricalcolo delle classifiche (leaderboard.AfterRefresh):
// la query in sql/medals.sql da' i numeri della stagione in corso, le soglie
// stanno in medals.go. Per ogni riga:
//
//   - current_tier e' il metallo che i dati danno adesso, tier il massimo
//     raggiunto nella stagione, che non scende mai (un evento in meno o un
//     socio in piu' nel denominatore non tolgono una medaglia gia' presa);
//   - unlocked ricorda quando ogni metallo e' stato raggiunto per la prima
//     volta, witnesses chi c'era: i soci attivi del registro della regione
//     in quel momento, perche' il medagliere possa dire "c'eri anche tu";
//   - il Podio non si decide sui dati di adesso ma sullo snapshot del 30
//     settembre della classifica delle regioni (leaderboard_history), o,
//     se manca, sull'albo d'oro (leaderboard_seasons) scritto alla chiusura:
//     finche' non c'e' resta "none", value e' sempre la posizione di adesso.
//     Al primo ricalcolo della stagione dopo si assegna anche il Podio della
//     stagione appena chiusa, se la riga e' ancora senza metallo.
//
// Le medaglie non danno punti in nessuna classifica.
//
// Un ricalcolo ritorna i metalli sbloccati (Unlock) perche' chi lo chiama
// possa avvisare la regione. Una riga appena creata non genera sblocchi: i
// metalli che ha gia' al primo ricalcolo (primo avvio, primo giorno della
// stagione, ricalcolo di una stagione passata) sono preesistenti.
package regionmedals

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"

	"mensadb/tools/leaderboard"
	"mensadb/tools/season"
)

// Table e' la collection delle medaglie di regione.
const Table = "leaderboard_region_medals"

//go:embed sql/medals.sql
var medalsSQL string

// Unlock e' un metallo raggiunto per la prima volta da una regione, su
// una riga che c'era gia': quello piu' alto, se ne ha saltati.
type Unlock struct {
	LocalOffice string
	RegionKey   string
	Medal       string
	Tier        string
	Season      int
}

// Un ricalcolo alla volta: il cron e il ricalcolo richiesto dall'opt-out
// possono sovrapporsi.
var refreshMu sync.Mutex

// Refresh ricalcola le medaglie della stagione in corso. Vedi RefreshUnlocks.
func Refresh(app core.App) error {
	_, err := RefreshUnlocks(app)
	return err
}

// RefreshUnlocks ricalcola le medaglie della stagione in corso in una sola
// transazione e ritorna i metalli sbloccati, solo se la transazione si
// chiude. Le righe di altre sedi o altre stagioni (a parte il Podio della
// stagione scorsa) non si toccano.
func RefreshUnlocks(app core.App) ([]Unlock, error) {
	return RefreshUnlocksAt(app, time.Now())
}

// RefreshUnlocksAt e' RefreshUnlocks con l'orologio passato da fuori.
func RefreshUnlocksAt(app core.App, now time.Time) ([]Unlock, error) {
	return RefreshSeasonAt(app, season.Current(now), now)
}

// RefreshSeasonAt ricalcola le medaglie della stagione s, che puo' essere
// anche una stagione chiusa (la migrazione che introduce la stagione ricalcola
// la scorsa). Per la stagione in corso i numeri sono quelli di adesso, per
// una chiusa la finestra e' la sua e il Podio viene dallo snapshot finale.
// now serve alla data di sblocco dei metalli nuovi.
func RefreshSeasonAt(app core.App, s int, now time.Time) ([]Unlock, error) {
	refreshMu.Lock()
	defer refreshMu.Unlock()
	started := time.Now()

	var unlocks []Unlock
	err := app.RunInTransaction(func(tx core.App) error {
		var err error
		unlocks, err = refreshSeason(tx, s, now, s == season.Current(now))
		return err
	})
	if err != nil {
		return nil, err
	}
	app.Logger().Info("[regionmedals] medaglie ricalcolate", "season", season.Label(s), "unlocked", len(unlocks), "in", time.Since(started).String())
	return unlocks, nil
}

// SettlePodium assegna il Podio della stagione s, appena chiusa, alle righe
// ancora senza metallo, dallo snapshot del 30 settembre o dall'albo d'oro.
// Lo chiama il passaggio di stagione; ritorna gli sblocchi per le notifiche.
func SettlePodium(app core.App, s int) ([]Unlock, error) {
	refreshMu.Lock()
	defer refreshMu.Unlock()
	var unlocks []Unlock
	err := app.RunInTransaction(func(tx core.App) error {
		collection, err := tx.FindCollectionByNameOrId(Table)
		if err != nil {
			return err
		}
		members, err := ActiveMembers(tx)
		if err != nil {
			return err
		}
		r := &run{tx: tx, collection: collection, members: members, stamp: time.Now().UTC().Format(types.DefaultDateLayout)}
		unlocks, err = r.settlePodium(s)
		return err
	})
	if err != nil {
		return nil, err
	}
	return unlocks, nil
}

// run raccoglie quello che serve a scrivere le righe di un ricalcolo.
type run struct {
	tx         core.App
	collection *core.Collection
	members    map[string][]string
	stamp      string
}

// target e' la sede di una riga.
type target struct {
	office, region, regionKey string
}

// reading e' quello che i dati dicono di una medaglia, prima di
// confrontarlo con la riga che c'e' gia'.
type reading struct {
	value, count, extra float64
	eligible            bool
	current             string
}

func refreshSeason(tx core.App, s int, now time.Time, live bool) ([]Unlock, error) {
	collection, err := tx.FindCollectionByNameOrId(Table)
	if err != nil {
		return nil, err
	}
	// Schema precedente alla stagione (le migrazioni vecchie chiamano il
	// primo riempimento prima di quella che rinomina year in season): ci
	// pensa la migrazione della stagione.
	if collection.Fields.GetByName("season") == nil {
		return nil, nil
	}

	var rows []metrics
	if err := tx.DB().NewQuery(medalsSQL).Bind(season.Params(s)).All(&rows); err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	members, err := ActiveMembers(tx)
	if err != nil {
		return nil, err
	}
	snapshot, err := podiumSnapshot(tx, s)
	if err != nil {
		return nil, err
	}
	existing, err := tx.FindAllRecords(collection, dbx.HashExp{"season": s})
	if err != nil {
		return nil, err
	}
	byKey := make(map[string]*core.Record, len(existing))
	for _, record := range existing {
		byKey[record.GetString("local_office")+"|"+record.GetString("medal")] = record
	}

	r := &run{tx: tx, collection: collection, members: members, stamp: now.UTC().Format(types.DefaultDateLayout)}
	var unlocks []Unlock
	for i := range rows {
		m := &rows[i]
		if !live {
			// La classifica viva e' della stagione in corso: per una
			// chiusa la posizione e' quella dello snapshot finale.
			m.PodiumPosition, m.PodiumScore = snapshot[m.ID].position, snapshot[m.ID].score
		}
		t := target{office: m.ID, region: m.Region, regionKey: m.RegionKey}
		for _, def := range Medals {
			value, count, extra := def.measure(m)
			rd := reading{value: def.store(value), count: count, extra: extra, eligible: count >= def.Minimum}
			switch {
			case def.Key == PodiumKey:
				// Il metallo viene dallo snapshot, il valore resta quello di adesso.
				rd.current = TierNone
				if snapshot != nil {
					rd.current = def.tierFor(snapshot[m.ID].position)
				}
			case rd.eligible:
				rd.current = def.tierFor(rd.value)
			default:
				rd.current = TierNone
			}
			u, err := r.upsert(byKey[m.ID+"|"+def.Key], t, def, rd, s)
			if err != nil {
				return nil, err
			}
			if u != nil {
				unlocks = append(unlocks, *u)
			}
		}
	}

	prev, err := r.settlePodium(s - 1)
	if err != nil {
		return nil, err
	}
	return append(unlocks, prev...), nil
}

// settlePodium assegna il Podio di una stagione chiusa alle righe ancora
// senza metallo, se lo snapshot del 30 settembre (o l'albo d'oro) c'e'.
// Serve al primo ricalcolo della stagione dopo: lo snapshot arriva alle
// 23:55, il ricalcolo che lo vede gira dopo la mezzanotte, quando la
// stagione in corso e' gia' la nuova.
func (r *run) settlePodium(s int) ([]Unlock, error) {
	snapshot, err := podiumSnapshot(r.tx, s)
	if err != nil || snapshot == nil {
		return nil, err
	}
	rows, err := r.tx.FindAllRecords(r.collection, dbx.HashExp{"season": s, "medal": PodiumKey})
	if err != nil {
		return nil, err
	}
	def := Medals[len(Medals)-1]
	var unlocks []Unlock
	for _, record := range rows {
		if rank(record.GetString("tier")) > 0 {
			continue
		}
		position := snapshot[record.GetString("local_office")].position
		current := def.tierFor(position)
		if current == TierNone {
			continue
		}
		t := target{office: record.GetString("local_office"), region: record.GetString("region"), regionKey: record.GetString("region_key")}
		rd := reading{value: position, count: record.GetFloat("count"), extra: record.GetFloat("extra"), eligible: true, current: current}
		u, err := r.upsert(record, t, def, rd, s)
		if err != nil {
			return nil, err
		}
		if u != nil {
			unlocks = append(unlocks, *u)
		}
	}
	return unlocks, nil
}

// upsert scrive la riga di una medaglia: crea quella che manca, aggiorna
// i numeri, alza il metallo massimo senza mai abbassarlo e, per ogni
// metallo nuovo, salva quando e con chi. Non salva se non e' cambiato
// niente, cosi' updated dice davvero quando e' cambiata. Ritorna lo
// sblocco, se la riga c'era gia' e ha preso un metallo in piu'.
func (r *run) upsert(record *core.Record, t target, def definition, rd reading, s int) (*Unlock, error) {
	created := record == nil
	previous := TierNone
	unlocked := map[string]string{}
	witnesses := map[string][]string{}
	if created {
		record = core.NewRecord(r.collection)
		record.Set("local_office", t.office)
		record.Set("season", s)
		record.Set("medal", def.Key)
	} else {
		previous = record.GetString("tier")
		_ = record.UnmarshalJSONField("unlocked", &unlocked)
		_ = record.UnmarshalJSONField("witnesses", &witnesses)
	}

	if r.collection.Fields.GetByName("year") != nil {
		// alias di season per l'app 22.4.0, finche' e' in giro
		record.Set("year", s)
	}
	best := max(rank(previous), rank(rd.current))
	for level := 1; level <= best; level++ {
		tier := tierAt(level)
		if _, done := unlocked[tier]; done {
			continue
		}
		unlocked[tier] = r.stamp
		witnesses[tier] = append([]string{}, r.members[t.regionKey]...)
	}

	record.Set("region", t.region)
	record.Set("region_key", t.regionKey)
	record.Set("value", rd.value)
	record.Set("count", rd.count)
	record.Set("extra", rd.extra)
	record.Set("levels", def.Levels.json())
	record.Set("minimum", def.Minimum)
	record.Set("eligible", rd.eligible)
	record.Set("current_tier", rd.current)
	record.Set("tier", tierAt(best))
	record.Set("unlocked", unlocked)
	record.Set("witnesses", witnesses)

	if !created && !differs(record) {
		return nil, nil
	}
	if err := r.tx.SaveNoValidate(record); err != nil {
		return nil, fmt.Errorf("salvataggio %s/%s: %w", t.office, def.Key, err)
	}
	if created || best <= rank(previous) {
		return nil, nil
	}
	return &Unlock{LocalOffice: t.office, RegionKey: t.regionKey, Medal: def.Key, Tier: tierAt(best), Season: s}, nil
}

// trackedFields sono i campi che un ricalcolo riscrive.
var trackedFields = []string{
	"region", "region_key", "value", "count", "extra", "levels", "minimum",
	"eligible", "current_tier", "tier", "unlocked", "witnesses",
}

// differs dice se un campo di una riga e' diverso da com'era al
// caricamento. Il confronto passa dal JSON, che tratta uguali un numero
// letto dal DB e uno appena calcolato, e due json con lo stesso contenuto.
func differs(record *core.Record) bool {
	original := record.Original()
	for _, name := range trackedFields {
		if canonical(record.Get(name)) != canonical(original.Get(name)) {
			return true
		}
	}
	return false
}

func canonical(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	var generic any
	if json.Unmarshal(raw, &generic) != nil {
		return string(raw)
	}
	out, _ := json.Marshal(generic)
	return string(out)
}

// standing e' la posizione e il punteggio di una sede in classifica.
type standing struct {
	position, score float64
}

// podiumSnapshot legge le posizioni dello snapshot del 30 settembre della
// classifica delle regioni per la stagione s, per sede. Se lo snapshot non
// c'e' prova con l'albo d'oro (leaderboard_seasons), che il passaggio di
// stagione scrive. Nil se non c'e' nessuno dei due.
func podiumSnapshot(tx core.App, s int) (map[string]standing, error) {
	var rows []struct {
		Subject  string  `db:"subject"`
		Position float64 `db:"position"`
		Score    float64 `db:"score"`
	}
	err := tx.DB().NewQuery(`SELECT subject, position, score FROM ` + leaderboard.HistoryTable + ` WHERE board = 'regions' AND day = {:day}`).
		Bind(dbx.Params{"day": season.LastDay(s)}).All(&rows)
	if err != nil {
		return nil, fmt.Errorf("storico: %w", err)
	}
	if len(rows) == 0 {
		err := tx.DB().NewQuery(`SELECT subject, position, score FROM ` + leaderboard.SeasonsTable + ` WHERE board = 'regions' AND season = {:s}`).
			Bind(dbx.Params{"s": s}).All(&rows)
		if err != nil {
			return nil, fmt.Errorf("albo d'oro: %w", err)
		}
	}
	if len(rows) == 0 {
		return nil, nil
	}
	out := make(map[string]standing, len(rows))
	for _, row := range rows {
		out[row.Subject] = standing{position: row.Position, score: row.Score}
	}
	return out, nil
}

// ActiveMembers ritorna gli id dei soci attivi del registro per regione
// (la chiave accorpata, come region_key delle classifiche). Una query sola.
func ActiveMembers(app core.App) (map[string][]string, error) {
	var rows []struct {
		ID        string `db:"id"`
		RegionKey string `db:"region_key"`
	}
	err := app.DB().NewQuery(`
		SELECT mr.id AS id,
		  CASE mr.area WHEN 'Trentino-Alto Adige/Südtirol' THEN 'Trentino-Alto Adige' WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE mr.area END AS region_key
		FROM members_registry mr
		WHERE mr.is_active AND mr.area NOT IN ('', 'NaN')
		ORDER BY mr.id
	`).All(&rows)
	if err != nil {
		return nil, fmt.Errorf("soci della regione: %w", err)
	}
	out := map[string][]string{}
	for _, row := range rows {
		out[row.RegionKey] = append(out[row.RegionKey], row.ID)
	}
	return out, nil
}
