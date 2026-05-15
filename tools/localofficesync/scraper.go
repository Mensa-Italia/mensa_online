package localofficesync

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const (
	regionURLFmt = "https://www.mensa.it/gruppi-locali-referenti/?s_squadra=%s"
	httpTimeout  = 30 * time.Second
)

// roleCodeToKind classifica i codici ruolo cloud32 usati dal sito.
// 001 = Segretario (uno per gruppo locale)
// 002 = Assistente al Test
// 003 = Co-Segretario
var roleCodeToKind = map[string]string{
	"001": "segretario",
	"002": "assistente",
	"003": "cosegretario",
}

// memberRefRE matcha gli anchor verso la scheda socio:
//   /Associazioni/gruppi/170734/{ID}?...&s_ruolo=NNN&s_squadra=NN
// e ne estrae user id (cloud32) + ruolo (001/002/003).
// {ID} e` lo stesso valore usato come `id` in members_registry / users.
var memberRefRE = regexp.MustCompile(`/Associazioni/gruppi/\d+/(\d+)\?[^"']*?s_ruolo=(\d{3})`)

// PersonRef e` un riferimento a una persona estratto dalla pagina della
// regione: id cloud32 + ruolo + codice squadra.
type PersonRef struct {
	UserID      string // = members_registry.id
	Role        string // segretario | cosegretario | assistente
	SquadraCode string // 01..20
	RegionName  string // es. "Abruzzo"
}

// FetchRegion scarica la pagina referenti per un codice squadra e ne estrae
// la lista di persone presenti, con ruolo. Le ripetizioni vengono dedotte
// (utile perche` ogni <a> della scheda compare due volte: foto + bottone).
func FetchRegion(code string) ([]PersonRef, error) {
	url := fmt.Sprintf(regionURLFmt, code)
	client := &http.Client{Timeout: httpTimeout}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	regionName := regionsByCode[code]
	seen := map[string]struct{}{}
	var out []PersonRef
	for _, m := range memberRefRE.FindAllStringSubmatch(string(body), -1) {
		userID := strings.TrimSpace(m[1])
		ruoloCode := m[2]
		kind, ok := roleCodeToKind[ruoloCode]
		if !ok {
			continue
		}
		// Dedup su (userID, ruolo): la stessa persona puo` apparire due volte
		// per la stessa carica (foto + bottone scheda), ma potrebbe anche
		// avere due cariche diverse nello stesso gruppo (raro ma possibile).
		key := userID + "|" + kind
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, PersonRef{
			UserID:      userID,
			Role:        kind,
			SquadraCode: code,
			RegionName:  regionName,
		})
	}
	return out, nil
}
