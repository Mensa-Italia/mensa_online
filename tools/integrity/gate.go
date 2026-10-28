package integrity

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

// Header della prova per-richiesta (contratto §5).
const (
	HeaderPlatform  = "X-Integrity-Platform"
	HeaderChallenge = "X-Integrity-Challenge"
	HeaderToken     = "X-Integrity-Token"
	HeaderKeyID     = "X-Integrity-Key-Id"
	HeaderAssertion = "X-Integrity-Assertion"

	PlatformAndroid = "android"
	PlatformIOS     = "ios"
)

// Level e` users.access_level.
type Level string

const (
	LevelFree    Level = "free"
	LevelWatched Level = "watched"
	LevelBlocked Level = "blocked"
)

// LevelOf legge il livello d'accesso da un record utente. Vuoto, assente (la
// migrazione non e` ancora girata) o sconosciuto valgono "free".
func LevelOf(rec interface{ GetString(string) string }) Level {
	if rec == nil {
		return LevelFree
	}
	switch Level(rec.GetString("access_level")) {
	case LevelWatched:
		return LevelWatched
	case LevelBlocked:
		return LevelBlocked
	}
	return LevelFree
}

// IsMCP dice se il path e` quello del server MCP.
func IsMCP(p string) bool { return p == "/mcp" || strings.HasPrefix(p, "/mcp/") }

// InAPIScope dice se il path e` sotto /api/ (o e` /api).
func InAPIScope(p string) bool { return p == "/api" || strings.HasPrefix(p, "/api/") }

// Exempt elenca le richieste di un utente "watched" che NON richiedono la
// prova: servono a far funzionare il flusso stesso (challenge e attest) o non
// passano dal client HTTP che sa allegarla (immagini dei loader nativi, apertura
// dell'SSE, health check). Le sottoscrizioni realtime (POST /api/realtime)
// restano invece protette.
//
// Un path non "pulito" (con "..", "//" o "/./") non e` mai esente: il router
// lo normalizza prima di scegliere la route, quindi un'esenzione concessa sul
// testo grezzo potrebbe coprire una route diversa.
func Exempt(method, p string) bool {
	method = strings.ToUpper(method)
	if method == http.MethodOptions {
		return true // preflight CORS: non porta mai auth
	}
	if path.Clean(p) != p {
		return false
	}
	read := method == http.MethodGet || method == http.MethodHead
	switch {
	case method == http.MethodPost && (p == "/api/integrity/challenge" || p == "/api/integrity/ios/attest"):
		return true
	case read && strings.HasPrefix(p, "/api/files/"):
		return true
	case read && (p == "/api/realtime" || p == "/api/health"):
		return true
	}
	return false
}

// ExternalAppRoute dice se il path e` una route chiamata da app esterne con
// una chiave di ex_keys (hooks.CheckKey): /api/cs/exapp/* (tranne
// export/decide, che invece e` dell'utente autenticato) e le route
// members-hashed / members-snapshots. Queste route NON usano un utente
// autenticato: la chiave sta nell'header Authorization e l'utente su cui si
// agisce arriva come member_id / attendee_user_id nel body, quindi e.Auth e`
// nil e ne' "blocked" ne' "watched" li riguardano.
//
// Si riconoscono per PATH e non guardando che il bearer inizi per "sk_",
// perche' CheckKey accetta la chiave anche senza il prefisso "Bearer ", e le
// route members-snapshots anche in query (?authKey=): l'header non e` un
// segnale affidabile, il path si'. Non apre niente: gli handler rivalidano
// comunque la chiave e rispondono 401 a un token utente.
func ExternalAppRoute(p string) bool {
	if path.Clean(p) != p {
		return false
	}
	switch p {
	case "/api/cs/exapp/request",
		"/api/cs/exapp/valid",
		"/api/cs/exapp/store-user-ticket",
		"/api/cs/exapp/delete-user-ticket",
		"/api/cs/exapp/export/request",
		"/api/cs/exapp/export/fetch",
		"/api/cs/members-hashed",
		"/api/cs/members-snapshots":
		return true
	}
	return strings.HasPrefix(p, "/api/cs/members-snapshots/")
}

// RequiresProof dice se una richiesta di un utente "watched" a (method, path)
// richiede una prova per-richiesta: sta sotto /api, non e` esente e non e` una
// route di app esterne. /mcp non rientra: per quello non c'e` prova possibile,
// la richiesta e` rifiutata a prescindere (vedi Authorize).
func RequiresProof(method, p string) bool {
	return InAPIScope(p) && !Exempt(method, p) && !ExternalAppRoute(p)
}

// Subject e` l'utente autenticato che fa la richiesta.
type Subject struct {
	UserID string
	Level  Level
}

// Request e` cio` che serve a decidere: metodo, path (r.URL.Path) e header.
type Request struct {
	Method string
	Path   string
	Query  url.Values // query string (serve all'allowlist dei blocked)
	Header http.Header
}

// Gate decide se una richiesta autenticata passa.
//
// Android o IOS nil significano "non configurato": la verifica di quella
// piattaforma fallisce CHIUSA (mai aperta) con il motivo nel log.
//
// Sessions nil spegne le sessioni Android (ogni richiesta vuole la prova
// completa); Decodes nil toglie il tetto di decode per utente.
type Gate struct {
	Challenges *ChallengeStore
	Sessions   *SessionStore
	Decodes    *DecodeLimiter
	Android    *AndroidVerifier
	IOS        *AppAttest
}

// Grant e` cio` che una richiesta passata fa tornare al client: una sessione
// di integrita` nuova, se la richiesta ha portato una prova Play Integrity
// completa e valida.
type Grant struct {
	Session string
	TTL     time.Duration
}

// Authorize ritorna nil se la richiesta puo` proseguire, altrimenti il
// rifiuto da restituire. Tabella delle decisioni:
//
//	route app esterne (ex_keys) -> sempre ok, vedi ExternalAppRoute
//	free     -> sempre ok
//	blocked  -> 403 account_blocked, qualsiasi richiesta autenticata TRANNE
//	            l'allowlist di blocked_allowlist.go
//	watched  -> /mcp: 403 integrity_failed
//	            fuori da /api e richieste esenti: ok
//	            altrimenti serve una prova valida (428 / 403)
func (g *Gate) Authorize(ctx context.Context, sub Subject, req Request) *Rejection {
	_, rej := g.Check(ctx, sub, req)
	return rej
}

// Check e` Authorize piu` l'eventuale sessione da mandare al client.
func (g *Gate) Check(ctx context.Context, sub Subject, req Request) (Grant, *Rejection) {
	// Le route delle app esterne si autenticano con la loro chiave, non con
	// un utente: passano sempre, qualunque sia il livello di chi (non) c'e`.
	if ExternalAppRoute(req.Path) {
		return Grant{}, nil
	}

	switch sub.Level {
	case LevelBlocked:
		// Allowlist minima (blocked_allowlist.go): serve a far finire il flusso
		// di conferma delle app esterne a un socio sospeso.
		if BlockedAllowed(sub.UserID, req.Method, req.Path, req.Query) {
			return Grant{}, nil
		}
		return Grant{}, Blocked("utente con access_level=blocked")
	case LevelWatched:
	default:
		return Grant{}, nil
	}

	// /mcp usa un token OAuth proprio e un client (modello, non l'app) che non
	// puo` produrre una prova: per un utente sotto osservazione non c'e` un
	// modo valido di arrivarci. Si risponde 403 integrity_failed e non 428
	// perche` il 428 dice "riprova con una prova", e qui riprovare e` inutile.
	if IsMCP(req.Path) {
		return Grant{}, Failed("/mcp non disponibile per utenti watched (nessuna prova possibile)")
	}
	if !InAPIScope(req.Path) || Exempt(req.Method, req.Path) {
		return Grant{}, nil
	}
	return g.verifyProof(ctx, sub, req)
}

func (g *Gate) verifyProof(ctx context.Context, sub Subject, req Request) (Grant, *Rejection) {
	platform := strings.ToLower(strings.TrimSpace(req.Header.Get(HeaderPlatform)))
	authKey := AuthKey(req.Header.Get("Authorization"))

	// Token di login gia` verificato con Play Integrity (vedi AuthKey): passa
	// senza guardare la prova, che le app 22.2.18 mandano comunque. Non su
	// timbri, medaglie e realtime.
	if g.Sessions != nil && authKey != "" && !SensitivePath(req.Method, req.Path) &&
		g.Sessions.Valid(sub.UserID, authKey) {
		return Grant{}, nil
	}

	// Sessione Android: vale al posto di challenge + token, ma mai su timbri,
	// medaglie e sottoscrizioni realtime (vedi SessionTTL). Una sessione
	// scaduta, sconosciuta o usata dove non vale e` un 428: il client la butta
	// e rifa` la prova completa.
	if s := strings.TrimSpace(req.Header.Get(HeaderSession)); s != "" {
		switch {
		case platform != PlatformAndroid:
			return Grant{}, required("sessione su una piattaforma diversa da android")
		case SensitivePath(req.Method, req.Path):
			return Grant{}, required("sessione non valida su timbri, medaglie e realtime")
		case g.Sessions == nil || !g.Sessions.Valid(sub.UserID, s):
			return Grant{}, required("sessione sconosciuta, scaduta o di un altro utente")
		}
		return Grant{}, nil
	}

	rej := g.verifyFullProof(ctx, sub, req, platform)
	if rej != nil || platform != PlatformAndroid || g.Sessions == nil {
		return Grant{}, rej
	}
	g.Sessions.Remember(sub.UserID, authKey)
	id, ttl, err := g.Sessions.Issue(sub.UserID)
	if err != nil {
		return Grant{}, nil // la richiesta resta buona: alla prossima si rifa` la prova
	}
	return Grant{Session: id, TTL: ttl}, nil
}

func (g *Gate) verifyFullProof(ctx context.Context, sub Subject, req Request, platform string) *Rejection {
	challenge := strings.TrimSpace(req.Header.Get(HeaderChallenge))
	if platform == "" || challenge == "" {
		return required("prova assente (header X-Integrity-Platform / X-Integrity-Challenge)")
	}

	// La challenge si consuma SEMPRE, anche se la verifica poi fallisce: una
	// prova scartata non deve poter essere ritentata con la stessa challenge.
	if g.Challenges == nil || !g.Challenges.Consume(sub.UserID, challenge) {
		return required("challenge sconosciuta, scaduta, gia` usata o di un altro utente")
	}

	clientData := ClientData(challenge, req.Method, req.Path)

	switch platform {
	case PlatformAndroid:
		token := strings.TrimSpace(req.Header.Get(HeaderToken))
		if token == "" {
			return required("token Play Integrity assente")
		}
		if g.Android == nil {
			return Failed("Play Integrity non configurato (PLAY_INTEGRITY_*)")
		}
		if g.Decodes != nil && !g.Decodes.Allow(sub.UserID) {
			return Failed("troppe decode Play Integrity per questo utente nell'ultimo minuto")
		}
		if err := g.Android.Verify(ctx, token, RequestHash(clientData)); err != nil {
			return Failed("Play Integrity: " + err.Error())
		}
		return nil

	case PlatformIOS:
		keyID := strings.TrimSpace(req.Header.Get(HeaderKeyID))
		if keyID == "" {
			return attestKeyRequired("key id assente")
		}
		assertionB64 := strings.TrimSpace(req.Header.Get(HeaderAssertion))
		if assertionB64 == "" {
			return required("asserzione App Attest assente")
		}
		if g.IOS == nil || g.IOS.Keys == nil {
			return Failed("App Attest non configurato")
		}
		assertion, err := base64.StdEncoding.DecodeString(assertionB64)
		if err != nil {
			return Failed("asserzione non in base64: " + err.Error())
		}
		err = g.IOS.VerifyAssertion(sub.UserID, keyID, assertion, ClientDataHash(clientData))
		switch {
		case err == nil:
			return nil
		case errors.Is(err, ErrKeyNotFound):
			return attestKeyRequired("key id sconosciuto o di un altro utente")
		default:
			return Failed("App Attest: " + err.Error())
		}
	}
	return Failed("piattaforma sconosciuta: " + platform)
}
