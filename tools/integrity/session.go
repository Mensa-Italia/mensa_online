package integrity

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"
)

const (
	// HeaderSession porta la sessione di integrita` Android: il server la
	// manda in risposta a una prova Play Integrity riuscita, il client la
	// rimanda al posto di challenge + token finche' non scade.
	HeaderSession = "X-Integrity-Session"
	// HeaderSessionTTL accompagna HeaderSession in risposta: secondi di vita.
	HeaderSessionTTL = "X-Integrity-Session-Ttl"

	// SessionTTL e` la vita di una sessione di integrita`.
	//
	// Perche' esiste: Google limita le decodeIntegrityToken del progetto (27
	// al minuto di default) e una schermata dell'app parte con 10-20
	// richieste in parallelo. Con una decode per richiesta bastava un
	// telefono "watched" per esaurire la quota di tutti. Con la sessione si
	// fa una decode ogni SessionTTL, e la prova per-richiesta resta solo su
	// SensitivePath, cioe` su timbri e medaglie.
	//
	// Cosa costa: per SessionTTL la sessione vale come prova su tutto il
	// resto, e chi la estrae dall'app la puo` riusare. Estrarla vuol dire
	// leggere il traffico TLS dell'app su un dispositivo che passa
	// MEETS_DEVICE_INTEGRITY, e la stessa sessione non apre comunque timbri e
	// medaglie.
	SessionTTL = 10 * time.Minute

	sessionBytes = 32

	// DecodesPerMinutePerUser e` quante decodeIntegrityToken puo` far fare un
	// singolo utente in un minuto. Con le sessioni all'app ne servono poche;
	// senza tetto un client (o chi manda token a caso con challenge valide)
	// potrebbe bruciare da solo la quota del progetto.
	DecodesPerMinutePerUser = 8
)

// SensitivePath dice se (method, path) tocca i dati di timbri e medaglie, o
// apre sottoscrizioni realtime (che possono portarli): li` la sessione non
// basta e serve sempre la prova per-richiesta.
func SensitivePath(method, p string) bool {
	if path.Clean(p) != p {
		return true // il router lo normalizza: nel dubbio, sensibile
	}
	if strings.ToUpper(method) == http.MethodPost && p == "/api/realtime" {
		return true
	}
	for _, c := range []string{"stamp", "stamp_users", "view_medals"} {
		prefix := "/api/collections/" + c + "/"
		if strings.HasPrefix(p, prefix) || p == strings.TrimSuffix(prefix, "/") {
			return true
		}
	}
	return false
}

// SessionStore conserva in memoria le sessioni di integrita` emesse, legate
// all'utente. In memoria per lo stesso motivo delle challenge: una sessione
// persa al riavvio costa un 428 e una prova completa.
type SessionStore struct {
	mu   sync.Mutex
	byID map[string]session
	now  func() time.Time
	ttl  time.Duration
}

type session struct {
	userID  string
	expires time.Time
}

// NewSessionStore crea uno store con TTL di default.
func NewSessionStore() *SessionStore {
	return &SessionStore{byID: make(map[string]session), now: time.Now, ttl: SessionTTL}
}

// Issue emette una sessione nuova per l'utente. Le scadute si buttano qui.
func (s *SessionStore) Issue(userID string) (string, time.Duration, error) {
	buf := make([]byte, sessionBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", 0, err
	}
	id := base64.RawURLEncoding.EncodeToString(buf)

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for k, v := range s.byID {
		if !now.Before(v.expires) {
			delete(s.byID, k)
		}
	}
	s.byID[id] = session{userID: userID, expires: now.Add(s.ttl)}
	return id, s.ttl, nil
}

// Remember segna come verificato, per la durata di una sessione, il token di
// login con cui e` arrivata una prova Play Integrity valida (vedi AuthKey).
func (s *SessionStore) Remember(userID, key string) {
	if userID == "" || key == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID[key] = session{userID: userID, expires: s.now().Add(s.ttl)}
}

// AuthKey e` la chiave con cui SessionStore ricorda un token di login:
// l'hash dell'header Authorization, mai il token in chiaro. Vuota se
// l'header manca.
//
// Perche' il token e non l'utente: chi e` "watched" puo` passare la prova una
// volta con l'app vera e poi usare uno script. Lo script pero` fa login per
// conto suo e ha un token diverso; per riusare quello dell'app dovrebbe
// leggerne il traffico TLS su un dispositivo integro, cioe` lo stesso lavoro
// che servirebbe per rubare X-Integrity-Session. Serve alle app gia`
// installate (22.2.18), che mandano la prova completa a ogni richiesta e non
// conoscono la sessione: con il token verificato il server non chiede a
// Google di decodificarla.
func AuthKey(authorization string) string {
	a := strings.TrimSpace(authorization)
	if a == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(a))
	return "auth:" + base64.RawURLEncoding.EncodeToString(sum[:])
}

// Valid dice se la sessione e` stata emessa a quell'utente e non e` scaduta.
func (s *SessionStore) Valid(userID, id string) bool {
	if userID == "" || id == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.byID[id]
	if !ok {
		return false
	}
	if !s.now().Before(v.expires) {
		delete(s.byID, id)
		return false
	}
	return v.userID == userID
}

// DecodeLimiter conta le decodeIntegrityToken per utente, a finestre di un
// minuto.
type DecodeLimiter struct {
	mu    sync.Mutex
	byUsr map[string]window
	now   func() time.Time
	max   int
}

type window struct {
	start time.Time
	count int
}

// NewDecodeLimiter crea un limitatore con il tetto di default.
func NewDecodeLimiter() *DecodeLimiter {
	return &DecodeLimiter{byUsr: make(map[string]window), now: time.Now, max: DecodesPerMinutePerUser}
}

// Allow dice se l'utente puo` fare un'altra decode adesso, e se si' la conta.
func (l *DecodeLimiter) Allow(userID string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	w := l.byUsr[userID]
	if now.Sub(w.start) >= time.Minute {
		w = window{start: now}
	}
	if w.count >= l.max {
		return false
	}
	w.count++
	l.byUsr[userID] = w
	return true
}
