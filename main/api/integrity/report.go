package integrity

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/pocketbase/pocketbase/core"

	tools "mensadb/tools/integrity"
)

// reportMessage e` il messaggio della riga di log: la query su _logs per
// vedere le segnalazioni dei client e` message = 'integrity: segnalazione del
// client'.
const reportMessage = "integrity: segnalazione del client"

// reportHandler riceve la segnalazione di un'app che non riesce a produrre la
// prova di integrita` (POST /api/integrity/report) e la scrive nel log: quale
// chiamata, con quale errore del sistema, su quale dispositivo e build. Senza,
// il server vede solo un utente "watched" che non manda la prova e non puo`
// distinguere un telefono che non sa attestare da un client qualunque.
//
// DESCRIVE E NON CONCEDE. Il report e` testo non fidato scritto dal client (che
// puo` essere uno script) e non ha nessun effetto sull'autorizzazione: non
// legge ne' scrive sessioni, challenge, chiavi, token ricordati o access_level,
// non emette X-Integrity-Session e non cambia mai l'esito di Gate.Authorize.
// "Il mio dispositivo non riesce ad attestare" e` esattamente cio` che
// dichiarerebbe uno script, quindi non puo` abbassare l'asticella: se un
// socio con un telefono che non attesta va ammesso, lo decide una persona
// (users.access_level) leggendo questi log. Lo prova
// TestReportNeverChangesAuthorization.
//
//   - L'utente viene SEMPRE da e.Auth, mai dal corpo.
//   - Corpo JSON di al piu` tools.MaxReportBytes; oltre, 413.
//   - Al piu` tools.ReportsPerHourPerUser report all'ora per utente: oltre il
//     tetto la risposta e` la stessa, ma non si scrive niente (il client non
//     deve ritentare ne' accorgersene).
//   - Ogni campo passa da DeviceReport.Clean prima del log.
//   - Risposta costante {"ok":true}: il client la ignora e non deve imparare
//     niente da come il server ha letto il report.
//
// Un utente "blocked" non arriva qui: il middleware risponde 403
// account_blocked come per challenge e attest, e il client lo ignora.
func reportHandler(e *core.RequestEvent) error {
	gate := current.Load()
	if gate == nil {
		return e.JSON(http.StatusServiceUnavailable, map[string]any{
			"status": http.StatusServiceUnavailable, "message": "Servizio non pronto.",
		})
	}
	userID := e.Auth.Id

	// Il tetto viene PRIMA di leggere il corpo: oltre, non si fa piu` niente.
	if gate.Reports != nil && !gate.Reports.Allow(userID) {
		return reportOK(e)
	}

	raw, err := io.ReadAll(http.MaxBytesReader(e.Response, e.Request.Body, tools.MaxReportBytes))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return e.Error(http.StatusRequestEntityTooLarge, "Report troppo grande.", err)
		}
		return e.BadRequestError("Body non valido.", err)
	}
	var report tools.DeviceReport
	if err := json.Unmarshal(raw, &report); err != nil {
		return e.BadRequestError("Body non valido.", err)
	}

	logReport(e.App.Logger(), userID, report.Clean())
	return reportOK(e)
}

func reportOK(e *core.RequestEvent) error {
	return e.JSON(http.StatusOK, map[string]any{"ok": true})
}

// logReport scrive la riga del report. Il report deve essere gia` passato da
// Clean: qui non si ripulisce niente.
func logReport(l *slog.Logger, userID string, r tools.DeviceReport) {
	l.Warn(reportMessage,
		"user", userID,
		"platform", r.Platform,
		"stage", r.Stage,
		"domain", r.Domain,
		"code", r.Code,
		"detail", r.Detail,
		"underlying", r.Underlying,
		"disposition", r.Disposition,
		"outcome", r.Outcome,
		"attempt", r.Attempt,
		"key_phase", r.KeyPhase,
		"key_age_s", r.KeyAgeS,
		"elapsed_ms", r.ElapsedMs,
		"app", r.App,
		"os", r.OS,
		"ios_app_on_mac", r.IOSAppOnMac,
	)
}
