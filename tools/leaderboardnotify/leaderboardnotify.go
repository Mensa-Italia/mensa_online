// Package leaderboardnotify manda le notifiche push della classifica.
//
// Due famiglie:
//
//   - soglie, valutate dopo ogni ricalcolo (leaderboard.AfterRefresh): un
//     socio che entra nei primi dieci, nei primi tre o in testa alla
//     classifica soci, e il segretario (local_offices_admins.is_the_officer)
//     di una sede la cui regione entra nei primi dieci o nei primi tre;
//   - il messaggio quindicinale, dal cron delle 18: a ogni socio tocca
//     quando il giorno dell'anno della sua iscrizione (users.created) e'
//     congruo a oggi modulo 15, e riceve posizione e punti di oggi contro
//     lo snapshot di quindici giorni fa (leaderboard_history), con il
//     prossimo evento nella sua regione se e' sceso o fermo.
//
// Chi non riceve mai niente: chi non ha nemmeno un timbro (non gioca), chi
// ha scelto di non apparire (users_metadata leaderboard_hidden = true), chi
// non ha dispositivi, chi ha spento le notifiche generali (notify_general
// = false) o quelle della classifica (notify_leaderboard = false, acceso se
// assente), e nessuno riceve notifiche di una classifica spenta in configs
// (leaderboard_members_enabled, leaderboard_regions_enabled).
//
// I testi non nominano altri soci ne' regioni: solo i numeri del
// destinatario. Stanno in Tolgee (chiavi leaderboard.notify.*), con una
// copia italiana in texts.it.json come riserva finche' le chiavi non sono
// caricate.
//
// Lo stato (ultima posizione vista, data dell'ultimo invio per soglia e del
// quindicinale) sta in leaderboard_notify_state, una riga per classifica e
// soggetto, solo superuser.
package leaderboardnotify

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"time"

	"mensadb/tools/dbtools"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	StateTable = "leaderboard_notify_state"

	// Chiavi di users_metadata.
	hiddenKey      = "leaderboard_hidden"
	notifyGeneral  = "notify_general"
	notifyBoardKey = "notify_leaderboard"
	notifyRegions  = "notify_me_events"

	// Chiavi di configs con cui l'app accende le classifiche.
	membersEnabledKey = "leaderboard_members_enabled"
	regionsEnabledKey = "leaderboard_regions_enabled"
)

// send e' il punto di invio, sostituibile nei test.
var send = func(app core.App, n dbtools.PushNotification) {
	dbtools.SendPushNotificationToUser(app, n)
}

// EnsureStateCollection crea leaderboard_notify_state se manca. Nessuna
// regola API: solo superuser.
func EnsureStateCollection(app core.App) error {
	if existing, _ := app.FindCollectionByNameOrId(StateTable); existing != nil {
		return nil
	}
	c := core.NewBaseCollection(StateTable)
	c.Fields.Add(
		&core.SelectField{Name: "board", Values: []string{"members", "regions"}, MaxSelect: 1, Required: true},
		&core.TextField{Name: "subject", Required: true},
		// Ultima posizione vista al ricalcolo precedente, 0 = fuori classifica.
		&core.NumberField{Name: "last_position"},
		&core.DateField{Name: "sent_top10"},
		&core.DateField{Name: "sent_top3"},
		&core.DateField{Name: "sent_top1"},
		&core.DateField{Name: "fortnightly_sent"},
		&core.AutodateField{Name: "created", OnCreate: true},
		&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
	)
	c.AddIndex("idx_leaderboard_notify_state_key", true, "board, subject", "")
	if err := app.Save(c); err != nil {
		return fmt.Errorf("creazione %s: %w", StateTable, err)
	}
	return nil
}

// recipients sa chi puo' ricevere queste notifiche. Caricato una volta per
// giro, con quattro query in tutto invece di quattro per socio.
type recipients struct {
	stamped map[string]bool     // almeno una riga in stamp_users
	hidden  map[string]bool     // leaderboard_hidden = true
	devices map[string]bool     // almeno un dispositivo con token
	off     map[string]bool     // notify_general o notify_leaderboard = false
	regions map[string][]string // notify_me_events, per il fallback della regione
}

func loadRecipients(app core.App) (*recipients, error) {
	r := &recipients{
		stamped: map[string]bool{}, hidden: map[string]bool{}, devices: map[string]bool{},
		off: map[string]bool{}, regions: map[string][]string{},
	}
	var ids []string
	if err := app.DB().NewQuery(`SELECT DISTINCT user FROM stamp_users WHERE user != ''`).Column(&ids); err != nil {
		return nil, fmt.Errorf("stamp_users: %w", err)
	}
	for _, id := range ids {
		r.stamped[id] = true
	}
	ids = nil
	if err := app.DB().NewQuery(`SELECT DISTINCT user FROM users_devices WHERE user != '' AND firebase_id != ''`).Column(&ids); err != nil {
		return nil, fmt.Errorf("users_devices: %w", err)
	}
	for _, id := range ids {
		r.devices[id] = true
	}
	var meta []struct {
		User  string `db:"user"`
		Key   string `db:"key"`
		Value string `db:"value"`
	}
	err := app.DB().NewQuery(`SELECT user, key, value FROM users_metadata WHERE key IN ({:a}, {:b}, {:c}, {:d})`).
		Bind(dbx.Params{"a": hiddenKey, "b": notifyGeneral, "c": notifyBoardKey, "d": notifyRegions}).All(&meta)
	if err != nil {
		return nil, fmt.Errorf("users_metadata: %w", err)
	}
	for _, m := range meta {
		switch m.Key {
		case hiddenKey:
			if m.Value == "true" {
				r.hidden[m.User] = true
			}
		case notifyGeneral, notifyBoardKey:
			if m.Value == "false" {
				r.off[m.User] = true
			}
		case notifyRegions:
			var list []string
			if json.Unmarshal([]byte(m.Value), &list) == nil {
				r.regions[m.User] = list
			}
		}
	}
	return r, nil
}

// ok dice se un socio puo' ricevere una notifica della classifica: la
// regola vale per tutte, di soglia e quindicinali, e per i segretari.
func (r *recipients) ok(userID string) bool {
	return userID != "" && r.stamped[userID] && !r.hidden[userID] && r.devices[userID] && !r.off[userID]
}

// boardEnabled legge l'interruttore di una classifica in configs.
func boardEnabled(app core.App, key string) bool {
	return dbtools.GetInternalConfig(app, key) == "true"
}

// stateRows carica le righe di stato di una classifica, per soggetto.
func stateRows(app core.App, board string) (map[string]*core.Record, error) {
	rows, err := app.FindAllRecords(StateTable, dbx.NewExp("board = {:b}", dbx.Params{"b": board}))
	if err != nil {
		return nil, err
	}
	out := make(map[string]*core.Record, len(rows))
	for _, row := range rows {
		out[row.GetString("subject")] = row
	}
	return out, nil
}

func newStateRow(app core.App, board, subject string) (*core.Record, error) {
	collection, err := app.FindCollectionByNameOrId(StateTable)
	if err != nil {
		return nil, err
	}
	row := core.NewRecord(collection)
	row.Set("board", board)
	row.Set("subject", subject)
	return row, nil
}

// variant sceglie una delle n varianti di frase, sempre la stessa per lo
// stesso socio nello stesso giorno: cosi' un rinvio non cambia il testo, e
// soci diversi ricevono frasi diverse.
func variant(userID string, day time.Time, n int) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(userID + "|" + day.Format("2006-01-02")))
	return int(h.Sum32()%uint32(n)) + 1
}

// positionsText scrive un numero di posizioni in italiano, con il
// singolare giusto: "una posizione", "3 posizioni".
func positionsText(n int) string {
	if n < 0 {
		n = -n
	}
	if n == 1 {
		return "una posizione"
	}
	return fmt.Sprintf("%d posizioni", n)
}
