package leaderboardnotify

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"mensadb/tools/dbtools"
	"mensadb/tools/leaderboard"
	"mensadb/tools/season"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

// Lo storico comincia il 30 ottobre 2026: prima di questa data non ci sono
// quindici giorni da confrontare e il cron non manda niente.
var FortnightlyStart = time.Date(2026, time.November, 14, 0, 0, 0, 0, time.UTC)

const (
	// Ogni socio riceve il messaggio ogni 15 giorni: il giorno e' deciso dal
	// giorno dell'anno della sua iscrizione, cosi' il carico si spalma.
	fortnightlyPeriod = 15
	// Con meno di 14 giorni di storico non si confronta niente.
	fortnightlyMinHistoryDays = 14
)

// Fortnightly manda il messaggio quindicinale ai soci di turno oggi. Va
// chiamato una volta al giorno (cron delle 18). Ritorna quante notifiche
// ha mandato.
func Fortnightly(app core.App, now time.Time) (int, error) {
	if now.Before(FortnightlyStart) {
		return 0, nil
	}
	if !boardEnabled(app, membersEnabledKey) || !boardEnabled(app, fortnightlyEnabledKey) {
		return 0, nil
	}
	if err := EnsureStateCollection(app); err != nil {
		return 0, err
	}
	rec, err := loadRecipients(app)
	if err != nil {
		return 0, err
	}
	rows, err := app.FindAllRecords(leaderboard.MembersTable)
	if err != nil {
		return 0, err
	}
	created, err := usersCreated(app)
	if err != nil {
		return 0, err
	}
	states, err := stateRows(app, "members")
	if err != nil {
		return 0, err
	}

	sent := 0
	for _, row := range rows {
		userID := row.Id
		if !rec.ok(userID) || row.GetFloat("stamps_total") <= 0 {
			continue
		}
		if !IsTurn(created[userID], now) {
			continue
		}
		state := states[userID]
		if state == nil {
			if state, err = newStateRow(app, "members", userID); err != nil {
				return sent, err
			}
			state.Set("last_position", row.GetFloat("position"))
		}
		// Un secondo giro nello stesso ciclo (cron lanciato a mano) non
		// manda due volte.
		if last := state.GetDateTime("fortnightly_sent"); !last.IsZero() && now.Sub(last.Time()) < (fortnightlyPeriod-1)*24*time.Hour {
			continue
		}
		old, ok, err := historyRow(app, userID, now)
		if err != nil {
			return sent, err
		}
		if !ok {
			continue
		}
		n := fortnightlyMessage(app, rec, userID, now, int(row.GetFloat("position")), int(row.GetFloat("score")), old)
		send(app, n)
		state.Set("fortnightly_sent", now)
		if err := app.SaveNoValidate(state); err != nil {
			return sent, fmt.Errorf("stato members/%s: %w", userID, err)
		}
		sent++
	}
	if sent > 0 {
		app.Logger().Info("[leaderboardnotify] quindicinale", "sent", sent)
	}
	return sent, nil
}

// IsTurn dice se oggi tocca a un socio iscritto in `created`: il giorno
// dell'anno dell'iscrizione e quello di oggi sono congrui modulo 15.
// Senza data (socio non trovato) non tocca mai.
func IsTurn(created, now time.Time) bool {
	if created.IsZero() {
		return false
	}
	return created.YearDay()%fortnightlyPeriod == now.YearDay()%fortnightlyPeriod
}

// usersCreated legge la data di iscrizione di ogni socio.
func usersCreated(app core.App) (map[string]time.Time, error) {
	var rows []struct {
		ID      string         `db:"id"`
		Created types.DateTime `db:"created"`
	}
	if err := app.DB().NewQuery(`SELECT id, created FROM users`).All(&rows); err != nil {
		return nil, fmt.Errorf("users: %w", err)
	}
	out := make(map[string]time.Time, len(rows))
	for _, r := range rows {
		out[r.ID] = r.Created.Time()
	}
	return out, nil
}

// snapshotRow e' una riga dello storico.
type snapshotRow struct {
	Day      string  `db:"day"`
	Position float64 `db:"position"`
	Score    float64 `db:"score"`
}

// historyRow ritorna la riga di quindici giorni fa del socio, o la piu'
// vicina a quella data fra quelle con almeno 14 giorni, o niente se lo
// storico della stagione in corso e' piu' corto.
func historyRow(app core.App, userID string, now time.Time) (snapshotRow, bool, error) {
	var rows []snapshotRow
	err := app.DB().NewQuery(`
		SELECT day, position, score FROM ` + leaderboard.HistoryTable + `
		WHERE board = 'members' AND subject = {:subject} AND day <= {:latest} AND day >= {:since}
		ORDER BY day DESC
	`).Bind(dbx.Params{
		"subject": userID,
		"latest":  now.AddDate(0, 0, -fortnightlyMinHistoryDays).Format("2006-01-02"),
		// solo la stagione in corso: nelle prime due settimane di ottobre
		// non si confronta con la classifica della stagione chiusa
		"since": season.FirstDay(season.Of(now)),
	}).All(&rows)
	if err != nil {
		return snapshotRow{}, false, fmt.Errorf("storico: %w", err)
	}
	return pickHistoryRow(rows, now)
}

// pickHistoryRow sceglie, fra le righe con almeno 14 giorni (dalla piu'
// recente), quella piu' vicina a quindici giorni fa.
func pickHistoryRow(rows []snapshotRow, now time.Time) (snapshotRow, bool, error) {
	if len(rows) == 0 {
		return snapshotRow{}, false, nil
	}
	target := now.AddDate(0, 0, -fortnightlyPeriod).Format("2006-01-02")
	best := rows[0]
	for _, r := range rows[1:] {
		if dayDistance(r.Day, target) < dayDistance(best.Day, target) {
			best = r
		}
	}
	return best, true, nil
}

func dayDistance(a, b string) int {
	ta, errA := time.Parse("2006-01-02", a)
	tb, errB := time.Parse("2006-01-02", b)
	if errA != nil || errB != nil {
		return 1 << 30
	}
	d := int(ta.Sub(tb).Hours() / 24)
	if d < 0 {
		d = -d
	}
	return d
}

// fortnightlyMessage compone la notifica: salito, sceso o fermo rispetto
// alla riga dello storico, e per gli ultimi due il prossimo evento nella
// regione del socio, se c'e', con il deep link all'evento.
func fortnightlyMessage(app core.App, rec *recipients, userID string, now time.Time, position, score int, old snapshotRow) dbtools.PushNotification {
	delta := int(old.Position) - position
	params := map[string]string{
		"position": strconv.Itoa(position),
		"score":    strconv.Itoa(score),
	}
	data := map[string]string{"type": "leaderboard", "board": "members"}
	v := strconv.Itoa(variant(userID, now, Variants))

	var tag string
	switch {
	case delta > 0:
		params["gained"] = positionsText(delta)
		tag = "leaderboard.notify.up." + v
	default:
		event := nextEvent(app, now, memberRegions(app, rec, userID))
		base := "still"
		if delta < 0 {
			params["lost"] = positionsText(delta)
			base = "down"
		}
		if event != nil {
			params["event"] = event.GetString("name")
			params["date"] = italianDate(event.GetDateTime("when_start").Time())
			data = map[string]string{"type": "event", "event_id": event.Id}
			tag = "leaderboard.notify." + base + "." + v
		} else {
			tag = "leaderboard.notify." + base + "_noevent." + v
		}
	}
	title, body := fallback(tag, params)
	return dbtools.PushNotification{
		UserId:        userID,
		TrTag:         tag,
		TrNamedParams: params,
		Title:         title,
		Body:          body,
		Data:          data,
	}
}

// memberRegions: la regione del socio dal registro (members_registry.area),
// altrimenti quelle scelte per le notifiche degli eventi (notify_me_events).
// Normalizzate come positions.state.
func memberRegions(app core.App, rec *recipients, userID string) []string {
	if mr, err := app.FindRecordById("members_registry", userID); err == nil {
		if area := regionKey(mr.GetString("area")); area != "" {
			return []string{area}
		}
	}
	var out []string
	for _, r := range rec.regions[userID] {
		if k := regionKey(r); k != "" {
			out = append(out, k)
		}
	}
	return out
}

// regionKey porta un nome di regione (members_registry.area,
// positions.state, notify_me_events) alla chiave con cui le classifiche
// accorpano le sedi doppie: gli stessi CASE di tools/leaderboard/sql.
func regionKey(name string) string {
	name = strings.TrimSpace(name)
	switch name {
	case "", "NaN":
		return ""
	case "Trentino-Alto Adige/Südtirol", "Trentino Alto Adige":
		return "Trentino-Alto Adige"
	case "Valle d'Aosta", "Piemonte e Val d'Aosta":
		return "Piemonte"
	case "Basilicata", "Basilicata e Puglia":
		return "Puglia"
	case "Emilia Romagna":
		return "Emilia-Romagna"
	case "Friuli Venezia Giulia":
		return "Friuli-Venezia Giulia"
	}
	return name
}

// nextEvent: il primo evento in calendario, da adesso in poi, che si
// svolge in una delle regioni date. Niente se non ce ne sono.
func nextEvent(app core.App, now time.Time, regions []string) *core.Record {
	if len(regions) == 0 {
		return nil
	}
	wanted := map[string]bool{}
	for _, r := range regions {
		wanted[r] = true
	}
	events, err := app.FindRecordsByFilter("events",
		"when_start > {:now} && position != ''", "when_start", 200, 0,
		dbx.Params{"now": now.UTC().Format(types.DefaultDateLayout)})
	if err != nil {
		return nil
	}
	states := map[string]string{}
	for _, event := range events {
		positionID := event.GetString("position")
		state, ok := states[positionID]
		if !ok {
			if p, err := app.FindRecordById("positions", positionID); err == nil {
				state = regionKey(p.GetString("state"))
			}
			states[positionID] = state
		}
		if state != "" && wanted[state] {
			return event
		}
	}
	return nil
}

var italianWeekdays = [...]string{"domenica", "lunedì", "martedì", "mercoledì", "giovedì", "venerdì", "sabato"}
var italianMonths = [...]string{"", "gennaio", "febbraio", "marzo", "aprile", "maggio", "giugno", "luglio", "agosto", "settembre", "ottobre", "novembre", "dicembre"}

// italianDate scrive una data come "giovedì 12 novembre", nel fuso italiano.
func italianDate(t time.Time) string {
	if loc, err := time.LoadLocation("Europe/Rome"); err == nil {
		t = t.In(loc)
	}
	return fmt.Sprintf("%s %d %s", italianWeekdays[t.Weekday()], t.Day(), italianMonths[t.Month()])
}
