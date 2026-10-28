package integrity

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"
)

const (
	// ChallengeTTL e` la vita di una challenge: abbastanza per far girare
	// Play Integrity / App Attest sul dispositivo (secondi), troppo poco per
	// accumulare prove da spendere piu` tardi.
	ChallengeTTL = 120 * time.Second

	// challengeBytes sono i byte casuali della challenge (256 bit).
	challengeBytes = 32

	// maxChallengesPerUser limita le challenge pendenti di un utente. Le
	// challenge stanno in memoria: senza tetto, un client che chiama
	// /api/integrity/challenge in loop gonfierebbe la mappa fino al TTL.
	// Superato il tetto si butta la piu` vecchia.
	maxChallengesPerUser = 64
)

// ChallengeStore conserva in memoria le challenge emesse, legate all'utente
// che le ha chieste. Ogni challenge si puo` consumare una volta sola.
//
// E` volutamente in-memory: il backend gira in singola istanza (come gia` il
// resto dello stato volatile: indice Bleve, cache). Se un giorno diventano
// piu` istanze serve uno store condiviso dietro la stessa interfaccia, ma
// una challenge persa al riavvio costa solo un 428 e un secondo tentativo.
type ChallengeStore struct {
	mu    sync.Mutex
	byUsr map[string]map[string]time.Time // userID -> challenge -> scadenza
	now   func() time.Time
	ttl   time.Duration
}

// NewChallengeStore crea uno store con TTL di default.
func NewChallengeStore() *ChallengeStore {
	return &ChallengeStore{
		byUsr: make(map[string]map[string]time.Time),
		now:   time.Now,
		ttl:   ChallengeTTL,
	}
}

// Issue genera una challenge nuova per l'utente: 32 byte casuali in base64url
// senza padding.
func (s *ChallengeStore) Issue(userID string) (string, error) {
	buf := make([]byte, challengeBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	challenge := base64.RawURLEncoding.EncodeToString(buf)

	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	pending := s.byUsr[userID]
	if pending == nil {
		pending = make(map[string]time.Time)
		s.byUsr[userID] = pending
	}
	if len(pending) >= maxChallengesPerUser {
		evictOldest(pending, now)
	}
	pending[challenge] = now.Add(s.ttl)
	return challenge, nil
}

// Consume invalida la challenge e dice se era valida: emessa a quell'utente,
// non scaduta, mai usata. Una challenge scaduta viene comunque rimossa. Una
// challenge di un ALTRO utente non viene toccata: altrimenti chi ne
// indovina una potrebbe bruciarla a chi l'ha richiesta.
func (s *ChallengeStore) Consume(userID, challenge string) bool {
	if userID == "" || challenge == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	pending := s.byUsr[userID]
	expires, ok := pending[challenge]
	if !ok {
		return false
	}
	delete(pending, challenge)
	if len(pending) == 0 {
		delete(s.byUsr, userID)
	}
	return s.now().Before(expires)
}

// Cleanup toglie le challenge scadute e ritorna quante ne ha rimosse.
func (s *ChallengeStore) Cleanup() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	removed := 0
	for userID, pending := range s.byUsr {
		for challenge, expires := range pending {
			if !now.Before(expires) {
				delete(pending, challenge)
				removed++
			}
		}
		if len(pending) == 0 {
			delete(s.byUsr, userID)
		}
	}
	return removed
}

// Len ritorna il numero di challenge pendenti (anche scadute non ancora
// ripulite). Serve ai test.
func (s *ChallengeStore) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, pending := range s.byUsr {
		n += len(pending)
	}
	return n
}

// StartCleanup lancia la pulizia periodica e ritorna la funzione che la
// ferma. Senza, le challenge mai consumate resterebbero in memoria per sempre
// (la scadenza viene controllata solo in Consume).
func (s *ChallengeStore) StartCleanup(every time.Duration) (stop func()) {
	done := make(chan struct{})
	var once sync.Once
	go func() {
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				s.Cleanup()
			case <-done:
				return
			}
		}
	}()
	return func() { once.Do(func() { close(done) }) }
}

// evictOldest scarta dalla mappa le scadute e, se non basta, la challenge con
// la scadenza piu` vicina (cioe` la piu` vecchia: il TTL e` uguale per tutte).
func evictOldest(pending map[string]time.Time, now time.Time) {
	var oldest string
	var oldestExp time.Time
	for c, exp := range pending {
		if !now.Before(exp) {
			delete(pending, c)
			continue
		}
		if oldest == "" || exp.Before(oldestExp) {
			oldest, oldestExp = c, exp
		}
	}
	if len(pending) >= maxChallengesPerUser && oldest != "" {
		delete(pending, oldest)
	}
}
