package integrity

import (
	"strings"
	"unicode"
)

// Limiti del report (POST /api/integrity/report).
const (
	// MaxReportBytes e` la dimensione massima del corpo: un report tipico ne
	// pesa circa 500, e con le stringhe al massimo (MaxReportText) resta sotto
	// il tetto.
	MaxReportBytes = 2048
	// ReportsPerHourPerUser e` quanti report logga il server per un utente in
	// un'ora. Il ritmo del client (backoff 30 s x 4^(n-1), tetto 10 min, piu'
	// uno per rotazione) ne manda al piu' una decina: il resto e` rumore o uno
	// script che vuole sporcare i log.
	ReportsPerHourPerUser = 20

	// MaxReportText e` la lunghezza massima, in caratteri, di ogni stringa.
	MaxReportText = 200

	maxReportAttempt = 1000
	maxReportKeyAge  = 10 * 365 * 24 * 3600 // secondi: dieci anni
	maxReportElapsed = 24 * 3600 * 1000     // millisecondi: un giorno
	maxReportCode    = 1<<31 - 1
)

// DeviceReport e` la segnalazione che un'app manda quando non riesce a
// produrre la prova di integrita` (POST /api/integrity/report): dice QUALE
// chiamata ha fallito e con quale errore del sistema, perche' il server da solo
// vede soltanto un utente "watched" che non manda la prova.
//
// E` testo NON fidato, scritto da un client che puo` essere uno script: serve a
// leggere i log, mai a decidere. Per questo si passa sempre da Clean e non
// entra in nessuna decisione di accesso (Gate non lo conosce).
type DeviceReport struct {
	Platform    string `json:"platform"`    // ios | android
	Stage       string `json:"stage"`       // generate_key | attest_key | assertion | register | challenge | server
	Domain      string `json:"domain"`      // dominio NSError, it.mensa.timeout o http
	Code        int    `json:"code"`        // codice dell'errore
	Detail      string `json:"detail"`      // localizedDescription
	Underlying  string `json:"underlying"`  // dominio/codice/descrizione dell'errore sottostante
	Disposition string `json:"disposition"` // unsupported | transient | discard_key
	Outcome     string `json:"outcome"`     // failed | rotated
	Attempt     int    `json:"attempt"`     // tentativo consecutivo
	KeyPhase    string `json:"key_phase"`   // none | pending | ready
	KeyAgeS     int    `json:"key_age_s"`   // eta` della chiave in secondi, -1 se ignota
	ElapsedMs   int    `json:"elapsed_ms"`  // durata della chiamata fallita
	App         string `json:"app"`         // CFBundleVersion / versionCode
	OS          string `json:"os"`          // versione del sistema
	IOSAppOnMac bool   `json:"ios_app_on_mac"`
}

// Elenchi chiusi del report: quello che non ci sta diventa "other" ("?" per la
// piattaforma). Un valore vuoto in un campo facoltativo resta vuoto.
var (
	reportPlatforms    = []string{PlatformIOS, PlatformAndroid}
	reportStages       = []string{"generate_key", "attest_key", "assertion", "register", "challenge", "server"}
	reportDispositions = []string{"unsupported", "transient", "discard_key"}
	reportOutcomes     = []string{"failed", "rotated"}
	reportKeyPhases    = []string{"none", "pending", "ready"}
)

// Clean ritorna il report ripulito, l'unica forma che si scrive nei log:
// piattaforma, stage, disposition, outcome e key_phase da elenchi chiusi,
// stringhe senza caratteri di controllo e al piu` MaxReportText caratteri,
// numeri dentro un intervallo ragionevole. Funzione pura: non tocca il
// ricevente e non guarda nient'altro.
func (r DeviceReport) Clean() DeviceReport {
	return DeviceReport{
		Platform:    pickClosed(r.Platform, reportPlatforms, "?"),
		Stage:       pickClosed(r.Stage, reportStages, "other"),
		Domain:      CleanText(r.Domain, MaxReportText),
		Code:        clamp(r.Code, -maxReportCode, maxReportCode),
		Detail:      CleanText(r.Detail, MaxReportText),
		Underlying:  CleanText(r.Underlying, MaxReportText),
		Disposition: pickOptional(r.Disposition, reportDispositions),
		Outcome:     pickOptional(r.Outcome, reportOutcomes),
		Attempt:     clamp(r.Attempt, 0, maxReportAttempt),
		KeyPhase:    pickOptional(r.KeyPhase, reportKeyPhases),
		KeyAgeS:     clamp(r.KeyAgeS, -1, maxReportKeyAge),
		ElapsedMs:   clamp(r.ElapsedMs, 0, maxReportElapsed),
		App:         CleanText(r.App, MaxReportText),
		OS:          CleanText(r.OS, MaxReportText),
		IOSAppOnMac: r.IOSAppOnMac,
	}
}

// CleanText prepara una stringa non fidata per un log: UTF-8 valido (i byte
// che non lo sono diventano U+FFFD), i caratteri di controllo (a capo e
// tabulazioni comprese) diventano uno spazio, quelli invisibili di formato
// (zero-width, inversione bidi) spariscono, e si taglia a limit caratteri.
func CleanText(s string, limit int) string {
	var b strings.Builder
	n := 0
	for _, c := range s { // il range sostituisce da solo i byte non UTF-8
		switch {
		case unicode.IsControl(c) || c == '\u2028' || c == '\u2029':
			c = ' '
		case unicode.Is(unicode.Cf, c):
			continue
		}
		if n == limit {
			break
		}
		b.WriteRune(c)
		n++
	}
	return strings.TrimSpace(b.String())
}

// pickClosed ritorna v (minuscolo, senza spazi ai bordi) se sta nell'elenco,
// altrimenti other.
func pickClosed(v string, allowed []string, other string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	for _, a := range allowed {
		if v == a {
			return v
		}
	}
	return other
}

// pickOptional e` pickClosed per un campo facoltativo: assente resta vuoto.
func pickOptional(v string, allowed []string) string {
	if strings.TrimSpace(v) == "" {
		return ""
	}
	return pickClosed(v, allowed, "other")
}

func clamp(v, lo, hi int) int { return min(max(v, lo), hi) }
