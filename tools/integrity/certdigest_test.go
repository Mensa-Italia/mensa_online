package integrity

import (
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
)

func TestNormalizeCertDigest(t *testing.T) {
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = byte(i*7 + 3)
	}
	want := base64.RawURLEncoding.EncodeToString(raw)
	hexUpper := strings.ToUpper(hex.EncodeToString(raw))
	var colon []string
	for i := 0; i < len(hexUpper); i += 2 {
		colon = append(colon, hexUpper[i:i+2])
	}

	forms := map[string]string{
		"hex con due punti":   strings.Join(colon, ":"),
		"hex minuscolo":       strings.ToLower(hexUpper),
		"hex senza due punti": hexUpper,
		"base64 std":          base64.StdEncoding.EncodeToString(raw),
		"base64 url padded":   base64.URLEncoding.EncodeToString(raw),
		"base64 url raw":      want,
		"con spazi attorno":   "  " + want + "  ",
	}
	for name, in := range forms {
		got, err := NormalizeCertDigest(in)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if got != want {
			t.Errorf("%s: %q, want %q", name, got, want)
		}
	}
}

func TestNormalizeCertDigestErrors(t *testing.T) {
	for _, in := range []string{"", "   ", "non un digest!", "AB:CD", base64.RawURLEncoding.EncodeToString([]byte("troppo corto"))} {
		if _, err := NormalizeCertDigest(in); err == nil {
			t.Errorf("%q doveva fallire", in)
		}
	}
}

func TestNormalizeCertDigestsDedupAndBad(t *testing.T) {
	raw := make([]byte, 32)
	a := base64.RawURLEncoding.EncodeToString(raw)
	ok, bad := NormalizeCertDigests([]string{a, base64.StdEncoding.EncodeToString(raw), "zzz"})
	if len(ok) != 1 || ok[0] != a {
		t.Errorf("ok = %v", ok)
	}
	if len(bad) != 1 {
		t.Errorf("bad = %v", bad)
	}
}
