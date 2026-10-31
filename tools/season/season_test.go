package season

import (
	"testing"
	"time"
)

func utc(y int, m time.Month, d, h, min, s int) time.Time {
	return time.Date(y, m, d, h, min, s, 0, time.UTC)
}

// Il confine e' la mezzanotte di Roma: il 1 ottobre e' sempre ora legale
// (UTC+2), quindi la stagione nuova comincia alle 22:00 UTC del 30 settembre.
func TestOfBoundaries(t *testing.T) {
	for _, c := range []struct {
		name string
		at   time.Time
		want int
	}{
		{"un secondo prima del confine", utc(2026, time.September, 30, 21, 59, 59), 2025},
		{"sul confine", utc(2026, time.September, 30, 22, 0, 0), 2026},
		{"il 30/9 sera UTC e' gia' ottobre a Roma", utc(2026, time.September, 30, 23, 30, 0), 2026},
		{"1 ottobre a mezzogiorno", utc(2026, time.October, 1, 12, 0, 0), 2026},
		{"capodanno a Roma, ancora UTC dicembre", utc(2026, time.December, 31, 23, 30, 0), 2026},
		{"gennaio", utc(2027, time.January, 15, 10, 0, 0), 2026},
		{"ultimo istante della stagione", utc(2027, time.September, 30, 21, 59, 59), 2026},
		{"stagione dopo", utc(2027, time.September, 30, 22, 0, 0), 2027},
	} {
		if got := Of(c.at); got != c.want {
			t.Errorf("%s: Of(%s) = %d, atteso %d", c.name, c.at.Format(time.RFC3339), got, c.want)
		}
		if got := Current(c.at); got != c.want {
			t.Errorf("%s: Current = %d, atteso %d", c.name, got, c.want)
		}
	}
}

// Anche nei giorni del cambio d'ora la stagione non si sposta, e un
// istante espresso in un altro fuso vale quanto lo stesso istante in UTC.
func TestOfDaylightSavingDays(t *testing.T) {
	rome, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		at   time.Time
		want int
	}{
		// 25/10/2026: si torna all'ora solare alle 03:00 (01:00 UTC).
		{time.Date(2026, time.October, 25, 0, 30, 0, 0, rome), 2026},
		{time.Date(2026, time.October, 25, 3, 30, 0, 0, rome), 2026},
		// 28/3/2027: si passa all'ora legale alle 02:00.
		{time.Date(2027, time.March, 28, 1, 59, 0, 0, rome), 2026},
		{time.Date(2027, time.March, 28, 3, 0, 0, 0, rome), 2026},
		// Il 1 ottobre 2027 a mezzanotte di Roma.
		{time.Date(2027, time.October, 1, 0, 0, 0, 0, rome), 2027},
		{time.Date(2027, time.September, 30, 23, 59, 59, 0, rome), 2026},
		// Stesso istante in un fuso lontano.
		{time.Date(2026, time.September, 30, 15, 59, 59, 0, time.FixedZone("x", -6*3600)), 2025},
		{time.Date(2026, time.September, 30, 16, 0, 0, 0, time.FixedZone("x", -6*3600)), 2026},
	} {
		if got := Of(c.at); got != c.want {
			t.Errorf("Of(%s) = %d, atteso %d", c.at.Format(time.RFC3339), got, c.want)
		}
	}
}

func TestBounds(t *testing.T) {
	start, end := Bounds(2026)
	if want := utc(2026, time.September, 30, 22, 0, 0); !start.Equal(want) || start.Location() != time.UTC {
		t.Errorf("inizio %s, atteso %s UTC", start, want)
	}
	if want := utc(2027, time.September, 30, 22, 0, 0); !end.Equal(want) || end.Location() != time.UTC {
		t.Errorf("fine %s, atteso %s UTC", end, want)
	}
	// Le stagioni si toccano senza buchi ne' sovrapposizioni.
	_, prevEnd := Bounds(2025)
	if !prevEnd.Equal(start) {
		t.Errorf("la 2025 finisce a %s, la 2026 comincia a %s", prevEnd, start)
	}
	// Dentro la finestra, Of da' la stagione; subito fuori, un'altra.
	for s := 2024; s <= 2030; s++ {
		a, b := Bounds(s)
		if Of(a) != s || Of(b.Add(-time.Second)) != s {
			t.Errorf("stagione %d: i suoi estremi non le appartengono", s)
		}
		if Of(a.Add(-time.Second)) != s-1 || Of(b) != s+1 {
			t.Errorf("stagione %d: i vicini non sono quelli giusti", s)
		}
	}
}

func TestLabel(t *testing.T) {
	for s, want := range map[int]string{2025: "2025/26", 2026: "2026/27", 2029: "2029/30", 2099: "2099/00", 2008: "2008/09"} {
		if got := Label(s); got != want {
			t.Errorf("Label(%d) = %q, atteso %q", s, got, want)
		}
	}
}

func TestWindowAndDays(t *testing.T) {
	start, end := Window(2026)
	if start != "2026-09-30 22:00:00" || end != "2027-09-30 22:00:00" {
		t.Errorf("finestra %q / %q", start, end)
	}
	p := Params(2026)
	if p["start"] != start || p["end"] != end {
		t.Errorf("parametri %v", p)
	}
	if LastDay(2026) != "2027-09-30" || FirstDay(2026) != "2026-10-01" {
		t.Errorf("giorni %s / %s", FirstDay(2026), LastDay(2026))
	}
	// Il testo ordina come il tempo anche col formato lungo di PocketBase.
	if "2026-09-30 22:00:00.000Z" < start || "2027-09-30 21:59:59.999Z" >= end || "2027-09-30 22:00:00.000Z" < end {
		t.Error("il confronto come testo non rispetta la finestra semiaperta")
	}
}

// Il giorno si conta a Roma, e l'ultima ora del giorno e' quella che
// precede la mezzanotte di Roma, in estate come in inverno.
func TestRomeDayAndLastHour(t *testing.T) {
	for _, c := range []struct {
		at   time.Time
		day  string
		last bool
	}{
		{utc(2026, time.September, 30, 21, 55, 0), "2026-09-30", true},  // 23:55 a Roma, ora legale
		{utc(2026, time.September, 30, 22, 5, 0), "2026-10-01", false},  // 00:05 del 1 ottobre
		{utc(2026, time.September, 30, 20, 55, 0), "2026-09-30", false}, // 22:55 a Roma
		{utc(2026, time.December, 31, 22, 55, 0), "2026-12-31", true},   // 23:55 a Roma, ora solare
		{utc(2026, time.December, 31, 23, 5, 0), "2027-01-01", false},
	} {
		if got := Day(c.at); got != c.day {
			t.Errorf("Day(%s) = %s, atteso %s", c.at.Format(time.RFC3339), got, c.day)
		}
		if got := LastHour(c.at); got != c.last {
			t.Errorf("LastHour(%s) = %v, atteso %v", c.at.Format(time.RFC3339), got, c.last)
		}
		if Local(c.at).Format("2006-01-02") != c.day {
			t.Errorf("Local(%s)", c.at.Format(time.RFC3339))
		}
	}
}
