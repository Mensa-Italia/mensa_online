// Package memberchanges tiene la storia dei movimenti del registro soci:
// chi e' entrato, chi e' uscito e chi ha cambiato regione, giorno per
// giorno, nella collection members_registry_changes.
//
// Il registro (members_registry) sa solo chi e' attivo adesso e in che
// area: la storia nasce confrontando due fotografie del registro (Diff).
// Il sync con Area32 (tools/dbtools) ne prende una prima e una dopo
// l'aggiornamento e scrive i movimenti (Record); Backfill ricostruisce una
// volta sola la storia passata dagli snapshot giornalieri su S3.
//
// Una riga per movimento: member, name, region_key (la chiave accorpata di
// tools/regionkey), local_office (la sede con quella chiave, vuota se la
// regione non ne ha), kind e day (la mezzanotte di Roma del giorno del
// movimento). I movimenti verso o dall'estero (area vuota o "NaN") non
// scrivono la riga del lato senza regione, e un cambio di area da o verso
// l'area vuota non e' un trasferimento (vedi Diff).
package memberchanges

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"

	"mensadb/tools/regionkey"
	"mensadb/tools/season"
)

// Table e' la collection dei movimenti.
const Table = "members_registry_changes"

// I quattro movimenti.
const (
	Joined   = "joined"    // nuovo socio attivo (o riattivato)
	Left     = "left"      // non piu' attivo
	MovedIn  = "moved_in"  // arrivato da un'altra regione
	MovedOut = "moved_out" // trasferito in un'altra regione
)

// Kinds sono i valori del campo kind, nell'ordine dello schema.
var Kinds = []string{Joined, Left, MovedIn, MovedOut}

// AdminReadRule e' la regola di lettura dei dati riservati di una sede:
// gli amministratori dell'app (powers super) e gli admin di quella sede
// (local_offices_admins). La collection deve avere il campo local_office.
const AdminReadRule = `@request.auth.id != "" && (@request.auth.powers:each ?= "super" || (@collection.local_offices_admins.user ?= @request.auth.id && @collection.local_offices_admins.local_office ?= local_office))`

// ExitsWindowDays sono i giorni di uscite (left, moved_out) che gli admin di
// una sede vedono: oggi e i 6 giorni prima. I super le vedono tutte.
const ExitsWindowDays = 7

// ChangesReadRule e' la regola di lettura di members_registry_changes: gli
// arrivi (joined, moved_in) li legge ogni socio loggato, come il registro
// soci che gia' vede, cosi' la pagina del gruppo locale mostra a tutti i
// nuovi della settimana. Le uscite le vedono i super, tutte, e gli admin
// della sede, solo quelle degli ultimi ExitsWindowDays giorni: day e' la
// mezzanotte di Roma in UTC, e da adesso meno 7 giorni restano proprio
// oggi e i 6 giorni prima.
const ChangesReadRule = `@request.auth.id != "" && (kind = "joined" || kind = "moved_in" || @request.auth.powers:each ?= "super" || (day >= strftime('%Y-%m-%d %H:%M:%fZ', 'now', '-7 days') && @collection.local_offices_admins.user ?= @request.auth.id && @collection.local_offices_admins.local_office ?= local_office))`

// ChangesReadRuleBefore7Days e' la ChangesReadRule di prima del limite di 7
// giorni, per la down della migrazione 1794100004.
const ChangesReadRuleBefore7Days = `@request.auth.id != "" && (kind = "joined" || kind = "moved_in" || @request.auth.powers:each ?= "super" || (@collection.local_offices_admins.user ?= @request.auth.id && @collection.local_offices_admins.local_office ?= local_office))`

// State e' lo stato di un socio in una fotografia del registro.
type State struct {
	Area   string
	Active bool
	Name   string
}

// Snapshot e' una fotografia del registro: numero tessera -> stato.
type Snapshot map[string]State

// Change e' un movimento. RegionKey vuota vuol dire estero o sconosciuta:
// Record non la scrive.
type Change struct {
	Member    string
	Name      string
	Kind      string
	RegionKey string
}

// Un Record alla volta: il sync e il backfill possono sovrapporsi, e il
// controllo dei duplicati legge prima di scrivere.
var recordMu sync.Mutex

// EnsureCollection crea members_registry_changes se manca. Idempotente.
func EnsureCollection(app core.App) error {
	if existing, _ := app.FindCollectionByNameOrId(Table); existing != nil {
		return nil
	}
	registry, err := app.FindCollectionByNameOrId("members_registry")
	if err != nil {
		return err
	}
	offices, err := app.FindCollectionByNameOrId("local_offices")
	if err != nil {
		return err
	}
	c := core.NewBaseCollection(Table)
	rule := ChangesReadRule
	c.ListRule = &rule
	c.ViewRule = &rule
	c.Fields.Add(
		&core.RelationField{Name: "member", CollectionId: registry.Id, MaxSelect: 1},
		&core.TextField{Name: "name"},
		&core.TextField{Name: "region_key"},
		&core.RelationField{Name: "local_office", CollectionId: offices.Id, MaxSelect: 1},
		&core.SelectField{Name: "kind", Values: Kinds, MaxSelect: 1, Required: true},
		&core.DateField{Name: "day"},
		&core.AutodateField{Name: "created", OnCreate: true},
		&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
	)
	c.AddIndex("idx_members_registry_changes_office_day", false, "local_office, day", "")
	c.AddIndex("idx_members_registry_changes_member_day", false, "member, day", "")
	if err := app.Save(c); err != nil {
		return fmt.Errorf("creazione %s: %w", Table, err)
	}
	return nil
}

// Diff confronta due fotografie del registro e ritorna i movimenti, in
// ordine di tessera:
//
//   - assente (o non attivo) prima e attivo dopo: joined nella regione di
//     dopo;
//   - attivo prima e assente (o non attivo) dopo: left nella regione di
//     prima;
//   - attivo prima e dopo, con chiave di regione diversa: moved_out nella
//     regione di prima e moved_in in quella di dopo.
//
// L'area vuota o "NaN" (estero, sconosciuta) ha chiave "". Per joined e
// left il movimento c'e' lo stesso, con la chiave vuota (Record non lo
// scrive). Un cambio di area da o verso la chiave vuota non e' un
// trasferimento: l'area vuota e' quasi sempre un registro a meta' sync, non
// un socio emigrato, e un trasferimento senza regione dall'altro lato non
// direbbe niente a nessuna sede.
func Diff(prev, next Snapshot) []Change {
	ids := make([]string, 0, len(next)+len(prev))
	for id := range next {
		ids = append(ids, id)
	}
	for id := range prev {
		if _, ok := next[id]; !ok {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)

	var out []Change
	for _, id := range ids {
		before, hadBefore := prev[id]
		after, hasAfter := next[id]
		wasActive := hadBefore && before.Active
		isActive := hasAfter && after.Active
		name := after.Name
		if name == "" {
			name = before.Name
		}
		switch {
		case !wasActive && isActive:
			out = append(out, Change{Member: id, Name: name, Kind: Joined, RegionKey: regionkey.OfRegistryArea(after.Area)})
		case wasActive && !isActive:
			out = append(out, Change{Member: id, Name: name, Kind: Left, RegionKey: regionkey.OfRegistryArea(before.Area)})
		case wasActive && isActive:
			from, to := regionkey.OfRegistryArea(before.Area), regionkey.OfRegistryArea(after.Area)
			if from != to && from != "" && to != "" {
				out = append(out,
					Change{Member: id, Name: name, Kind: MovedOut, RegionKey: from},
					Change{Member: id, Name: name, Kind: MovedIn, RegionKey: to},
				)
			}
		}
	}
	return out
}

// Capture legge la fotografia del registro di adesso. Una query sola.
func Capture(app core.App) (Snapshot, error) {
	var rows []struct {
		ID       string `db:"id"`
		Area     string `db:"area"`
		IsActive bool   `db:"is_active"`
		Name     string `db:"name"`
	}
	err := app.DB().NewQuery(`SELECT id, COALESCE(area, '') AS area, COALESCE(is_active, FALSE) AS is_active, COALESCE(name, '') AS name FROM members_registry`).All(&rows)
	if err != nil {
		return nil, fmt.Errorf("lettura del registro: %w", err)
	}
	out := make(Snapshot, len(rows))
	for _, r := range rows {
		out[r.ID] = State{Area: r.Area, Active: r.IsActive, Name: r.Name}
	}
	return out, nil
}

// Day ritorna la mezzanotte di Roma del giorno di t: il valore del campo day.
func Day(t time.Time) time.Time {
	l := season.Local(t)
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, l.Location())
}

// OfficesByKey ritorna l'id della sede per ogni chiave di regione:
// local_offices.region_key, o la chiave calcolata dal nome della regione se
// la colonna e' vuota. Se due sedi hanno la stessa chiave vince la prima
// per id.
func OfficesByKey(app core.App) (map[string]string, error) {
	records, err := app.FindAllRecords("local_offices")
	if err != nil {
		return nil, fmt.Errorf("sedi: %w", err)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Id < records[j].Id })
	out := make(map[string]string, len(records))
	for _, r := range records {
		key := OfficeKey(r)
		if key == "" {
			continue
		}
		if _, ok := out[key]; !ok {
			out[key] = r.Id
		}
	}
	return out, nil
}

// OfficeKey e' la chiave di regione di una sede: region_key se c'e',
// altrimenti calcolata da region.
func OfficeKey(office *core.Record) string {
	if key := office.GetString("region_key"); key != "" {
		return key
	}
	return regionkey.Of(office.GetString("region"))
}

// Record scrive una riga di members_registry_changes per ogni movimento con
// una regione, con day = la mezzanotte di Roma di day. La sede si risolve
// dalla chiave di regione. Idempotente: un movimento gia' scritto per lo
// stesso socio, tipo e giorno non si riscrive.
func Record(app core.App, changes []Change, day time.Time) error {
	written, err := record(app, changes, day)
	if err != nil {
		return err
	}
	if written > 0 {
		app.Logger().Info("[memberchanges] movimenti registrati", "day", season.Day(day), "rows", written)
	}
	return nil
}

func record(app core.App, changes []Change, day time.Time) (int, error) {
	if len(changes) == 0 {
		return 0, nil
	}
	recordMu.Lock()
	defer recordMu.Unlock()

	collection, err := app.FindCollectionByNameOrId(Table)
	if err != nil {
		return 0, err
	}
	offices, err := OfficesByKey(app)
	if err != nil {
		return 0, err
	}
	dayValue, err := types.ParseDateTime(Day(day))
	if err != nil {
		return 0, err
	}

	var existingRows []struct {
		Member string `db:"member"`
		Kind   string `db:"kind"`
	}
	if err := app.DB().NewQuery(`SELECT member, kind FROM ` + Table + ` WHERE day = {:day}`).
		Bind(dbx.Params{"day": dayValue.String()}).All(&existingRows); err != nil {
		return 0, fmt.Errorf("movimenti del giorno: %w", err)
	}
	seen := make(map[string]bool, len(existingRows)+len(changes))
	for _, r := range existingRows {
		seen[r.Member+"|"+r.Kind] = true
	}

	written := 0
	err = app.RunInTransaction(func(tx core.App) error {
		for _, c := range changes {
			if c.RegionKey == "" || c.Member == "" {
				continue
			}
			key := c.Member + "|" + c.Kind
			if seen[key] {
				continue
			}
			seen[key] = true
			record := core.NewRecord(collection)
			record.Set("member", c.Member)
			record.Set("name", c.Name)
			record.Set("region_key", c.RegionKey)
			record.Set("local_office", offices[c.RegionKey])
			record.Set("kind", c.Kind)
			record.Set("day", dayValue)
			// Senza validazione: il socio puo' non essere piu' nel registro
			// (uno snapshot vecchio), la riga resta comunque utile.
			if err := tx.SaveNoValidate(record); err != nil {
				return fmt.Errorf("movimento %s %s: %w", c.Kind, c.Member, err)
			}
			written++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return written, nil
}
