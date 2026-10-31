// Package rollover chiude le stagioni: al primo giro del cron dopo il 1
// ottobre archivia quella appena finita e solo dopo i ricalcoli della
// stagione nuova.
//
// Per ogni stagione chiusa scrive l'albo d'oro delle classifiche
// (leaderboard.Finalize), il metallo finale di ogni socio per le medaglie
// stagionali (medals.FinalizeMedals) e il Podio delle medaglie di regione
// (regionmedals.SettlePodium). Ricordarsi fin dove si e' arrivati e' compito
// della chiave leaderboard_season di configs: l'ultima stagione di cui il cron
// ha gia' fatto i ricalcoli. Se manca, la prima esecuzione archivia la
// stagione precedente a quella in corso (e' il caso di un database dove la
// migrazione della stagione non ha potuto archiviare); se e' indietro di piu'
// stagioni (server fermo a cavallo di un ottobre) le archivia tutte, una dopo
// l'altra, con la finestra di ciascuna: il calcolo parte dai dati, non dalle
// tabelle vive, quindi non conta quando gira.
//
// I ricalcoli della stagione nuova (medals.Refresh, leaderboard.Refresh,
// regionmedals) non dipendono da questo giro: lo stato delle notifiche delle
// medaglie e' per stagione e le righe nuove si seminano in silenzio, quindi
// nessun avviso parte per il cambio di stagione.
package rollover

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"mensadb/tools/dbtools"
	"mensadb/tools/leaderboard"
	"mensadb/tools/medals"
	"mensadb/tools/regionmedals"
	"mensadb/tools/season"
)

// ConfigKey e' la chiave di configs con l'ultima stagione di cui il cron ha
// fatto i ricalcoli.
const ConfigKey = "leaderboard_season"

// Stored ritorna la stagione salvata, e se c'e'.
func Stored(app core.App) (int, bool) {
	raw := strings.TrimSpace(dbtools.GetInternalConfig(app, ConfigKey))
	if raw == "" {
		return 0, false
	}
	s, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	return s, true
}

// Store salva la stagione corrente.
func Store(app core.App, s int) error {
	return dbtools.SetInternalConfig(app, ConfigKey, strconv.Itoa(s))
}

// Archive archivia una stagione chiusa: albo d'oro, annata delle medaglie e
// Podio delle regioni. Idempotente. Ritorna i Podi sbloccati, per le
// notifiche di chi chiama.
func Archive(app core.App, s int) ([]regionmedals.Unlock, error) {
	label := season.Label(s)
	if err := leaderboard.Finalize(app, s); err != nil {
		return nil, fmt.Errorf("albo d'oro %s: %w", label, err)
	}
	app.Logger().Info("[rollover] albo d'oro scritto", "season", label)
	if err := medals.FinalizeMedals(app, s); err != nil {
		return nil, fmt.Errorf("annata delle medaglie %s: %w", label, err)
	}
	app.Logger().Info("[rollover] annata delle medaglie scritta", "season", label)
	unlocks, err := regionmedals.SettlePodium(app, s)
	if err != nil {
		return nil, fmt.Errorf("podio delle regioni %s: %w", label, err)
	}
	app.Logger().Info("[rollover] podio delle regioni assegnato", "season", label, "unlocked", len(unlocks))
	return unlocks, nil
}

// Run controlla se la stagione e' cambiata dall'ultimo giro e, se si', archivia
// quelle chiuse e salva la nuova. Da chiamare prima dei ricalcoli del cron. Se
// un passo fallisce si ferma, la stagione salvata resta indietro e il giro
// dopo riprova. Ritorna i Podi sbloccati.
func Run(app core.App, now time.Time) ([]regionmedals.Unlock, error) {
	current := season.Current(now)
	stored, ok := Stored(app)
	if ok && stored >= current {
		return nil, nil
	}
	first := current - 1
	if ok {
		first = stored
	}
	app.Logger().Info("[rollover] cambio di stagione", "from", season.Label(first), "to", season.Label(current))
	var all []regionmedals.Unlock
	for s := first; s < current; s++ {
		unlocks, err := Archive(app, s)
		if err != nil {
			return all, err
		}
		all = append(all, unlocks...)
		if err := Store(app, s+1); err != nil {
			return all, err
		}
	}
	app.Logger().Info("[rollover] stagione archiviata", "season", season.Label(current))
	return all, nil
}
