package exapp

import (
	"net/http"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

// Export dei dati verso le app esterne: macchina a stati del consenso.
//
//	pending --decide(true)--> approved --fetch--> consumed
//	   |                          |
//	   +--decide(false)--> denied |
//	   +------ scadenza ----------+--> expired
//
// Il consenso e` MONOUSO: dopo il fetch la richiesta e` `consumed` e per una
// nuova estrazione serve una nuova conferma in app. L'approvazione non
// allunga la vita della richiesta: vale fino a expires_at, fissato alla
// creazione.
//
// Ogni passaggio e` un UPDATE condizionale sullo stato di partenza (e sulla
// scadenza), quindi atomico: se due fetch arrivano insieme ne vince uno solo
// (RowsAffected = 1), l'altro trova la riga gia` `consumed`. Niente
// read-modify-write in Go, che lascerebbe passare due estrazioni.

const (
	exportRequestsCollection = "ex_export_requests"

	statusPending  = "pending"
	statusApproved = "approved"
	statusDenied   = "denied"
	statusConsumed = "consumed"
	statusExpired  = "expired"

	scopeStamps = "stamps"
	scopeMedals = "medals"

	// permessi di ex_keys.permissions
	permExportStamps = "EXPORT_STAMPS"
	permExportMedals = "EXPORT_MEDALS"

	// exportTTL e` la vita di una richiesta dalla creazione.
	exportTTL = 10 * time.Minute

	// exportRetention: le richieste concluse (denied / consumed / expired) si
	// tengono ancora un po' per poter ricostruire cosa e` stato esportato e
	// quando, poi la pulizia le toglie.
	exportRetention = 30 * 24 * time.Hour
)

// nowFunc e` l'orologio, sostituibile nei test.
var nowFunc = time.Now

// apiError e` un errore da restituire come risposta HTTP.
type apiError struct {
	Status  int
	Message string
}

func (e *apiError) Error() string { return e.Message }

// permissionForScope ritorna il permesso di ex_keys richiesto dallo scope, o
// "" se lo scope non esiste. Timbri e medaglie sono permessi distinti.
func permissionForScope(scope string) string {
	switch scope {
	case scopeStamps:
		return permExportStamps
	case scopeMedals:
		return permExportMedals
	}
	return ""
}

// dbTime formatta un istante come lo salva PocketBase nei campi date, cosi` i
// confronti fra stringhe nelle query (`expires_at > {:now}`) sono confronti
// cronologici.
func dbTime(t time.Time) string {
	dt, _ := types.ParseDateTime(t.UTC())
	return dt.String()
}

// createExportRequest crea una richiesta `pending`. Le richieste ancora
// `pending` della stessa terna (app, utente, scope) vengono chiuse come
// `expired`: un partner che rilancia la richiesta sostituisce la precedente, e
// l'utente non si ritrova due conferme per lo stesso dato.
func createExportRequest(app core.App, exAppID, userID, scope, callmeURL string, now time.Time) (*core.Record, error) {
	if _, err := app.DB().NewQuery(`
		UPDATE ` + exportRequestsCollection + `
		SET status = {:expired}, updated = {:now}
		WHERE ex_app = {:app} AND user = {:user} AND scope = {:scope} AND status = {:pending}`,
	).Bind(dbx.Params{
		"expired": statusExpired, "pending": statusPending, "now": dbTime(now),
		"app": exAppID, "user": userID, "scope": scope,
	}).Execute(); err != nil {
		return nil, err
	}

	collection, err := app.FindCachedCollectionByNameOrId(exportRequestsCollection)
	if err != nil {
		return nil, err
	}
	rec := core.NewRecord(collection)
	rec.Set("ex_app", exAppID)
	rec.Set("user", userID)
	rec.Set("scope", scope)
	rec.Set("status", statusPending)
	rec.Set("callme_url", callmeURL)
	rec.Set("expires_at", dbTime(now.Add(exportTTL)))
	if err := app.Save(rec); err != nil {
		return nil, err
	}
	return rec, nil
}

// decideExportRequest registra la decisione dell'utente su una sua richiesta
// `pending` e non scaduta, e ritorna il nuovo stato ("approved" | "denied").
// Richiesta inesistente o di un altro utente: 404 (non si distingue, per non
// far sondare gli id). Gia` decisa, consumata o scaduta: 409.
func decideExportRequest(app core.App, requestID, userID string, accepted bool, now time.Time) (string, *apiError) {
	next := statusDenied
	if accepted {
		next = statusApproved
	}

	res, err := app.DB().NewQuery(`
		UPDATE ` + exportRequestsCollection + `
		SET status = {:next}, decided_at = {:now}, updated = {:now}
		WHERE id = {:id} AND user = {:user} AND status = {:pending} AND expires_at > {:now}`,
	).Bind(dbx.Params{
		"next": next, "pending": statusPending, "now": dbTime(now), "id": requestID, "user": userID,
	}).Execute()
	if err != nil {
		app.Logger().Error("export: decide fallita", "request", requestID, "err", err)
		return "", &apiError{http.StatusInternalServerError, "Errore interno."}
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return next, nil
	}

	// Nessuna riga toccata: capire perche' per rispondere 404 o 409.
	rec, err := app.FindRecordById(exportRequestsCollection, requestID)
	if err != nil || rec.GetString("user") != userID {
		return "", &apiError{http.StatusNotFound, "Richiesta non trovata."}
	}
	if rec.GetString("status") == statusPending {
		// Era pending ma scaduta: la si chiude, cosi' lo stato reale e` visibile.
		_ = markExpired(app, requestID, now)
		return "", &apiError{http.StatusConflict, "Richiesta scaduta."}
	}
	return "", &apiError{http.StatusConflict, "Richiesta gia` conclusa (" + rec.GetString("status") + ")."}
}

// consumeExportRequest porta `approved` -> `consumed` in modo atomico e dice
// se questa chiamata ha vinto. Con N chiamate concorrenti ne vince una sola.
func consumeExportRequest(app core.App, requestID string, now time.Time) (bool, error) {
	res, err := app.DB().NewQuery(`
		UPDATE ` + exportRequestsCollection + `
		SET status = {:consumed}, consumed_at = {:now}, updated = {:now}
		WHERE id = {:id} AND status = {:approved} AND expires_at > {:now}`,
	).Bind(dbx.Params{
		"consumed": statusConsumed, "approved": statusApproved, "now": dbTime(now), "id": requestID,
	}).Execute()
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// markExpired chiude come `expired` una richiesta pending/approved scaduta
// (scadenza "pigra", quando qualcuno la legge).
func markExpired(app core.App, requestID string, now time.Time) error {
	_, err := app.DB().NewQuery(`
		UPDATE ` + exportRequestsCollection + `
		SET status = {:expired}, updated = {:now}
		WHERE id = {:id} AND status IN ({:pending}, {:approved}) AND expires_at <= {:now}`,
	).Bind(dbx.Params{
		"expired": statusExpired, "pending": statusPending, "approved": statusApproved,
		"now": dbTime(now), "id": requestID,
	}).Execute()
	return err
}

// CleanupExportRequests e` la pulizia periodica (cron): chiude come `expired`
// le richieste pending/approved scadute che nessuno ha piu` letto, e toglie le
// richieste concluse piu' vecchie di exportRetention. Ritorna quante ne ha
// chiuse e quante cancellate.
func CleanupExportRequests(app core.App) (expired, deleted int64, err error) {
	now := nowFunc()

	res, err := app.DB().NewQuery(`
		UPDATE ` + exportRequestsCollection + `
		SET status = {:expired}, updated = {:now}
		WHERE status IN ({:pending}, {:approved}) AND expires_at <= {:now}`,
	).Bind(dbx.Params{
		"expired": statusExpired, "pending": statusPending, "approved": statusApproved, "now": dbTime(now),
	}).Execute()
	if err != nil {
		return 0, 0, err
	}
	expired, _ = res.RowsAffected()

	res, err = app.DB().NewQuery(`
		DELETE FROM ` + exportRequestsCollection + `
		WHERE status IN ({:denied}, {:consumed}, {:expired}) AND updated < {:cutoff}`,
	).Bind(dbx.Params{
		"denied": statusDenied, "consumed": statusConsumed, "expired": statusExpired,
		"cutoff": dbTime(now.Add(-exportRetention)),
	}).Execute()
	if err != nil {
		return expired, 0, err
	}
	deleted, _ = res.RowsAffected()
	return expired, deleted, nil
}
