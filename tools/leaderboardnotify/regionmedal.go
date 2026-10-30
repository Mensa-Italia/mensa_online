package leaderboardnotify

import (
	"mensadb/tools/dbtools"
	"mensadb/tools/regionmedals"

	"github.com/pocketbase/pocketbase/core"
)

// regionMedalTag e' la chiave Tolgee della notifica: {medal} e' il nome
// italiano della medaglia, {tier} il metallo (Bronzo, Argento, Oro).
const regionMedalTag = "leaderboard.notify.region_medal"

// RegionMedalUnlocked avvisa i soci di una regione che ha preso un metallo
// nuovo in una medaglia (regionmedals.Unlock). Destinatari: i soci attivi
// del registro la cui area porta a regionKey, che passano ok() come per
// ogni altra notifica della classifica (zero timbri mai, nascosti, senza
// dispositivo o con le notifiche spente no).
//
// Parte solo con leaderboard_notify_region_medals_enabled e
// leaderboard_regions_enabled entrambi a "true". Chi chiama non deve
// passare le righe appena create: i metalli gia' presi al primo ricalcolo
// non sono sblocchi. Ritorna quanti ne ha avvisati. Gli errori vanno nel log.
func RegionMedalUnlocked(app core.App, officeID, regionKey, medal, tier string) int {
	if !boardEnabled(app, regionMedalsEnabledKey) || !boardEnabled(app, regionsEnabledKey) {
		return 0
	}
	members, err := regionmedals.ActiveMembers(app)
	if err != nil {
		app.Logger().Error("[leaderboardnotify] soci della regione", "office", officeID, "err", err)
		return 0
	}
	rec, err := loadRecipients(app)
	if err != nil {
		app.Logger().Error("[leaderboardnotify] destinatari", "err", err)
		return 0
	}
	params := map[string]string{"medal": regionmedals.MedalName(medal), "tier": regionmedals.TierName(tier)}
	title, body := fallback(regionMedalTag, params)
	sent := 0
	for _, userID := range members[regionKey] {
		if !rec.ok(userID) {
			continue
		}
		send(app, dbtools.PushNotification{
			UserId:        userID,
			TrTag:         regionMedalTag,
			TrNamedParams: params,
			Title:         title,
			Body:          body,
			Data:          map[string]string{"type": "leaderboard", "board": "region_medals"},
		})
		sent++
	}
	if sent > 0 {
		app.Logger().Info("[leaderboardnotify] notifiche medaglia di regione", "office", officeID, "medal", medal, "tier", tier, "sent", sent)
	}
	return sent
}
