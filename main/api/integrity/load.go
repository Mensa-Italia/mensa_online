package integrity

import (
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	tools "mensadb/tools/integrity"
)

// Load registra gli endpoint del gruppo /api/integrity. Richiedono auth; sono
// esenti dalla prova per-richiesta (vedi tools.Exempt) perche' servono proprio
// a ottenerla, o a raccontare perche' non ci si riesce (report).
func Load(e *router.RouterGroup[*core.RequestEvent]) {
	e.POST("/challenge", challengeHandler).Bind(apis.RequireAuth())
	e.POST("/ios/attest", iosAttestHandler).Bind(apis.RequireAuth())
	e.POST("/report", reportHandler).Bind(apis.RequireAuth())
}

// challengeHandler emette una challenge monouso legata all'utente.
func challengeHandler(e *core.RequestEvent) error {
	gate := current.Load()
	if gate == nil {
		return e.JSON(http.StatusServiceUnavailable, map[string]any{
			"status": http.StatusServiceUnavailable, "message": "Servizio non pronto.",
		})
	}
	challenge, err := gate.Challenges.Issue(e.Auth.Id)
	if err != nil {
		return e.InternalServerError("Impossibile generare la challenge.", err)
	}
	return e.JSON(http.StatusOK, map[string]any{
		"challenge":  challenge,
		"expires_in": int(tools.ChallengeTTL.Seconds()),
	})
}

type iosAttestBody struct {
	KeyID       string `json:"key_id" form:"key_id"`
	Attestation string `json:"attestation" form:"attestation"`
	Challenge   string `json:"challenge" form:"challenge"`
}

// iosAttestHandler verifica l'attestazione App Attest di una chiave nuova e la
// registra per l'utente. La challenge si consuma sempre, anche se la verifica
// poi fallisce.
func iosAttestHandler(e *core.RequestEvent) error {
	gate := current.Load()
	if gate == nil || gate.IOS == nil {
		return e.JSON(http.StatusServiceUnavailable, map[string]any{
			"status": http.StatusServiceUnavailable, "message": "Servizio non pronto.",
		})
	}

	var body iosAttestBody
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("Body non valido.", err)
	}
	body.Challenge = strings.TrimSpace(body.Challenge)
	body.KeyID = strings.TrimSpace(body.KeyID)

	if !gate.Challenges.Consume(e.Auth.Id, body.Challenge) {
		return reject(e, tools.Failed("attest: challenge sconosciuta, scaduta, gia` usata o di un altro utente"))
	}
	if e.Auth.Collection().Name != "users" {
		return reject(e, tools.Failed("attest: solo gli utenti della collection users possono registrare chiavi"))
	}

	keyID, err := base64.StdEncoding.DecodeString(body.KeyID)
	if err != nil || len(keyID) != 32 {
		return reject(e, tools.Failed("attest: key_id non e` un base64 di 32 byte"))
	}
	attestation, err := base64.StdEncoding.DecodeString(body.Attestation)
	if err != nil || len(attestation) == 0 {
		return reject(e, tools.Failed("attest: attestation non in base64"))
	}

	pub, err := gate.IOS.VerifyAttestation(keyID, attestation, tools.ClientDataHash(body.Challenge))
	if err != nil {
		return reject(e, tools.Failed("attest: "+err.Error()))
	}

	// Un keyId e` registrabile una volta sola: se esiste gia` (anche per lo
	// stesso utente) l'attestazione e` un replay o un conflitto.
	if _, err := gate.IOS.Keys.Find(body.KeyID); err == nil {
		return reject(e, tools.Failed("attest: key_id gia` registrato"))
	}
	if err := gate.IOS.Keys.Create(&tools.Key{
		KeyID:       body.KeyID,
		UserID:      e.Auth.Id,
		PublicKey:   pub,
		Counter:     0,
		Environment: gate.IOS.Environment,
	}); err != nil {
		return e.InternalServerError("Impossibile salvare la chiave.", err)
	}
	return e.JSON(http.StatusOK, map[string]any{"ok": true})
}
