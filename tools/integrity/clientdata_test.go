package integrity

import (
	"crypto/sha256"
	"encoding/base64"
	"testing"
)

// La stringa e` challenge|METHOD|PATH, con metodo maiuscolo e solo il path.
func TestClientData(t *testing.T) {
	cases := []struct{ ch, method, path, want string }{
		{"abc", "POST", "/api/collections/stamp_users/records", "abc|POST|/api/collections/stamp_users/records"},
		{"abc", "get", "/api/cs/me", "abc|GET|/api/cs/me"},
		{"Zm9v", "PATCH", "/api/collections/users/records/xyz", "Zm9v|PATCH|/api/collections/users/records/xyz"},
	}
	for _, c := range cases {
		if got := ClientData(c.ch, c.method, c.path); got != c.want {
			t.Errorf("ClientData(%q,%q,%q) = %q, want %q", c.ch, c.method, c.path, got, c.want)
		}
	}
}

// Vettore noto: SHA-256("abc|GET|/api/cs/me") calcolato indipendentemente,
// in hex (iOS) e in base64url senza padding (Android).
func TestHashes(t *testing.T) {
	cd := ClientData("abc", "GET", "/api/cs/me")
	sum := sha256.Sum256([]byte("abc|GET|/api/cs/me"))

	if got := ClientDataHash(cd); string(got) != string(sum[:]) {
		t.Errorf("ClientDataHash non e` SHA-256 del clientData")
	}
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	if got := RequestHash(cd); got != want {
		t.Errorf("RequestHash = %q, want %q", got, want)
	}
	if len(want) != 43 || want[len(want)-1] == '=' {
		t.Errorf("il requestHash deve essere di 43 caratteri senza padding: %q", want)
	}
}

// Metodo, path e challenge diversi danno hash diversi.
func TestHashesAreBound(t *testing.T) {
	base := RequestHash(ClientData("c", "GET", "/api/a"))
	for _, other := range []string{
		RequestHash(ClientData("d", "GET", "/api/a")),
		RequestHash(ClientData("c", "POST", "/api/a")),
		RequestHash(ClientData("c", "GET", "/api/b")),
	} {
		if other == base {
			t.Error("l'hash non e` legato a challenge/metodo/path")
		}
	}
}
