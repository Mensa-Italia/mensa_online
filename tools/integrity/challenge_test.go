package integrity

import (
	"encoding/base64"
	"sync"
	"testing"
	"time"
)

func newTestStore(now *time.Time) *ChallengeStore {
	s := NewChallengeStore()
	s.now = func() time.Time { return *now }
	return s
}

// La challenge e` base64url senza padding di 32 byte, e due emissioni non
// possono coincidere.
func TestChallengeFormat(t *testing.T) {
	s := NewChallengeStore()
	a, err := s.Issue("u1")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := s.Issue("u1")
	if a == b {
		t.Fatal("due challenge uguali")
	}
	raw, err := base64.RawURLEncoding.DecodeString(a)
	if err != nil || len(raw) != 32 {
		t.Fatalf("challenge %q non e` base64url di 32 byte (err=%v, len=%d)", a, err, len(raw))
	}
}

// Monouso: la seconda Consume della stessa challenge fallisce.
func TestChallengeSingleUse(t *testing.T) {
	s := NewChallengeStore()
	c, _ := s.Issue("u1")
	if !s.Consume("u1", c) {
		t.Fatal("la prima Consume deve riuscire")
	}
	if s.Consume("u1", c) {
		t.Fatal("la seconda Consume deve fallire")
	}
}

// Legata all'utente: un altro utente non la consuma, e il tentativo non la
// brucia al legittimo proprietario.
func TestChallengeBoundToUser(t *testing.T) {
	s := NewChallengeStore()
	c, _ := s.Issue("u1")
	if s.Consume("u2", c) {
		t.Fatal("un altro utente non deve poter consumare la challenge")
	}
	if !s.Consume("u1", c) {
		t.Fatal("il proprietario deve ancora poterla consumare")
	}
}

func TestChallengeUnknownAndEmpty(t *testing.T) {
	s := NewChallengeStore()
	if s.Consume("u1", "") || s.Consume("", "x") || s.Consume("u1", "mai-emessa") {
		t.Fatal("challenge vuote o sconosciute non devono passare")
	}
}

// TTL 120 s: valida fino a un istante prima, scaduta (e rimossa) dopo.
func TestChallengeExpiry(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	s := newTestStore(&now)

	ok, _ := s.Issue("u1")
	late, _ := s.Issue("u1")

	now = now.Add(ChallengeTTL - time.Second)
	if !s.Consume("u1", ok) {
		t.Fatal("a 119 s la challenge e` ancora valida")
	}
	now = now.Add(2 * time.Second)
	if s.Consume("u1", late) {
		t.Fatal("oltre 120 s la challenge e` scaduta")
	}
	if s.Len() != 0 {
		t.Fatalf("la scaduta consumata va rimossa, restano %d", s.Len())
	}
}

func TestChallengeCleanup(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	s := newTestStore(&now)
	_, _ = s.Issue("u1")
	_, _ = s.Issue("u2")
	now = now.Add(time.Minute)
	fresh, _ := s.Issue("u1")

	now = now.Add(ChallengeTTL - time.Minute + time.Second) // le prime due scadono, la terza no
	if n := s.Cleanup(); n != 2 {
		t.Fatalf("Cleanup ha rimosso %d, attese 2", n)
	}
	if s.Len() != 1 || !s.Consume("u1", fresh) {
		t.Fatal("la challenge ancora valida deve sopravvivere alla pulizia")
	}
}

// Tetto per utente: oltre maxChallengesPerUser si scarta la piu` vecchia.
func TestChallengePerUserCap(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	s := newTestStore(&now)

	first, _ := s.Issue("u1")
	for i := 0; i < maxChallengesPerUser; i++ {
		now = now.Add(time.Millisecond)
		if _, err := s.Issue("u1"); err != nil {
			t.Fatal(err)
		}
	}
	if s.Len() != maxChallengesPerUser {
		t.Fatalf("pendenti %d, attese %d", s.Len(), maxChallengesPerUser)
	}
	if s.Consume("u1", first) {
		t.Fatal("la piu` vecchia doveva essere scartata")
	}
}

func TestChallengeConcurrent(t *testing.T) {
	s := NewChallengeStore()
	var wg sync.WaitGroup
	for u := 0; u < 8; u++ {
		wg.Add(1)
		go func(user string) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				c, err := s.Issue(user)
				if err != nil {
					t.Error(err)
					return
				}
				if !s.Consume(user, c) {
					t.Error("consume fallita")
					return
				}
				s.Cleanup()
			}
		}(string(rune('a' + u)))
	}
	wg.Wait()
	if s.Len() != 0 {
		t.Fatalf("restano %d challenge", s.Len())
	}
}

// Una stessa challenge consumata da molte goroutine riesce una volta sola.
func TestChallengeConsumeRace(t *testing.T) {
	s := NewChallengeStore()
	c, _ := s.Issue("u1")
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if s.Consume("u1", c) {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("la challenge e` stata consumata %d volte", wins)
	}
}

func TestChallengeStartCleanupStops(t *testing.T) {
	s := NewChallengeStore()
	stop := s.StartCleanup(time.Millisecond)
	stop()
	stop() // idempotente
}
