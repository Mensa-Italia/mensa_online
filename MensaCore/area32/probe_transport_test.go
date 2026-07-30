package area32

import (
	"crypto/tls"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
)

// TestProbeCloud32Transport misura il livello di trasporto verso cloud32.it con
// lo stesso stack dello scraper (resty + cookiejar standard).
//
// Esiste per una ragione precisa: gli aggiornamenti del toolchain Go hanno
// storicamente rotto lo scraping di Area32, e la causa non e` mai il parsing
// HTML — e` sotto. Go ha inasprito piu` volte il parsing dei Set-Cookie, la
// versione minima di TLS accettata dal client e la negoziazione HTTP/2. Se il
// jar scarta il cookie di sessione, la login "funziona" (200) ma la richiesta
// successiva a /home torna anonima, e il sintomo si vede solo a valle.
//
// La sonda non usa credenziali: verifica solo che la pagina di login sia
// raggiungibile, che i cookie di sessione sopravvivano al jar e che il token
// CSRF sia estraibile. Questo e` il pezzo che il cambio di Go puo` rompere.
//
// Fa rete, quindi gira solo con AREA32_PROBE=1 e mai in -short. In CI non
// viene eseguita: i workflow lanciano go build e go vet, non go test.
func TestProbeCloud32Transport(t *testing.T) {
	if os.Getenv("AREA32_PROBE") != "1" {
		t.Skip("sonda di rete: impostare AREA32_PROBE=1 per eseguirla")
	}
	if testing.Short() {
		t.Skip("sonda di rete non compatibile con -short")
	}

	const loginURL = "https://www.cloud32.it/Associazioni/utenti/login?codass=170734"

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}

	client := resty.New().
		SetTimeout(30 * time.Second).
		SetCookieJar(jar).
		SetDoNotParseResponse(true)

	resp, err := client.R().Get(loginURL)
	if err != nil {
		t.Fatalf("GET pagina di login non riuscita: %v", err)
	}

	// Letti direttamente da RawResponse e non dentro un hook OnAfterResponse:
	// con SetDoNotParseResponse(true) — che e` la configurazione dello scraper —
	// quell'hook non arriva a popolarli e i valori restano vuoti.
	var (
		tlsVersion uint16
		tlsCipher  uint16
		certSigAlg string
		negotiated string
	)
	if raw := resp.RawResponse; raw != nil {
		negotiated = raw.Proto
		if cs := raw.TLS; cs != nil {
			tlsVersion, tlsCipher = cs.Version, cs.CipherSuite
			if len(cs.PeerCertificates) > 0 {
				certSigAlg = cs.PeerCertificates[0].SignatureAlgorithm.String()
			}
		}
	}
	body, err := io.ReadAll(resp.RawBody())
	if err != nil {
		t.Fatalf("lettura body: %v", err)
	}
	_ = resp.RawBody().Close()

	t.Logf("stato HTTP:        %d", resp.StatusCode())
	t.Logf("protocollo:        %s", negotiated)
	t.Logf("TLS:               %s / cipher 0x%04x", tlsVersionName(tlsVersion), tlsCipher)
	t.Logf("firma certificato: %s", certSigAlg)
	t.Logf("dimensione body:   %d byte", len(body))

	setCookies := resp.Header().Values("Set-Cookie")
	t.Logf("Set-Cookie ricevuti: %d", len(setCookies))
	for _, sc := range setCookies {
		name := sc
		if i := strings.Index(sc, "="); i > 0 {
			name = sc[:i]
		}
		t.Logf("  header: %s", name)
	}

	// Il controllo che conta: quanti di quei cookie il jar accetta e
	// rimanderebbe. Se Go inasprisce il parsing, e` qui che si perdono.
	parsed, err := url.Parse(loginURL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	kept := jar.Cookies(parsed)
	keptNames := make([]string, 0, len(kept))
	for _, c := range kept {
		keptNames = append(keptNames, c.Name)
	}
	t.Logf("cookie trattenuti dal jar: %d %v", len(kept), keptNames)

	homeURL, err := url.Parse("https://www.cloud32.it/Associazioni/utenti/home")
	if err != nil {
		t.Fatalf("parse home url: %v", err)
	}
	t.Logf("cookie che il jar manderebbe a /home: %d", len(jar.Cookies(homeURL)))

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("goquery: %v", err)
	}
	var token string
	doc.Find("input").Each(func(_ int, s *goquery.Selection) {
		if name, _ := s.Attr("name"); name == "_token" {
			token, _ = s.Attr("value")
		}
	})
	t.Logf("token CSRF _token: presente=%v len=%d", token != "", len(token))

	// Asserzioni: sono le precondizioni senza le quali la login non puo`
	// funzionare, indipendentemente dalle credenziali.
	if resp.StatusCode() != http.StatusOK {
		t.Errorf("stato = %d, atteso 200", resp.StatusCode())
	}
	if len(kept) == 0 {
		t.Error("il jar non ha trattenuto nessun cookie: la sessione non potra` essere mantenuta")
	}
	if token == "" {
		t.Error("token CSRF non trovato: la POST di login verrebbe rifiutata")
	}
}

// TestProbeCloud32Login esercita la login reale con DoLoginAndRetrieveMain, che
// e` la funzione da cui dipendono sia /auth-with-zitadel sia i cron del registro
// soci. E` la verifica che conta davvero prima e dopo un cambio di toolchain:
// il trasporto puo` sembrare sano e la login rompersi comunque a valle, quando
// il jar non rimanda il cookie di sessione a /home.
//
// Le credenziali arrivano solo da ambiente e non vengono mai loggate: non
// devono finire nel repo.
func TestProbeCloud32Login(t *testing.T) {
	if os.Getenv("AREA32_PROBE") != "1" {
		t.Skip("sonda di rete: impostare AREA32_PROBE=1 per eseguirla")
	}
	email := os.Getenv("AREA32_PROBE_EMAIL")
	password := os.Getenv("AREA32_PROBE_PASSWORD")
	if email == "" || password == "" {
		t.Skip("servono AREA32_PROBE_EMAIL e AREA32_PROBE_PASSWORD")
	}

	user, err := NewAPI().DoLoginAndRetrieveMain(email, password)
	if err != nil {
		t.Fatalf("DoLoginAndRetrieveMain: %v", err)
	}
	if user == nil {
		t.Fatal("utente nil senza errore")
	}

	// Non logghiamo il nome completo per intero: basta sapere che e` arrivato.
	t.Logf("login riuscita: id=%q fullname_len=%d scadenza=%q attiva=%v",
		user.Id, len(user.Fullname), user.ExpireDate, user.IsMembershipActive)

	if strings.TrimSpace(user.Id) == "" {
		t.Error("id socio vuoto: lo scraping della home non ha prodotto l'identita`")
	}
	if strings.TrimSpace(user.Fullname) == "" {
		t.Error("fullname vuoto: la home e` stata servita anonima (cookie di sessione perso?)")
	}
}

func tlsVersionName(v uint16) string {
	switch v {
	case tls.VersionTLS10:
		return "TLS 1.0"
	case tls.VersionTLS11:
		return "TLS 1.1"
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS13:
		return "TLS 1.3"
	case 0:
		return "nessuno (connessione in chiaro?)"
	default:
		return "sconosciuta"
	}
}
