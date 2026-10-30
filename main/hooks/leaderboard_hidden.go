package hooks

import (
	"mensadb/tools/leaderboard"

	"github.com/pocketbase/pocketbase/core"
)

// La chiave di users_metadata con cui un socio esce dalla classifica dei
// soci (la scrive l'app dalle impostazioni della classifica). La stessa
// stringa sta nella query di tools/leaderboard/sql/soci.sql.
const leaderboardHiddenKey = "leaderboard_hidden"

// LeaderboardHiddenChanged fa valere subito la scelta di apparire o no in
// classifica. Le classifiche sono tabelle riempite da un cron ogni quarto
// d'ora, quindi senza questo chi si nasconde resterebbe visibile fino al
// ricalcolo successivo: qui la sua riga sparisce subito, e il ricalcolo
// (che ridistribuisce le posizioni, o lo fa rientrare se ha scelto di
// tornare) parte in sottofondo pochi secondi dopo, uno solo anche se i
// cambi sono tanti, senza far aspettare la risposta.
func LeaderboardHiddenChanged(e *core.RecordEvent) error {
	if e.Record.GetString("key") != leaderboardHiddenKey {
		return e.Next()
	}
	userID := e.Record.GetString("user")
	if e.Record.GetString("value") == "true" && userID != "" {
		if err := leaderboard.Hide(e.App, userID); err != nil {
			e.App.Logger().Error("[leaderboard] hide", "user", userID, "err", err)
		}
	}
	// Con debounce: cento toggle di fila sono un ricalcolo solo.
	leaderboard.RequestRefresh(e.App)
	return e.Next()
}
