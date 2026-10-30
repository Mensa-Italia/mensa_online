package leaderboardnotify

import (
	"strings"
	"testing"

	"mensadb/tools/medals"
)

// medalFixture: 500 riceve; 501 non ha timbri, 502 e' nascosto, 503 ha
// spento la classifica, 504 non ha dispositivi, 505 ha spento le
// notifiche generali. Interruttore delle medaglie acceso.
func medalFixture(t *testing.T) *fixture {
	f := newFixture(t)
	f.config(medalsEnabledKey, "true")
	for _, id := range []string{"500", "502", "503", "504", "505"} {
		f.record("stamp_users", "", map[string]any{"user": id, "stamp": "s1"})
	}
	for _, id := range []string{"500", "501", "502", "503", "505"} {
		f.record("users_devices", "", map[string]any{"user": id, "firebase_id": "tok-" + id, "language": "it"})
	}
	f.record("users_metadata", "", map[string]any{"user": "502", "key": hiddenKey, "value": "true"})
	f.record("users_metadata", "", map[string]any{"user": "503", "key": notifyBoardKey, "value": "false"})
	f.record("users_metadata", "", map[string]any{"user": "505", "key": notifyGeneral, "value": "false"})
	return f
}

func TestMedalsChangedTextsAndRecipients(t *testing.T) {
	f := medalFixture(t)

	earned := medals.Change{UserID: "500", Medal: "collector", Title: "Collezionista", From: medals.TierNone, To: medals.TierBronze}
	if n := MedalsChanged(f.app, []medals.Change{earned}); n != 1 {
		t.Fatalf("inviate %d, attesa 1", n)
	}
	n := f.expectOne(f.take(), "500", "medals.notify.earned")
	if n.TrNamedParams["medal"] != "Collezionista" || n.TrNamedParams["tier"] != "Bronzo" || len(n.TrNamedParams) != 2 {
		t.Errorf("parametri: %v", n.TrNamedParams)
	}
	if n.Title != "Nuova medaglia" || n.Body != "Collezionista: Bronzo. È nel tuo medagliere." {
		t.Errorf("testo di riserva: %q / %q", n.Title, n.Body)
	}
	if n.Data["type"] != "medals" || len(n.Data) != 1 {
		t.Errorf("data: %v", n.Data)
	}

	upgraded := medals.Change{UserID: "500", Medal: "steady", Title: "Costante", From: medals.TierSilver, To: medals.TierGold}
	MedalsChanged(f.app, []medals.Change{upgraded})
	n = f.expectOne(f.take(), "500", "medals.notify.upgraded")
	if n.Title != "Medaglia salita di livello" || n.Body != "Costante è ora Oro." {
		t.Errorf("testo di riserva: %q / %q", n.Title, n.Body)
	}

	// Chi non puo' ricevere (nessun timbro, nascosto, classifica spenta,
	// nessun dispositivo, notifiche generali spente) non riceve niente.
	var rest []medals.Change
	for _, id := range []string{"501", "502", "503", "504", "505", "999"} {
		rest = append(rest, medals.Change{UserID: id, Medal: "collector", Title: "Collezionista", From: medals.TierNone, To: medals.TierBronze})
	}
	if n := MedalsChanged(f.app, rest); n != 0 || len(f.take()) != 0 {
		t.Fatalf("destinatari esclusi: inviate %d", n)
	}
}

// Fino a tre medaglie cambiate per socio si avvisano una per una, oltre
// una sola notifica riassuntiva.
func TestMedalsChangedManyCollapses(t *testing.T) {
	f := medalFixture(t)
	titles := []string{"Collezionista", "Costante", "Inarrestabile", "Maratoneta"}
	var changes []medals.Change
	for i, title := range titles {
		changes = append(changes, medals.Change{UserID: "500", Medal: "m" + string(rune('a'+i)), Title: title, From: medals.TierNone, To: medals.TierBronze})
	}

	if n := MedalsChanged(f.app, changes[:MedalsMaxSingle]); n != MedalsMaxSingle {
		t.Fatalf("a %d medaglie: inviate %d", MedalsMaxSingle, n)
	}
	if got := f.take(); len(got) != MedalsMaxSingle {
		t.Fatalf("attese %d notifiche, trovate %d", MedalsMaxSingle, len(got))
	}

	if n := MedalsChanged(f.app, changes); n != 1 {
		t.Fatalf("a 4 medaglie: inviate %d, attesa 1", n)
	}
	n := f.expectOne(f.take(), "500", "medals.notify.many")
	if n.TrNamedParams["count"] != "4" || len(n.TrNamedParams) != 1 {
		t.Errorf("parametri: %v", n.TrNamedParams)
	}
	if n.Title != "Medagliere aggiornato" || n.Body != "4 medaglie nuove o salite di livello." {
		t.Errorf("testo di riserva: %q / %q", n.Title, n.Body)
	}

	// Il conteggio e' per socio: due soci con tre medaglie ciascuno ne
	// ricevono tre a testa, non una riassuntiva.
	f.record("stamp_users", "", map[string]any{"user": "506", "stamp": "s2"})
	f.record("users_devices", "", map[string]any{"user": "506", "firebase_id": "tok-506", "language": "it"})
	var two []medals.Change
	for _, c := range changes[:3] {
		two = append(two, c)
		d := c
		d.UserID = "506"
		two = append(two, d)
	}
	if n := MedalsChanged(f.app, two); n != 6 {
		t.Fatalf("inviate %d, attese 6", n)
	}
}

// Spento in configs (o chiave assente) non si manda niente; non serve
// nessun interruttore delle classifiche.
func TestMedalsChangedSwitch(t *testing.T) {
	change := []medals.Change{{UserID: "500", Medal: "collector", Title: "Collezionista", From: medals.TierNone, To: medals.TierBronze}}

	f := newFixture(t)
	f.record("stamp_users", "", map[string]any{"user": "500", "stamp": "s1"})
	f.record("users_devices", "", map[string]any{"user": "500", "firebase_id": "tok", "language": "it"})
	if n := MedalsChanged(f.app, change); n != 0 || len(f.take()) != 0 {
		t.Fatalf("senza la chiave in configs non si manda: %d", n)
	}

	f.config(medalsEnabledKey, "false")
	if n := MedalsChanged(f.app, change); n != 0 || len(f.take()) != 0 {
		t.Fatalf("a interruttore spento non si manda: %d", n)
	}

	rows, _ := f.app.FindAllRecords("configs")
	for _, r := range rows {
		switch r.GetString("key") {
		case medalsEnabledKey:
			r.Set("value", "true")
		case membersEnabledKey, regionsEnabledKey:
			r.Set("value", "false")
		default:
			continue
		}
		if err := f.app.SaveNoValidate(r); err != nil {
			t.Fatal(err)
		}
	}
	if n := MedalsChanged(f.app, change); n != 1 {
		t.Fatalf("a classifiche spente le medaglie partono lo stesso: %d", n)
	}
	if n := MedalsChanged(f.app, nil); n != 0 {
		t.Fatalf("senza avanzamenti: %d", n)
	}
}

func TestMedalTextsFallback(t *testing.T) {
	for _, tag := range []string{medalEarnedTag, medalUpgradedTag, medalManyTag} {
		title, body := fallback(tag, map[string]string{"medal": "Costante", "tier": "Oro", "count": "5"})
		if title == "" || body == "" || strings.Contains(title+body, "{") {
			t.Errorf("%s: %q / %q", tag, title, body)
		}
	}
}
