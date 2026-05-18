package dbtools

import (
	"slices"
	"strings"
	"sync"

	"mensadb/importers"

	"github.com/pocketbase/pocketbase/core"
)

// Alias mail Plesk @mensa.it che identificano i ruoli del Consiglio
// Nazionale. Per ognuno seguiamo la chain di forwarding via
// importers.RetrieveForwardedMail: se l'alias punta a una sola email
// finale, quella persona ottiene il potere "super". Se punta a piu`
// indirizzi (alias condiviso / distribution list) l'assegnazione viene
// saltata per quell'alias, per evitare di promuovere chi non e` davvero
// titolare del ruolo.
var boardAliases = []string{
	"consigliere",
	"comunicazione",
	"tesoriere",
	"segretario",
	"sviluppo",
}

// alwaysSuperUserID e` il socio che deve avere il potere "super"
// indipendentemente da qualsiasi alias mail (override hard-coded).
const alwaysSuperUserID = "5366"

// Mutex separato da quello di RefreshUserStatesManagersPowers cosi`
// possiamo girarli anche in parallelo senza farsi attendere a vicenda.
var lockSuperPowers sync.Mutex

// RefreshUserSuperPowers riassegna il potere "super" basandosi sui
// forward Plesk degli alias di consiglio. Side effects:
//   - aggiunge "super" a chi appare come destinazione unica di uno
//     degli alias in boardAliases;
//   - rimuove "super" da chi non e` piu` titolare di nessun ruolo;
//   - garantisce sempre "super" per alwaysSuperUserID;
//   - se i powers cambiano, ruota il tokenKey del record cosi` i
//     token PB nativi scadono (= disconnessione forzata). I bearer
//     Zitadel non hanno bisogno di rotazione: il middleware risolve
//     i powers al volo dal record ad ogni request.
func RefreshUserSuperPowers(app core.App) {
	if !lockSuperPowers.TryLock() {
		return
	}
	defer lockSuperPowers.Unlock()

	// Email finali autorizzate a "super", in un set per O(1) lookup.
	authorized := map[string]struct{}{}
	for _, alias := range boardAliases {
		dests := uniqueLowerStrings(importers.RetrieveForwardedMail(alias))
		if len(dests) != 1 {
			// Salta: 0 destinazioni (alias non configurato) o >1
			// (distribution list / shared). Vogliamo evitare di
			// promuovere chi non e` univocamente quel ruolo.
			app.Logger().Info("[super] alias non univoco, skip",
				"alias", alias, "destinations", len(dests))
			continue
		}
		authorized[dests[0]] = struct{}{}
	}

	records, err := app.FindRecordsByFilter("users", "id != ''", "-created", -1, 0)
	if err != nil {
		app.Logger().Error("[super] list users failed", "err", err)
		return
	}

	for _, record := range records {
		email := normalizeEmail(record.GetString("email"))
		shouldHaveSuper := record.Id == alwaysSuperUserID
		if !shouldHaveSuper {
			_, ok := authorized[email]
			shouldHaveSuper = ok
		}

		powers := record.GetStringSlice("powers")
		hadSuper := slices.Contains(powers, "super")
		if shouldHaveSuper == hadSuper {
			continue
		}

		newPowers := make([]string, 0, len(powers)+1)
		for _, p := range powers {
			if p == "super" {
				continue
			}
			newPowers = append(newPowers, p)
		}
		if shouldHaveSuper {
			newPowers = append(newPowers, "super")
		}
		record.Set("powers", newPowers)
		// Forza il logout: la rotazione del tokenKey invalida tutti i
		// token PB nativi gia` emessi. I bearer Zitadel restano firmati
		// validi (scadono al loro exp), ma le powers verranno comunque
		// rilette dal middleware al prossimo request, quindi l'effetto
		// e` immediato.
		record.RefreshTokenKey()
		if err := app.Save(record); err != nil {
			app.Logger().Error("[super] save user failed", "id", record.Id, "err", err)
			continue
		}
		app.Logger().Info("[super] powers updated",
			"user", record.Id, "email", email,
			"super_added", shouldHaveSuper, "super_removed", !shouldHaveSuper)
	}
}

func uniqueLowerStrings(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		k := normalizeEmail(s)
		if k == "" {
			continue
		}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, k)
	}
	return out
}

func normalizeEmail(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
