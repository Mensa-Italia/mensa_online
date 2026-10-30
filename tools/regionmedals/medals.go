package regionmedals

import "math"

// Metalli, dal piu' basso. "none" e' nessun metallo.
const (
	TierNone   = "none"
	TierBronze = "bronze"
	TierSilver = "silver"
	TierGold   = "gold"
)

// Tiers sono i metalli veri, dal piu' basso al piu' alto: l'indice + 1 e'
// il loro rango.
var Tiers = []string{TierBronze, TierSilver, TierGold}

// rank da' un numero ai metalli per confrontarli: 0 nessuno, 1 bronzo,
// 2 argento, 3 oro.
func rank(tier string) int {
	for i, t := range Tiers {
		if t == tier {
			return i + 1
		}
	}
	return 0
}

// tierAt e' il contrario di rank.
func tierAt(rank int) string {
	if rank < 1 || rank > len(Tiers) {
		return TierNone
	}
	return Tiers[rank-1]
}

// TierName ritorna il metallo in italiano, come nei testi delle notifiche.
func TierName(tier string) string {
	switch tier {
	case TierBronze:
		return "Bronzo"
	case TierSilver:
		return "Argento"
	case TierGold:
		return "Oro"
	}
	return ""
}

// levels sono le soglie di una medaglia: il valore da raggiungere per
// bronzo, argento e oro. Per il Podio vale al contrario (posizione al
// massimo uguale alla soglia).
type levels struct {
	Bronze, Silver, Gold float64
}

func (l levels) json() map[string]float64 {
	return map[string]float64{TierBronze: l.Bronze, TierSilver: l.Silver, TierGold: l.Gold}
}

// definition e' una delle dieci medaglie: chiave nella tabella, nome in
// italiano, soglie, minimo di persone sotto il quale non si assegna niente
// (count deve essere almeno minimum), decimali con cui si salva il valore
// e come si ricava dalla riga della query.
type definition struct {
	Key      string
	Name     string
	Levels   levels
	Minimum  float64
	Decimals int
	// Lower: vince il valore piu' basso (la posizione del Podio).
	Lower   bool
	measure func(m *metrics) (value, count, extra float64)
}

// PodiumKey e' la medaglia che decide lo snapshot di fine anno.
const PodiumKey = "podium"

// Medals nell'ordine in cui l'app le mostra. Le soglie e i minimi sono
// quelli della nota di design: le quote sono in quota efficace
// (distinti / (N_r^0,7 * Nmedia^0,3)), il resto sono conteggi.
var Medals = []definition{
	{Key: "present", Name: "Tutti presenti", Levels: levels{5, 10, 15}, Minimum: 3, Decimals: 1,
		measure: func(m *metrics) (float64, float64, float64) { return m.PresentPct, m.PresentMembers, m.PresentOnce }},
	{Key: "host", Name: "Casa aperta", Levels: levels{2, 4, 6}, Minimum: 2,
		measure: func(m *metrics) (float64, float64, float64) { return m.HostRegions, m.HostVisitors, 0 }},
	{Key: "away", Name: "In trasferta", Levels: levels{2, 4, 6},
		measure: func(m *metrics) (float64, float64, float64) { return m.AwayRegions, m.AwayMembers, 0 }},
	{Key: "months", Name: "Senza pause", Levels: levels{6, 9, 12},
		measure: func(m *metrics) (float64, float64, float64) { return m.ActiveMonths, m.RegionalEvents, 0 }},
	{Key: "welcome", Name: "Benvenuto", Levels: levels{25, 40, 60}, Minimum: 3,
		measure: func(m *metrics) (float64, float64, float64) {
			return m.NewcomersPct, m.NewcomersStamped, m.Newcomers
		}},
	{Key: "fullhouse", Name: "Sala piena", Levels: levels{8.5, 15, 25}, Minimum: 5, Decimals: 1,
		measure: func(m *metrics) (float64, float64, float64) { return m.BestEventPct, m.BestEventClaimers, 0 }},
	{Key: "core", Name: "Zoccolo duro", Levels: levels{8.5, 15, 25}, Minimum: 5, Decimals: 1,
		measure: func(m *metrics) (float64, float64, float64) { return m.CorePct, m.CorePersons, m.CoreEffective }},
	{Key: "national", Name: "Padroni di casa", Levels: levels{1, 2, 3},
		measure: func(m *metrics) (float64, float64, float64) { return m.NationalHosted, 0, 0 }},
	{Key: "relay", Name: "Staffetta", Levels: levels{2, 3, 4},
		measure: func(m *metrics) (float64, float64, float64) { return m.Organizers, m.RegionalEvents, 0 }},
	{Key: PodiumKey, Name: "Podio", Levels: levels{3, 2, 1}, Lower: true,
		measure: func(m *metrics) (float64, float64, float64) { return m.PodiumPosition, m.PodiumScore, 0 }},
}

// MedalName ritorna il nome italiano di una medaglia, o la chiave stessa
// se non la conosce.
func MedalName(key string) string {
	for _, d := range Medals {
		if d.Key == key {
			return d.Name
		}
	}
	return key
}

// tierFor ritorna il metallo che un valore raggiunge, senza guardare il
// minimo. Per le medaglie "Lower" il valore e' una posizione: 0 vuol dire
// fuori classifica.
func (d definition) tierFor(value float64) string {
	if d.Lower {
		switch {
		case value <= 0:
			return TierNone
		case value <= d.Levels.Gold:
			return TierGold
		case value <= d.Levels.Silver:
			return TierSilver
		case value <= d.Levels.Bronze:
			return TierBronze
		}
		return TierNone
	}
	switch {
	case value >= d.Levels.Gold:
		return TierGold
	case value >= d.Levels.Silver:
		return TierSilver
	case value >= d.Levels.Bronze:
		return TierBronze
	}
	return TierNone
}

// store arrotonda per difetto un valore ai decimali con cui si salva. Il
// metallo si decide sul valore salvato, non su quello grezzo: cosi' chi
// legge la riga e confronta il numero con le soglie ottiene lo stesso
// metallo (un 14,96% non diventa "15,0" e quindi oro per un arrotondamento).
func (d definition) store(value float64) float64 {
	p := math.Pow10(d.Decimals)
	return math.Floor(value*p+1e-9) / p
}

// metrics e' una riga della query (sql/medals.sql), una per sede.
type metrics struct {
	ID        string `db:"id"`
	Name      string `db:"name"`
	Region    string `db:"region"`
	RegionKey string `db:"region_key"`

	MembersActive float64 `db:"members_active"`

	PresentMembers float64 `db:"present_members"`
	PresentOnce    float64 `db:"present_once"`
	PresentPct     float64 `db:"present_pct"`

	HostRegions  float64 `db:"host_regions"`
	HostVisitors float64 `db:"host_visitors"`

	AwayRegions float64 `db:"away_regions"`
	AwayMembers float64 `db:"away_members"`

	ActiveMonths float64 `db:"active_months"`

	Newcomers        float64 `db:"newcomers"`
	NewcomersStamped float64 `db:"newcomers_stamped"`
	NewcomersPct     float64 `db:"newcomers_pct"`

	BestEventClaimers float64 `db:"best_event_claimers"`
	BestEventPct      float64 `db:"best_event_pct"`

	CorePersons   float64 `db:"core_persons"`
	CoreEffective float64 `db:"core_effective"`
	CorePct       float64 `db:"core_pct"`

	NationalHosted float64 `db:"national_hosted"`

	Organizers     float64 `db:"organizers"`
	RegionalEvents float64 `db:"regional_events"`

	PodiumPosition float64 `db:"podium_position"`
	PodiumScore    float64 `db:"podium_score"`
}
