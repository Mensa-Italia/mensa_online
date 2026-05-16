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
	regionURLFmt  = "https://www.mensa.it/gruppi-locali-referenti/?s_squadra=%s"
	profileURLFmt = "https://www.cloud32.it/Associazioni/gruppi/170734/%s?s_citta=&s_prov=&s_ruolo=%s&s_squadra=%s"
	httpTimeout   = 30 * time.Second
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
// e ne estrae cloud32 socio code + ruolo (001/002/003).
// NOTA: questo {ID} NON coincide con members_registry.id; serve solo a
// costruire l'URL del profilo da cui estrarre l'email @mensa.it.
var memberRefRE = regexp.MustCompile(`/Associazioni/gruppi/\d+/(\d+)\?[^"']*?s_ruolo=(\d{3})`)

// mensaAliasRE estrae l'email @mensa.it dal profilo cloud32 del socio.
var mensaAliasRE = regexp.MustCompile(`(?i)[a-z0-9._%+-]+@mensa\.it`)

// PersonRef e` un riferimento a una persona estratto dalla pagina della
// regione. SocioCode e` il codice della scheda cloud32 (NON l'id PB), usato
// solo per fare un secondo hop e tirare giu` l'alias @mensa.it. Il vero
// matching con i nostri users avviene tramite alias_mail su members_registry.
type PersonRef struct {
	SocioCode    string // codice scheda cloud32 (per costruire l'URL profilo)
	RoleCode     string // 001/002/003 (serve a costruire l'URL profilo)
	Role         string // segretario | cosegretario | assistente
	SquadraCode  string // 01..20
	RegionName   string // es. "Abruzzo"
	MensaAlias   string // popolato da FetchAlias (es. "ester.belfatto@mensa.it")
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
		socioCode := strings.TrimSpace(m[1])
		ruoloCode := m[2]
		kind, ok := roleCodeToKind[ruoloCode]
		if !ok {
			continue
		}
		// Dedup su (socioCode, ruolo): ogni riga sulla pagina compare due
		// volte (foto + bottone), ed e` lecito che la stessa persona occupi
		// due cariche diverse → un record per ognuna.
		key := socioCode + "|" + kind
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, PersonRef{
			SocioCode:   socioCode,
			RoleCode:    ruoloCode,
			Role:        kind,
			SquadraCode: code,
			RegionName:  regionName,
		})
	}
	return out, nil
}

// FetchAlias scarica la scheda socio cloud32 (pubblica, no login) ed estrae
// l'email @mensa.it. Ritorna "" se la pagina non espone alcun alias.
func FetchAlias(socioCode, roleCode, squadraCode string) (string, error) {
	url := fmt.Sprintf(profileURLFmt, socioCode, roleCode, squadraCode)
	client := &http.Client{Timeout: httpTimeout}
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("GET %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if m := mensaAliasRE.FindString(string(body)); m != "" {
		return strings.ToLower(m), nil
	}
	return "", nil
}
