package leaderboardnotify

import (
	_ "embed"
	"encoding/json"
	"strings"
	"sync"
)

// I testi italiani delle notifiche, chiave Tolgee -> testo, con i
// segnaposto {position}, {score}, {gained}, {lost}, {event}, {date}, {medal}, {tier}. Sono
// la riserva di dbtools.PushNotification (Title/Body) quando Tolgee non ha
// ancora la chiave, e il sorgente da caricare su Tolgee. Il file sta qui
// perche' ci siano un posto solo in cui cambiare una frase.
//
//go:embed texts.it.json
var textsJSON []byte

var (
	textsOnce sync.Once
	texts     map[string]string
)

// Variants e' quante frasi ci sono per ogni caso del quindicinale
// (leaderboard.notify.up.1 ... up.8 e cosi' via).
const Variants = 8

func loadTexts() {
	textsOnce.Do(func() {
		texts = map[string]string{}
		_ = json.Unmarshal(textsJSON, &texts)
	})
}

// fallback ritorna titolo e corpo italiani di una chiave, con i
// segnaposto sostituiti.
func fallback(tag string, params map[string]string) (string, string) {
	loadTexts()
	fill := func(s string) string {
		for k, v := range params {
			s = strings.ReplaceAll(s, "{"+k+"}", v)
		}
		return s
	}
	return fill(texts[tag+".title"]), fill(texts[tag+".body"])
}
