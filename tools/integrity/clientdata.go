package integrity

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
)

// ClientData e` la stringa che la prova per-richiesta vincola, identica a
// quella costruita dal client: challenge + "|" + METHOD + "|" + PATH.
//
// METHOD e` maiuscolo e PATH e` solo il path dell'URL (r.URL.Path), senza
// query string ne` host. Legare metodo e path fa si` che una prova ottenuta
// per una richiesta non si possa riusare su un'altra, e la challenge
// monouso che non si possa riusare affatto.
func ClientData(challenge, method, path string) string {
	return challenge + "|" + strings.ToUpper(method) + "|" + path
}

// ClientDataHash e` SHA256(UTF8(clientData)): quello che iOS passa a
// generateAssertion / attestKey.
func ClientDataHash(clientData string) []byte {
	sum := sha256.Sum256([]byte(clientData))
	return sum[:]
}

// RequestHash e` il requestHash di Play Integrity (Standard API):
// base64url senza padding di SHA256(UTF8(clientData)).
func RequestHash(clientData string) string {
	return base64.RawURLEncoding.EncodeToString(ClientDataHash(clientData))
}
