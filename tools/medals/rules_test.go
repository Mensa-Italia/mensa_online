package medals

import (
	"testing"
	"time"
)

// Le soglie di ogni medaglia, fissate qui apposta: sono la copia di
// MedalRules.kt dell'app, e una modifica fatta da una parte sola deve far
// fallire questo test. Se cambia una soglia (o nasce una medaglia) si
// cambia MedalRules.kt, soci.sql e questa tabella insieme.
var pinnedRules = []struct {
	id     string
	title  string
	secret bool
	// Soglie di bronzo, argento, oro, platino; a livello unico solo il platino.
	levels []int
}{
	{"collector", "Collezionista", false, []int{1, 25, 100, 250}},
	{"steady", "Costante", false, []int{3, 6, 12, 24}},
	{"unstoppable", "Inarrestabile", false, []int{3, 6, 12, 24}},
	{"marathon", "Maratoneta", false, []int{2, 3, 5, 8}},
	{"four_seasons", "Quattro stagioni", false, []int{4}},
	{"pioneer", "Pioniere", false, []int{1, 5, 15, 30}},
	{"globetrotter", "Giramondo", false, []int{2, 5, 10, 20}},
	{"traveller", "Trasfertista", false, []int{1, 5, 15, 30}},
	{"national_rally", "Raduno nazionale", false, []int{1, 3, 5, 10}},
	{"night_owl", "Nottambulo", false, []int{1}},
	{"early_bird", "Mattiniero", false, []int{1}},
	{"halloween", "Dolcetto o scherzetto", true, []int{1}},
	{"new_year", "Brindisi di mezzanotte", true, []int{1}},
	{"valentine", "Cuore razionale", true, []int{1}},
	{"pi_day", "3,14159…", true, []int{1}},
	{"birthday", "Festeggiato", true, []int{1}},
	{"front_row", "Prima fila", false, []int{1}},
	{"better_late", "Meglio tardi", false, []int{1}},
	{"always_there", "Sempre presente", false, []int{1, 10, 25, 50}},
	{"organizer", "Organizzatore", false, []int{1, 5, 15, 30}},
	{"national_director", "Regista nazionale", false, []int{1}},
	{"active_member", "Socio attivo", false, []int{1}},
	{"early_adopter", "Della prima ora", false, []int{1, 2, 3, 5}},
	{"up_to_date", "Sempre aggiornato", false, []int{1}},
	{"cartographer", "Cartografo", false, []int{1, 3, 5, 10}},
	{"multiscreen", "Multischermo", false, []int{2}},
}

// setCounter porta a n il contatore da cui si guadagna la medaglia id.
var setCounter = map[string]func(s *Stats, n int, now time.Time){
	"collector":         func(s *Stats, n int, _ time.Time) { s.StampsTotal = n },
	"steady":            func(s *Stats, n int, _ time.Time) { s.StampsDistinctMonths = n },
	"unstoppable":       func(s *Stats, n int, _ time.Time) { s.StampsMaxMonthStreak = n },
	"marathon":          func(s *Stats, n int, _ time.Time) { s.StampsMaxSameDay = n },
	"four_seasons":      func(s *Stats, n int, _ time.Time) { s.StampsMaxSeasons = n },
	"pioneer":           func(s *Stats, n int, _ time.Time) { s.StampsPioneer = n },
	"globetrotter":      func(s *Stats, n int, _ time.Time) { s.StampsDistinctRegion = n },
	"traveller":         func(s *Stats, n int, _ time.Time) { s.StampsOutOfRegion = n },
	"national_rally":    func(s *Stats, n int, _ time.Time) { s.StampsNational = n },
	"night_owl":         func(s *Stats, n int, _ time.Time) { s.StampsNight = n },
	"early_bird":        func(s *Stats, n int, _ time.Time) { s.StampsMorning = n },
	"halloween":         func(s *Stats, n int, _ time.Time) { s.StampsHalloween = n },
	"new_year":          func(s *Stats, n int, _ time.Time) { s.StampsNewYear = n },
	"valentine":         func(s *Stats, n int, _ time.Time) { s.StampsValentine = n },
	"pi_day":            func(s *Stats, n int, _ time.Time) { s.StampsPiDay = n },
	"birthday":          func(s *Stats, n int, _ time.Time) { s.StampsBirthday = n },
	"front_row":         func(s *Stats, n int, _ time.Time) { s.StampsFirstDay = n },
	"better_late":       func(s *Stats, n int, _ time.Time) { s.StampsLate = n },
	"always_there":      func(s *Stats, n int, _ time.Time) { s.EventRegistrations = n },
	"organizer":         func(s *Stats, n int, _ time.Time) { s.EventsCreated = n },
	"national_director": func(s *Stats, n int, _ time.Time) { s.EventsCreatedNat = n },
	"active_member":     func(s *Stats, n int, _ time.Time) { s.IsMembershipActive = n >= 1 },
	// n anni interi passati dall'iscrizione all'app (piu' qualche giorno).
	"early_adopter": func(s *Stats, n int, now time.Time) { s.AppSince = now.AddDate(0, 0, -(n*365 + 10)) },
	"up_to_date":    func(s *Stats, n int, _ time.Time) { s.CalendarLinked = n >= 1 },
	"cartographer":  func(s *Stats, n int, _ time.Time) { s.CalendarRegions = n },
	"multiscreen":   func(s *Stats, n int, _ time.Time) { s.Devices = n },
}

func TestRulesArePinned(t *testing.T) {
	if len(Rules) != len(pinnedRules) {
		t.Fatalf("%d regole, attese %d: aggiornare MedalRules.kt e questa tabella insieme", len(Rules), len(pinnedRules))
	}
	for i, want := range pinnedRules {
		got := Rules[i]
		if got.ID != want.id || got.Title != want.title || got.Secret != want.secret {
			t.Errorf("regola %d: %s/%q/%v, attesa %s/%q/%v", i, got.ID, got.Title, got.Secret, want.id, want.title, want.secret)
		}
		if len(got.Levels) != len(want.levels) {
			t.Errorf("%s: %d livelli, attesi %d", want.id, len(got.Levels), len(want.levels))
			continue
		}
		tiers := []Tier{TierBronze, TierSilver, TierGold, TierPlatinum}
		if len(want.levels) == 1 {
			tiers = []Tier{TierPlatinum}
		}
		for j, threshold := range want.levels {
			if got.Levels[j].Threshold != threshold || got.Levels[j].Tier != tiers[j] {
				t.Errorf("%s livello %d: %v/%d, atteso %v/%d", want.id, j, got.Levels[j].Tier, got.Levels[j].Threshold, tiers[j], threshold)
			}
		}
		if _, ok := setCounter[want.id]; !ok {
			t.Errorf("%s: manca il contatore nel test", want.id)
		}
	}
}

// Ogni soglia di ogni medaglia: un valore sotto non da' il metallo, quello
// alla soglia si'.
func TestRuleThresholds(t *testing.T) {
	now := time.Date(2026, time.October, 31, 12, 0, 0, 0, time.UTC)
	for _, rule := range Rules {
		set := setCounter[rule.ID]
		var empty Stats
		if got := rule.Tier(empty, now); got != TierNone {
			t.Errorf("%s: a contatori vuoti %v, atteso none", rule.ID, got)
		}
		previous := TierNone
		for _, level := range rule.Levels {
			if level.Threshold > 1 || rule.ID == "early_adopter" {
				var below Stats
				set(&below, level.Threshold-1, now)
				if got := rule.Tier(below, now); got != previous {
					t.Errorf("%s a %d: %v, atteso %v", rule.ID, level.Threshold-1, got, previous)
				}
			}
			var at Stats
			set(&at, level.Threshold, now)
			if got := rule.Tier(at, now); got != level.Tier {
				t.Errorf("%s a %d: %v, atteso %v", rule.ID, level.Threshold, got, level.Tier)
			}
			previous = level.Tier
		}
		// Oltre l'ultima soglia il metallo non cambia.
		last := rule.Levels[len(rule.Levels)-1]
		var above Stats
		set(&above, last.Threshold+1, now)
		if got := rule.Tier(above, now); got != last.Tier {
			t.Errorf("%s oltre la soglia: %v, atteso %v", rule.ID, got, last.Tier)
		}
	}
}

// La medaglia "Della prima ora" e' l'unica che cambia col tempo: anni
// interi dall'iscrizione, e una data mancante (o all'epoca zero) vale zero.
func TestEarlyAdopterUsesTime(t *testing.T) {
	rule, ok := RuleByID("early_adopter")
	if !ok {
		t.Fatal("manca early_adopter")
	}
	since := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	s := Stats{AppSince: since}
	for _, c := range []struct {
		now  time.Time
		want Tier
	}{
		{since.AddDate(0, 0, 364), TierNone},
		{since.AddDate(0, 0, 365), TierBronze},
		{since.AddDate(0, 0, 2*365), TierSilver},
		{since.AddDate(0, 0, 3*365), TierGold},
		{since.AddDate(0, 0, 5*365-1), TierGold},
		{since.AddDate(0, 0, 5*365), TierPlatinum},
		{since.AddDate(-1, 0, 0), TierNone}, // iscritto "nel futuro"
	} {
		if got := rule.Tier(s, c.now); got != c.want {
			t.Errorf("a %s: %v, atteso %v", c.now.Format("2006-01-02"), got, c.want)
		}
	}
	if got := rule.Tier(Stats{}, time.Now()); got != TierNone {
		t.Errorf("senza data: %v", got)
	}
	if got := rule.Tier(Stats{AppSince: time.UnixMilli(0)}, time.Now()); got != TierNone {
		t.Errorf("all'epoca zero: %v", got)
	}
}

func TestTierNames(t *testing.T) {
	for _, c := range []struct {
		t     Tier
		s, l  string
		order int
	}{
		{TierNone, "none", "", 0}, {TierBronze, "bronze", "Bronzo", 1}, {TierSilver, "silver", "Argento", 2},
		{TierGold, "gold", "Oro", 3}, {TierPlatinum, "platinum", "Platino", 4},
	} {
		if c.t.String() != c.s || c.t.Label() != c.l || int(c.t) != c.order || ParseTier(c.s) != c.t {
			t.Errorf("%v: %q/%q", c.t, c.t.String(), c.t.Label())
		}
	}
	if ParseTier("boh") != TierNone {
		t.Error("valore sconosciuto")
	}
}
