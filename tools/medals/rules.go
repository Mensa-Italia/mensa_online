package medals

import (
	"time"
)

// Tier e' il metallo di una medaglia. TierNone vale "non ancora presa".
type Tier int

const (
	TierNone Tier = iota
	TierBronze
	TierSilver
	TierGold
	TierPlatinum
)

// String e' il valore salvato in medals_notify_state.tier.
func (t Tier) String() string {
	switch t {
	case TierBronze:
		return "bronze"
	case TierSilver:
		return "silver"
	case TierGold:
		return "gold"
	case TierPlatinum:
		return "platinum"
	}
	return "none"
}

// Label e' il nome italiano del metallo, come nel medagliere dell'app.
func (t Tier) Label() string {
	switch t {
	case TierBronze:
		return "Bronzo"
	case TierSilver:
		return "Argento"
	case TierGold:
		return "Oro"
	case TierPlatinum:
		return "Platino"
	}
	return ""
}

// ParseTier e' l'inverso di String; un valore sconosciuto vale TierNone.
func ParseTier(s string) Tier {
	for t := TierNone; t <= TierPlatinum; t++ {
		if t.String() == s {
			return t
		}
	}
	return TierNone
}

// Family e' la famiglia di una medaglia: le permanenti restano per sempre,
// le stagionali ripartono da zero il 1 ottobre (tools/season) e di ogni
// stagione chiusa resta il metallo finale (medals_history).
type Family int

const (
	// Permanente: il contatore e' a vita (socio attivo, della prima ora,
	// calendario, multischermo). Non da' punti in classifica.
	Permanente Family = iota
	// Stagionale: il contatore e' quello della stagione in corso.
	Stagionale
)

// String e' il nome della famiglia nei testi e nei log.
func (f Family) String() string {
	if f == Permanente {
		return "permanente"
	}
	return "stagionale"
}

// Stats sono i contatori di una riga di view_medals: gli stessi della
// MedalStatsModel dell'app, piu' quelli che l'app non usa.
type Stats struct {
	AppSince             time.Time
	IsMembershipActive   bool
	StampsTotal          int
	StampsDistinctMonths int
	StampsNight          int
	StampsMorning        int
	StampsHalloween      int
	StampsNewYear        int
	StampsValentine      int
	StampsPiDay          int
	StampsBirthday       int
	StampsFirstDay       int
	StampsLate           int
	StampsMaxSameDay     int
	StampsMaxMonthStreak int
	StampsMaxSeasons     int
	StampsPioneer        int
	StampsDistinctRegion int
	StampsNational       int
	StampsOutOfRegion    int
	EventsCreated        int
	EventsCreatedNat     int
	EventRegistrations   int
	Devices              int
	CalendarLinked       bool
	CalendarRegions      int
}

// Level e' un metallo di una medaglia con il valore del contatore che lo
// da'.
type Level struct {
	Tier      Tier
	Threshold int
}

// Rule e' una medaglia: il contatore da cui si guadagna e la soglia di
// ogni metallo. Una medaglia a livello unico ha solo il platino.
type Rule struct {
	ID     string
	Title  string
	Family Family
	Secret bool
	Levels []Level
	Value  func(s Stats, now time.Time) int
}

// Rules e' la tabella delle medaglie. COPIA di MedalRules.kt dell'app
// (shared/medals): soglie, contatori, famiglie e id devono restare
// identici, perche' l'app mostra il metallo calcolato da quelle regole e il
// server avvisa quando lo stesso calcolo cambia. Se si cambia una soglia o
// si aggiunge una medaglia, si cambia qui E in MedalRules.kt insieme (e nel
// punteggio della classifica soci, tools/leaderboard/sql/soci.sql);
// rules_test.go fissa ogni soglia per far notare una modifica fatta da una
// parte sola. I titoli sono quelli di MedalCatalog (Medal.kt / Medal.swift).
//
// Le soglie delle stagionali sono tarate su una stagione sola (note di
// design della stagione): bronzo = ha giocato, argento = primo quarto, oro =
// primo decimo, platino = i pochissimi in cima.
var Rules = []Rule{
	// Passaporto e francobolli
	tiered("collector", "Collezionista", Stagionale, 1, 3, 6, 10, func(s Stats) int { return s.StampsTotal }),
	tiered("steady", "Costante", Stagionale, 1, 3, 5, 7, func(s Stats) int { return s.StampsDistinctMonths }),
	tiered("unstoppable", "Inarrestabile", Stagionale, 2, 3, 4, 6, func(s Stats) int { return s.StampsMaxMonthStreak }),
	tiered("marathon", "Maratoneta", Stagionale, 2, 3, 4, 5, func(s Stats) int { return s.StampsMaxSameDay }),
	single("four_seasons", "Quattro stagioni", Stagionale, 4, false, func(s Stats) int { return s.StampsMaxSeasons }),
	tiered("pioneer", "Pioniere", Stagionale, 1, 2, 4, 6, func(s Stats) int { return s.StampsPioneer }),

	// Viaggi
	tiered("globetrotter", "Giramondo", Stagionale, 2, 3, 4, 5, func(s Stats) int { return s.StampsDistinctRegion }),
	tiered("traveller", "Trasfertista", Stagionale, 1, 3, 6, 10, func(s Stats) int { return s.StampsOutOfRegion }),
	tiered("national_rally", "Raduno nazionale", Stagionale, 1, 2, 3, 4, func(s Stats) int { return s.StampsNational }),

	// Orari e ricorrenze
	single("night_owl", "Nottambulo", Stagionale, 1, false, func(s Stats) int { return s.StampsNight }),
	single("early_bird", "Mattiniero", Stagionale, 1, false, func(s Stats) int { return s.StampsMorning }),
	single("halloween", "Dolcetto o scherzetto", Stagionale, 1, true, func(s Stats) int { return s.StampsHalloween }),
	single("new_year", "Brindisi di mezzanotte", Stagionale, 1, true, func(s Stats) int { return s.StampsNewYear }),
	single("valentine", "Cuore razionale", Stagionale, 1, true, func(s Stats) int { return s.StampsValentine }),
	single("pi_day", "3,14159…", Stagionale, 1, true, func(s Stats) int { return s.StampsPiDay }),
	single("birthday", "Festeggiato", Stagionale, 1, true, func(s Stats) int { return s.StampsBirthday }),
	single("front_row", "Prima fila", Stagionale, 1, false, func(s Stats) int { return s.StampsFirstDay }),
	single("better_late", "Meglio tardi", Stagionale, 1, false, func(s Stats) int { return s.StampsLate }),

	// Comunita'
	tiered("always_there", "Sempre presente", Stagionale, 1, 3, 6, 10, func(s Stats) int { return s.EventRegistrations }),
	tiered("organizer", "Organizzatore", Stagionale, 1, 3, 8, 15, func(s Stats) int { return s.EventsCreated }),
	single("national_director", "Regista nazionale", Stagionale, 1, false, func(s Stats) int { return s.EventsCreatedNat }),

	// Iscrizione e app: permanenti, contatori a vita
	flag("active_member", "Socio attivo", Permanente, func(s Stats) bool { return s.IsMembershipActive }),
	{
		ID: "early_adopter", Title: "Della prima ora", Family: Permanente,
		Levels: []Level{{TierBronze, 1}, {TierSilver, 2}, {TierGold, 3}, {TierPlatinum, 5}},
		// Anni interi passati dall'iscrizione all'app: l'unica regola che
		// cambia col tempo anche senza che i dati si muovano.
		Value: func(s Stats, now time.Time) int {
			if s.AppSince.IsZero() || s.AppSince.UnixMilli() <= 0 {
				return 0
			}
			return int(now.Sub(s.AppSince).Hours() / 24 / 365)
		},
	},
	flag("up_to_date", "Sempre aggiornato", Permanente, func(s Stats) bool { return s.CalendarLinked }),
	tiered("cartographer", "Cartografo", Permanente, 1, 3, 5, 10, func(s Stats) int { return s.CalendarRegions }),
	single("multiscreen", "Multischermo", Permanente, 2, false, func(s Stats) int { return s.Devices }),
}

func tiered(id, title string, family Family, b, s, g, p int, value func(Stats) int) Rule {
	return Rule{
		ID: id, Title: title, Family: family,
		Levels: []Level{{TierBronze, b}, {TierSilver, s}, {TierGold, g}, {TierPlatinum, p}},
		Value:  func(st Stats, _ time.Time) int { return value(st) },
	}
}

func single(id, title string, family Family, threshold int, secret bool, value func(Stats) int) Rule {
	return Rule{
		ID: id, Title: title, Family: family, Secret: secret,
		Levels: []Level{{TierPlatinum, threshold}},
		Value:  func(st Stats, _ time.Time) int { return value(st) },
	}
}

func flag(id, title string, family Family, value func(Stats) bool) Rule {
	return Rule{
		ID: id, Title: title, Family: family,
		Levels: []Level{{TierPlatinum, 1}},
		Value: func(st Stats, _ time.Time) int {
			if value(st) {
				return 1
			}
			return 0
		},
	}
}

// Tier ritorna il metallo piu' alto preso con i contatori s a quest'ora:
// l'ultimo livello la cui soglia e' raggiunta (un valore negativo vale 0).
func (r Rule) Tier(s Stats, now time.Time) Tier {
	value := r.Value(s, now)
	tier := TierNone
	for _, l := range r.Levels {
		if value >= l.Threshold {
			tier = l.Tier
		}
	}
	return tier
}

// RuleByID cerca una medaglia per id.
func RuleByID(id string) (Rule, bool) {
	for _, r := range Rules {
		if r.ID == id {
			return r, true
		}
	}
	return Rule{}, false
}

// StateSeason e' la stagione con cui si tiene lo stato di una medaglia in
// medals_notify_state e medals_history: quella in corso per le stagionali,
// 0 per le permanenti, che non hanno annate.
func (r Rule) StateSeason(current int) int {
	if r.Family == Permanente {
		return 0
	}
	return current
}
