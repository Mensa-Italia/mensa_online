package leaderboardnotify

import (
	"strconv"

	"mensadb/tools/dbtools"
	"mensadb/tools/medals"

	"github.com/pocketbase/pocketbase/core"
)

// Chiavi Tolgee delle notifiche del medagliere personale (tools/medals).
// {medal} e' il titolo italiano della medaglia, {tier} il metallo (Bronzo,
// Argento, Oro, Platino), {count} quante medaglie sono cambiate.
const (
	medalEarnedTag   = "medals.notify.earned"
	medalUpgradedTag = "medals.notify.upgraded"
	medalManyTag     = "medals.notify.many"
)

// MedalsMaxSingle e' quante medaglie cambiate nello stesso giro si
// avvisano una per una: oltre, un solo messaggio riassuntivo (il primo
// giro dopo un ricalcolo grosso non deve riempire il telefono).
const MedalsMaxSingle = 3

// MedalsChanged avvisa i soci che hanno preso una medaglia nuova (From =
// none) o l'hanno vista salire di livello: una notifica per medaglia,
// oppure una sola "medagliere aggiornato" se per lo stesso socio ne sono
// cambiate piu' di MedalsMaxSingle. Si aggancia a medals.AfterRefresh.
//
// Parte solo con medals_notify_enabled a "true" in configs (spento se
// manca); i destinatari passano lo stesso filtro di tutte le notifiche
// della classifica (almeno un timbro, non nascosto, con dispositivo,
// notifiche generali e della classifica accese). La classifica puo' essere
// spenta: le medaglie non dipendono da lei. Ritorna quante notifiche ha
// mandato; gli errori vanno nel log.
func MedalsChanged(app core.App, changes []medals.Change) int {
	if len(changes) == 0 || !boardEnabled(app, medalsEnabledKey) {
		return 0
	}
	rec, err := loadRecipients(app)
	if err != nil {
		app.Logger().Error("[leaderboardnotify] destinatari", "err", err)
		return 0
	}

	// Per socio, nell'ordine in cui compaiono.
	var order []string
	byUser := map[string][]medals.Change{}
	for _, c := range changes {
		if _, ok := byUser[c.UserID]; !ok {
			order = append(order, c.UserID)
		}
		byUser[c.UserID] = append(byUser[c.UserID], c)
	}

	sent := 0
	for _, userID := range order {
		if !rec.ok(userID) {
			continue
		}
		list := byUser[userID]
		if len(list) > MedalsMaxSingle {
			sendMedal(app, userID, medalManyTag, map[string]string{"count": strconv.Itoa(len(list))})
			sent++
			continue
		}
		for _, c := range list {
			tag := medalUpgradedTag
			if c.From == medals.TierNone {
				tag = medalEarnedTag
			}
			sendMedal(app, userID, tag, map[string]string{"medal": c.Title, "tier": c.To.Label()})
			sent++
		}
	}
	if sent > 0 {
		app.Logger().Info("[leaderboardnotify] notifiche medaglie", "changes", len(changes), "sent", sent)
	}
	return sent
}

func sendMedal(app core.App, userID, tag string, params map[string]string) {
	title, body := fallback(tag, params)
	send(app, dbtools.PushNotification{
		UserId:        userID,
		TrTag:         tag,
		TrNamedParams: params,
		Title:         title,
		Body:          body,
		Data:          map[string]string{"type": "medals"},
	})
}
