package officestats

import (
	"slices"
	"testing"
	"time"
)

func TestWindowMonthsSpanTwoSeasons(t *testing.T) {
	now := time.Date(2026, time.November, 8, 15, 0, 0, 0, time.UTC) // stagione 2026
	got := windowMonths(2026, now)
	if len(got) != 14 || got[0] != "2025-10" || got[len(got)-1] != "2026-11" {
		t.Fatalf("mesi: %v", got)
	}
	if !slices.IsSorted(got) {
		t.Errorf("mesi non in ordine: %v", got)
	}
	// l'ultimo mese della stagione: 24 mesi, non uno di piu'
	last := windowMonths(2026, time.Date(2027, time.September, 30, 20, 0, 0, 0, time.UTC))
	if len(last) != 24 || last[23] != "2027-09" {
		t.Errorf("fine stagione: %d mesi, ultimo %s", len(last), last[len(last)-1])
	}
}

func TestSeasonOfMonth(t *testing.T) {
	cases := map[string]string{"2025-10": "2025/26", "2026-09": "2025/26", "2026-10": "2026/27", "x": ""}
	for month, want := range cases {
		if got := seasonOfMonth(month); got != want {
			t.Errorf("%s: %q, atteso %q", month, got, want)
		}
	}
}
