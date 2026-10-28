package integrity

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

// NormalizeCertDigest porta un digest SHA-256 di certificato nella forma che
// Play Integrity usa in appIntegrity.certificateSha256Digest: base64url senza
// padding di 32 byte.
//
// Accetta le forme in cui il digest circola davvero:
//   - esadecimale con i due punti, come lo mostra la Play Console
//     ("AB:CD:...:EF", 95 caratteri);
//   - esadecimale senza separatori (64 caratteri);
//   - base64 standard o url-safe, con o senza padding.
func NormalizeCertDigest(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", fmt.Errorf("digest vuoto")
	}

	hexForm := strings.NewReplacer(":", "", " ", "").Replace(s)
	if len(hexForm) == 64 && isHex(hexForm) {
		b, err := hex.DecodeString(hexForm)
		if err != nil {
			return "", err
		}
		return base64.RawURLEncoding.EncodeToString(b), nil
	}

	// Base64: si tolgono padding e si riconducono i due alfabeti a url-safe.
	b64 := strings.TrimRight(s, "=")
	b64 = strings.NewReplacer("+", "-", "/", "_").Replace(b64)
	b, err := base64.RawURLEncoding.DecodeString(b64)
	if err != nil {
		return "", fmt.Errorf("ne` esadecimale ne` base64: %w", err)
	}
	if len(b) != 32 {
		return "", fmt.Errorf("un SHA-256 e` di 32 byte, questo ne ha %d", len(b))
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// NormalizeCertDigests normalizza un elenco e ritorna anche le voci scartate
// (con il motivo), cosi` il chiamante puo` loggarle all'avvio senza che una
// voce sbagliata faccia sparire le altre.
func NormalizeCertDigests(raw []string) (ok []string, bad []string) {
	seen := map[string]bool{}
	for _, r := range raw {
		n, err := NormalizeCertDigest(r)
		if err != nil {
			bad = append(bad, fmt.Sprintf("%q: %v", r, err))
			continue
		}
		if !seen[n] {
			seen[n] = true
			ok = append(ok, n)
		}
	}
	return ok, bad
}

func isHex(s string) bool {
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}
