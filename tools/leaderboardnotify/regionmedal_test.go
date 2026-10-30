package leaderboardnotify

import (
	"strings"
	"testing"
)

// regionFixture: una regione con quattro soci attivi (uno puo' ricevere,
// gli altri no per motivi diversi), uno non attivo, e un socio di un'altra
// regione. Gli interruttori partono accesi.
func regionFixture(t *testing.T) *fixture {
	f := newFixture(t)
	f.config(regionMedalsEnabledKey, "true")

	// 500 riceve: attivo, timbro, dispositivo.
	// 501 non ha timbri, 502 e' nascosto, 503 ha spento la classifica,
	// 504 non ha dispositivi, 505 non e' piu' attivo, 600 e' di un'altra regione.
	for _, id := range []string{"500", "501", "502", "503", "504", "505"} {
		f.record("members_registry", id, map[string]any{"area": "Regione A", "is_active": id != "505"})
	}
	f.record("members_registry", "600", map[string]any{"area": "Regione B", "is_active": true})
	for _, id := range []string{"500", "502", "503", "504", "505", "600"} {
		f.record("stamp_users", "", map[string]any{"user": id, "stamp": "s1"})
	}
	for _, id := range []string{"500", "501", "502", "503", "505", "600"} {
		f.record("users_devices", "", map[string]any{"user": id, "firebase_id": "tok-" + id, "language": "it"})
	}
	f.record("users_metadata", "", map[string]any{"user": "502", "key": hiddenKey, "value": "true"})
	f.record("users_metadata", "", map[string]any{"user": "503", "key": notifyBoardKey, "value": "false"})
	return f
}

func TestRegionMedalUnlockedRecipientsAndText(t *testing.T) {
	f := regionFixture(t)

	if n := RegionMedalUnlocked(f.app, "loA", "Regione A", "host", "silver"); n != 1 {
		t.Fatalf("inviate %d, attesa 1", n)
	}
	sent := f.take()
	n := f.expectOne(sent, "500", "leaderboard.notify.region_medal")
	if n.TrNamedParams["medal"] != "Casa aperta" || n.TrNamedParams["tier"] != "Argento" {
		t.Errorf("parametri: %v", n.TrNamedParams)
	}
	if n.Title != "La tua regione ha un nuovo traguardo" || n.Body != "Casa aperta: Argento. Guardalo nel medagliere." {
		t.Errorf("testo di riserva: %q / %q", n.Title, n.Body)
	}
	if n.Data["type"] != "leaderboard" || n.Data["board"] != "region_medals" || len(n.Data) != 2 {
		t.Errorf("data: %v", n.Data)
	}
	if strings.Contains(n.Title+n.Body, "Regione") {
		t.Errorf("il testo non nomina regioni: %q %q", n.Title, n.Body)
	}

	// Un'altra regione avvisa solo i suoi.
	RegionMedalUnlocked(f.app, "loB", "Regione B", "podium", "gold")
	f.expectOne(f.take(), "600", "leaderboard.notify.region_medal")
}

func TestRegionMedalUnlockedSwitches(t *testing.T) {
	f := regionFixture(t)

	// Interruttore delle medaglie spento (o mancante): niente.
	rows, _ := f.app.FindAllRecords("configs")
	for _, r := range rows {
		if r.GetString("key") == regionMedalsEnabledKey {
			r.Set("value", "false")
			if err := f.app.SaveNoValidate(r); err != nil {
				t.Fatal(err)
			}
		}
	}
	if n := RegionMedalUnlocked(f.app, "loA", "Regione A", "host", "bronze"); n != 0 || len(f.take()) != 0 {
		t.Fatalf("a interruttore spento non si manda (inviate %d)", n)
	}

	// Medaglie accese ma classifica regioni spenta: niente.
	for _, r := range rows {
		switch r.GetString("key") {
		case regionMedalsEnabledKey:
			r.Set("value", "true")
		case regionsEnabledKey:
			r.Set("value", "false")
		default:
			continue
		}
		if err := f.app.SaveNoValidate(r); err != nil {
			t.Fatal(err)
		}
	}
	if n := RegionMedalUnlocked(f.app, "loA", "Regione A", "host", "bronze"); n != 0 || len(f.take()) != 0 {
		t.Fatalf("con la classifica regioni spenta non si manda (inviate %d)", n)
	}

	// Entrambi accesi: parte.
	for _, r := range rows {
		if r.GetString("key") == regionsEnabledKey {
			r.Set("value", "true")
			if err := f.app.SaveNoValidate(r); err != nil {
				t.Fatal(err)
			}
		}
	}
	if n := RegionMedalUnlocked(f.app, "loA", "Regione A", "host", "bronze"); n != 1 {
		t.Fatalf("inviate %d, attesa 1", n)
	}
}

// La chiave di configs e' documentata e spenta se manca.
func TestRegionMedalUnlockedOffWithoutConfig(t *testing.T) {
	f := newFixture(t)
	f.record("members_registry", "500", map[string]any{"area": "Regione A", "is_active": true})
	f.record("stamp_users", "", map[string]any{"user": "500", "stamp": "s1"})
	f.record("users_devices", "", map[string]any{"user": "500", "firebase_id": "tok", "language": "it"})
	if n := RegionMedalUnlocked(f.app, "loA", "Regione A", "host", "gold"); n != 0 {
		t.Fatalf("senza la chiave in configs non si manda: %d", n)
	}
}
