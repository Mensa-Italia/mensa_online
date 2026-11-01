package birthdaynotify

import (
	"testing"
	"time"
)

func at(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestBirthdayReadsTheDayInRome(t *testing.T) {
	now := at(t, "2026-11-01T08:00:00Z")
	if age, ok := birthday(at(t, "1990-10-31T23:00:00Z"), now); !ok || age != 36 {
		t.Fatalf("mezzanotte di Roma salvata in UTC: got %d %v", age, ok)
	}
	if _, ok := birthday(at(t, "1990-11-01T23:00:00Z"), now); ok {
		t.Fatal("il 2 novembre a Roma non e` oggi")
	}
}

func TestLeapDayOnFebruary28(t *testing.T) {
	born := at(t, "2000-02-28T23:00:00Z") // 29 febbraio a Roma
	if _, ok := birthday(born, at(t, "2027-02-28T09:00:00Z")); !ok {
		t.Fatal("anno non bisestile: si festeggia il 28")
	}
	if _, ok := birthday(born, at(t, "2028-02-28T09:00:00Z")); ok {
		t.Fatal("anno bisestile: il 28 non e` il compleanno")
	}
}

func TestImplausibleAgeIsHidden(t *testing.T) {
	if age, ok := birthday(at(t, "1899-12-31T23:00:00Z"), at(t, "2026-01-01T09:00:00Z")); !ok || age != 0 {
		t.Fatalf("got %d %v", age, ok)
	}
}

func TestMessage(t *testing.T) {
	m := message("u1", "MATTEO SIPIONE", 36)
	if m.TrNamedParams["name"] != "Matteo" || m.TrNamedParams["age"] != "36" || m.Data["type"] != "birthday" {
		t.Fatalf("%+v", m)
	}
	if _, ok := message("u1", "Anna", 0).TrNamedParams["age"]; ok {
		t.Fatal("senza eta` credibile niente parametro age")
	}
}
