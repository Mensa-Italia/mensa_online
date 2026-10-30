package leaderboardnotify

import (
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"mensadb/tools/dbtools"
	"mensadb/tools/leaderboard"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Una soglia si rinotifica allo stesso soggetto al piu' una volta ogni
// 30 giorni.
const thresholdCooldown = 30 * 24 * time.Hour

// threshold e' una soglia di posizione: il campo di stato con la data
// dell'ultimo invio e la chiave Tolgee del testo.
type threshold struct {
	limit int
	field string
	tag   string
}

// notifyBoard descrive una classifica da sorvegliare: da quale tabella
// leggere, come si chiama nello storico e nello stato, quale interruttore
// in configs, le soglie dalla piu' stretta alla piu' larga, e a chi va la
// notifica di un soggetto (il socio stesso, o i segretari della sede).
type notifyBoard struct {
	table      string
	key        string
	enabledKey string
	thresholds []threshold
	users      func(app core.App, subject string) []string
}

var boards = []notifyBoard{
	{
		table: leaderboard.MembersTable, key: "members", enabledKey: membersEnabledKey,
		thresholds: []threshold{
			{limit: 1, field: "sent_top1", tag: "leaderboard.notify.top1"},
			{limit: 3, field: "sent_top3", tag: "leaderboard.notify.top3"},
			{limit: 10, field: "sent_top10", tag: "leaderboard.notify.top10"},
		},
		users: func(_ core.App, subject string) []string { return []string{subject} },
	},
	{
		table: leaderboard.RegionsTable, key: "regions", enabledKey: regionsEnabledKey,
		thresholds: []threshold{
			{limit: 3, field: "sent_top3", tag: "leaderboard.notify.region_top3"},
			{limit: 10, field: "sent_top10", tag: "leaderboard.notify.region_top10"},
		},
		users: officers,
	},
}

// officers: i segretari (is_the_officer) di una sede.
func officers(app core.App, localOffice string) []string {
	rows, err := app.FindAllRecords("local_offices_admins",
		dbx.NewExp("local_office = {:lo} AND is_the_officer = 1", dbx.Params{"lo": localOffice}))
	if err != nil {
		return nil
	}
	var out []string
	for _, row := range rows {
		if u := row.GetString("user"); u != "" {
			out = append(out, u)
		}
	}
	return out
}

// Thresholds valuta le soglie di tutte le classifiche sorvegliate: va
// chiamata dopo ogni ricalcolo. Gli errori finiscono nel log, un giro
// saltato si recupera al ricalcolo dopo.
func Thresholds(app core.App) {
	ThresholdsAt(app, time.Now())
}

// Una valutazione alla volta: il cron e il ricalcolo richiesto
// dall'opt-out possono finire insieme, e due giri paralleli manderebbero
// la stessa notifica due volte.
var evalMu sync.Mutex

// ThresholdsAt e' Thresholds con l'orologio passato da fuori.
func ThresholdsAt(app core.App, now time.Time) {
	evalMu.Lock()
	defer evalMu.Unlock()
	if err := EnsureStateCollection(app); err != nil {
		app.Logger().Error("[leaderboardnotify] stato", "err", err)
		return
	}
	rec, err := loadRecipients(app)
	if err != nil {
		app.Logger().Error("[leaderboardnotify] destinatari", "err", err)
		return
	}
	for _, board := range boards {
		if err := evaluateBoard(app, now, rec, board); err != nil {
			app.Logger().Error("[leaderboardnotify] soglie", "board", board.key, "err", err)
		}
	}
}

// pending e' una notifica decisa dentro la transazione e mandata dopo il
// commit: se lo stato non si salva, non si manda niente e il giro dopo
// riprova.
type pending struct {
	userID string
	tag    string
	board  string
	params map[string]string
}

// evaluateBoard confronta la classifica di adesso con l'ultima posizione
// vista di ogni soggetto. Lo stato si aggiorna sempre, anche a classifica
// spenta in configs: cosi' riaccenderla non fa partire una raffica di
// notifiche per ingressi di settimane prima.
//
// Primo giro (nessuna riga di stato per la classifica): registra le
// posizioni di adesso senza mandare niente, altrimenti tutta la top 10
// riceverebbe "sei entrato" il giorno del rilascio.
func evaluateBoard(app core.App, now time.Time, rec *recipients, board notifyBoard) error {
	rows, err := app.FindAllRecords(board.table)
	if err != nil {
		return err
	}
	// Si manda solo con la classifica accesa e l'interruttore generale delle
	// soglie a "true"; lo stato si aggiorna comunque.
	enabled := boardEnabled(app, board.enabledKey) && boardEnabled(app, thresholdsEnabledKey)
	var toSend []pending

	err = app.RunInTransaction(func(tx core.App) error {
		states, err := stateRows(tx, board.key)
		if err != nil {
			return err
		}
		seeding := len(states) == 0
		yesterday, err := snapshotPositions(tx, board.key, now)
		if err != nil {
			return err
		}

		seen := map[string]bool{}
		for _, row := range rows {
			subject := row.Id
			seen[subject] = true
			position := int(row.GetFloat("position"))
			state := states[subject]
			if state == nil {
				if state, err = newStateRow(tx, board.key, subject); err != nil {
					return err
				}
			}
			previous := int(state.GetFloat("last_position"))

			if !seeding && position > 0 && (previous == 0 || previous > position) {
				crossed := crossedThresholds(board.thresholds, state, now, position, previous, yesterday[subject])
				if len(crossed) > 0 {
					if enabled {
						params := map[string]string{
							"position": strconv.Itoa(position),
							"score":    strconv.Itoa(int(row.GetFloat("score"))),
						}
						// La soglia piu' stretta fa il testo (da 15 a 1 si
						// riceve "sei il primo", non tre notifiche), ma tutte
						// quelle superate contano come inviate.
						for _, userID := range board.users(tx, subject) {
							if rec.ok(userID) && (board.key != "members" || row.GetFloat("stamps_total") > 0) {
								toSend = append(toSend, pending{userID: userID, tag: crossed[0].tag, board: board.key, params: params})
							}
						}
					}
					for _, t := range crossed {
						state.Set(t.field, now)
					}
				}
			}
			state.Set("last_position", position)
			if err := tx.SaveNoValidate(state); err != nil {
				return fmt.Errorf("stato %s/%s: %w", board.key, subject, err)
			}
		}
		// Chi e' sparito dalla classifica (nascosto, senza piu' punti) e'
		// fuori: se rientra, e' un ingresso.
		for subject, state := range states {
			if !seen[subject] && state.GetFloat("last_position") != 0 {
				state.Set("last_position", 0)
				if err := tx.SaveNoValidate(state); err != nil {
					return fmt.Errorf("stato %s/%s: %w", board.key, subject, err)
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	for _, p := range toSend {
		title, body := fallback(p.tag, p.params)
		send(app, dbtools.PushNotification{
			UserId:        p.userID,
			TrTag:         p.tag,
			TrNamedParams: p.params,
			Title:         title,
			Body:          body,
			Data:          map[string]string{"type": "leaderboard", "board": p.board},
		})
	}
	if len(toSend) > 0 {
		app.Logger().Info("[leaderboardnotify] notifiche di soglia", "board", board.key, "sent", len(toSend))
	}
	return nil
}

// crossedThresholds ritorna le soglie appena superate (posizione nuova
// dentro, precedente fuori o assente) che si possono notificare: nello
// snapshot di ieri il soggetto era fuori dalla soglia (isteresi: chi
// oscilla intorno al decimo posto nella giornata non riceve niente) e
// l'ultimo invio per quella soglia e' di almeno 30 giorni fa. In ordine
// dalla piu' stretta.
func crossedThresholds(thresholds []threshold, state *core.Record, now time.Time, position, previous, yesterday int) []threshold {
	var out []threshold
	for _, t := range thresholds {
		if position > t.limit || (previous != 0 && previous <= t.limit) {
			continue
		}
		if yesterday != 0 && yesterday <= t.limit {
			continue
		}
		if last := state.GetDateTime(t.field); !last.IsZero() && now.Sub(last.Time()) < thresholdCooldown {
			continue
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].limit < out[j].limit })
	return out
}

// snapshotPositions legge dallo storico le posizioni dell'ultimo snapshot
// prima di oggi (quello di ieri sera, normalmente), per soggetto. Chi non
// c'e' era fuori. Senza storico, nessuno era dentro.
func snapshotPositions(app core.App, board string, now time.Time) (map[string]int, error) {
	var rows []struct {
		Subject  string  `db:"subject"`
		Position float64 `db:"position"`
	}
	err := app.DB().NewQuery(`
		SELECT subject, position FROM ` + leaderboard.HistoryTable + `
		WHERE board = {:board}
		  AND day = (SELECT MAX(day) FROM ` + leaderboard.HistoryTable + ` WHERE board = {:board} AND day < {:today})
	`).Bind(dbx.Params{"board": board, "today": now.Format("2006-01-02")}).All(&rows)
	if err != nil {
		return nil, fmt.Errorf("storico: %w", err)
	}
	out := make(map[string]int, len(rows))
	for _, r := range rows {
		out[r.Subject] = int(r.Position)
	}
	return out, nil
}
