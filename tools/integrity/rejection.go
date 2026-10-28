package integrity

import "net/http"

// Codici d'errore del contratto (campo "code" della risposta JSON).
const (
	CodeAccountBlocked   = "account_blocked"
	CodeIntegrityRequire = "integrity_required"
	CodeAttestKeyRequire = "attest_key_required"
	CodeIntegrityFailed  = "integrity_failed"
)

// Rejection descrive un rifiuto: cosa vede il client (Status, Code, Message)
// e il motivo preciso che va solo nel log del server (Reason). Il client non
// deve sapere PERCHE' una prova e` stata scartata: darebbe a chi forgia
// richieste un oracolo su cosa correggere.
type Rejection struct {
	Status  int
	Code    string
	Message string
	Reason  string
}

func (r *Rejection) Error() string { return r.Code + ": " + r.Reason }

// Body e` il JSON di risposta: {"status", "message", "code"}.
func (r *Rejection) Body() map[string]any {
	return map[string]any{"status": r.Status, "message": r.Message, "code": r.Code}
}

// Blocked e` il rifiuto per un utente con access_level=blocked.
func Blocked(reason string) *Rejection {
	return &Rejection{
		Status:  http.StatusForbidden,
		Code:    CodeAccountBlocked,
		Message: "Account sospeso.",
		Reason:  reason,
	}
}

func required(reason string) *Rejection {
	return &Rejection{
		Status:  http.StatusPreconditionRequired,
		Code:    CodeIntegrityRequire,
		Message: "Prova di integrita` dell'app richiesta.",
		Reason:  reason,
	}
}

func attestKeyRequired(reason string) *Rejection {
	return &Rejection{
		Status:  http.StatusPreconditionRequired,
		Code:    CodeAttestKeyRequire,
		Message: "Chiave App Attest assente o sconosciuta: registrane una nuova.",
		Reason:  reason,
	}
}

// Failed e` il rifiuto per una prova presente ma non valida (o per un
// controllo che non si puo` eseguire: fail closed).
func Failed(reason string) *Rejection {
	return &Rejection{
		Status:  http.StatusForbidden,
		Code:    CodeIntegrityFailed,
		Message: "Verifica di integrita` dell'app non superata.",
		Reason:  reason,
	}
}
