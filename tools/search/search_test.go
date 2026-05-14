package search

import (
	"path/filepath"
	"testing"
	"time"
)

func setup(t *testing.T) func() {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "idx")
	if err := Init(dir); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return func() {
		if err := Shutdown(); err != nil {
			t.Fatalf("Shutdown: %v", err)
		}
	}
}

func TestUpsertAndQuery(t *testing.T) {
	defer setup(t)()

	now := time.Now()
	docs := []Doc{
		{ID: "e1", Type: "event", Title: "Carbonara serata", Body: "Cena con carbonara", Region: "Lazio", CreatedAt: now},
		{ID: "d1", Type: "deal", Title: "Trattoria Carbonara", Body: "Sconto trattoria", Region: "Lazio", CreatedAt: now},
		{ID: "s1", Type: "sig", Title: "Fotografia", Body: "Gruppo di fotografia", Region: "Lazio", CreatedAt: now},
	}
	for _, d := range docs {
		if err := Upsert(d); err != nil {
			t.Fatalf("Upsert %s: %v", d.ID, err)
		}
	}

	got, err := Query("carbonara", Filters{}, 10)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 hits, got %d: %+v", len(got), got)
	}

	got, err = Query("carbonara", Filters{Types: []string{"event"}}, 10)
	if err != nil {
		t.Fatalf("Query filtered: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 filtered hit, got %d: %+v", len(got), got)
	}
	if got[0].Type != "event" {
		t.Fatalf("expected type event, got %s", got[0].Type)
	}
}

func TestDelete(t *testing.T) {
	defer setup(t)()

	d := Doc{ID: "x1", Type: "event", Title: "Pizza party", Body: "festa", CreatedAt: time.Now()}
	if err := Upsert(d); err != nil {
		t.Fatal(err)
	}
	if err := Delete("x1"); err != nil {
		t.Fatal(err)
	}
	got, err := Query("pizza", Filters{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0, got %d", len(got))
	}
}

func TestBoost(t *testing.T) {
	defer setup(t)()

	now := time.Now()
	titleDoc := Doc{ID: "t1", Type: "event", Title: "Luna piena", Body: "serata generica", CreatedAt: now}
	bodyDoc := Doc{ID: "b1", Type: "event", Title: "Evento", Body: "osservazione della luna", CreatedAt: now}
	if err := Upsert(titleDoc); err != nil {
		t.Fatal(err)
	}
	if err := Upsert(bodyDoc); err != nil {
		t.Fatal(err)
	}

	got, err := Query("luna", Filters{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 hits, got %d", len(got))
	}
	if got[0].ID != "t1" {
		t.Fatalf("expected title doc t1 to rank first, got %+v", got)
	}
}

func TestFilterRegion(t *testing.T) {
	defer setup(t)()

	now := time.Now()
	a := Doc{ID: "a", Type: "event", Title: "Concerto", Region: "Lazio", CreatedAt: now}
	b := Doc{ID: "b", Type: "event", Title: "Concerto", Region: "Lombardia", CreatedAt: now}
	if err := Upsert(a); err != nil {
		t.Fatal(err)
	}
	if err := Upsert(b); err != nil {
		t.Fatal(err)
	}

	got, err := Query("concerto", Filters{Region: "Lazio"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1, got %d: %+v", len(got), got)
	}
	if got[0].ID != "a" {
		t.Fatalf("expected id a, got %s", got[0].ID)
	}
}
