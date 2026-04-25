package dbtools

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// GetHMACSHA256Hash calcola HMAC-SHA256 del valore (post-normalizzazione) usando il salt come chiave.
// La normalizzazione è la stessa applicata da GetMD5Hash, in modo che hash provenienti da
// fonti differenti restino confrontabili (case/spazi/newline/NFKC).
//
// A differenza di GetMD5Hash:
// - usa HMAC con SHA-256 invece di un MD5 con concatenazione naive value+salt;
// - resiste a length-extension e a collisioni note di MD5;
// - richiede la conoscenza del salt (server-side) per ricalcolare gli hash, quindi gli hash
//   diventano opachi per chi non possiede il salt.
func GetHMACSHA256Hash(value, salt string) string {
	normalized := NormalizeTextForHash(value)
	mac := hmac.New(sha256.New, []byte(salt))
	mac.Write([]byte(normalized))
	return hex.EncodeToString(mac.Sum(nil))
}
