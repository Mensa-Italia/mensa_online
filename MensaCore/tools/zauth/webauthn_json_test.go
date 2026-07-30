package zauth

import (
	"encoding/json"
	"testing"

	"google.golang.org/protobuf/types/known/structpb"
)

// Options di assertion come le emette Zitadel: envelope "publicKey" prodotto da
// go-webauthn (protocol.CredentialAssertion), campi binari in base64url senza
// padding.
const assertionOptionsFixture = `{
  "publicKey": {
    "challenge": "aBcD_-123456789abcdefghijklmnopqrstuvw",
    "timeout": 300000,
    "rpId": "svc.mensa.it",
    "allowCredentials": [
      {"type": "public-key", "id": "Zm9vYmFyX2NyZWRlbnRpYWxfaWQ"}
    ],
    "userVerification": "required"
  }
}`

// Options di registrazione: stesso envelope, piu` user.id ed excludeCredentials
// (che Zitadel popola sempre, via webauthn.WithExclusions).
const creationOptionsFixture = `{
  "publicKey": {
    "rp": {"name": "Mensa Italia", "id": "svc.mensa.it"},
    "user": {"name": "socio@mensa.it", "displayName": "Mario Rossi", "id": "NTM2Ng"},
    "challenge": "Q2hhbGxlbmdlXy0tX3Rlc3Q",
    "pubKeyCredParams": [{"type": "public-key", "alg": -7}],
    "authenticatorSelection": {"authenticatorAttachment": "platform", "userVerification": "required"},
    "attestation": "none",
    "excludeCredentials": [{"type": "public-key", "id": "ZXhpc3RpbmdfY3JlZA"}]
  }
}`

// PublicKeyCredential come lo restituisce Android Credential Manager (e, dopo
// la ricostruzione manuale, iOS).
const assertionCredentialFixture = `{
  "id": "Zm9vYmFyX2NyZWRlbnRpYWxfaWQ",
  "rawId": "Zm9vYmFyX2NyZWRlbnRpYWxfaWQ",
  "type": "public-key",
  "authenticatorAttachment": "platform",
  "clientExtensionResults": {},
  "response": {
    "clientDataJSON": "eyJ0eXBlIjoid2ViYXV0aG4uZ2V0In0",
    "authenticatorData": "YXV0aGVudGljYXRvckRhdGE",
    "signature": "c2lnbmF0dXJl",
    "userHandle": "NTM2Ng"
  }
}`

func mustStruct(t *testing.T, raw string) *structpb.Struct {
	t.Helper()
	var s structpb.Struct
	if err := s.UnmarshalJSON([]byte(raw)); err != nil {
		t.Fatalf("fixture non valida: %v", err)
	}
	return &s
}

func unwrapToMap(t *testing.T, raw string) map[string]any {
	t.Helper()
	out, err := UnwrapCredentialOptions(mustStruct(t, raw))
	if err != nil {
		t.Fatalf("UnwrapCredentialOptions: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("output non e` JSON valido: %v", err)
	}
	return m
}

func TestUnwrapCredentialOptionsStripsEnvelope(t *testing.T) {
	m := unwrapToMap(t, assertionOptionsFixture)

	if _, present := m["publicKey"]; present {
		t.Error(`l'envelope "publicKey" non e` + "`" + ` stato rimosso`)
	}
	if got, want := m["rpId"], "svc.mensa.it"; got != want {
		t.Errorf("rpId = %v, want %v", got, want)
	}
	if got, want := m["userVerification"], "required"; got != want {
		t.Errorf("userVerification = %v, want %v", got, want)
	}
}

// L'invariante che conta davvero: i campi binari escono byte-identici. Un
// re-encoding accidentale (padding aggiunto, alfabeto standard invece di
// url-safe) fa rifiutare l'assertion da Zitadel con un errore opaco, ed e`
// proprio la classe di bug che questo adapter esiste per non introdurre.
func TestUnwrapCredentialOptionsPreservesBase64URLVerbatim(t *testing.T) {
	assertion := unwrapToMap(t, assertionOptionsFixture)

	if got, want := assertion["challenge"], "aBcD_-123456789abcdefghijklmnopqrstuvw"; got != want {
		t.Errorf("challenge = %q, want %q", got, want)
	}

	allowed, ok := assertion["allowCredentials"].([]any)
	if !ok || len(allowed) != 1 {
		t.Fatalf("allowCredentials = %v, want una sola voce", assertion["allowCredentials"])
	}
	cred, _ := allowed[0].(map[string]any)
	if got, want := cred["id"], "Zm9vYmFyX2NyZWRlbnRpYWxfaWQ"; got != want {
		t.Errorf("allowCredentials[0].id = %q, want %q", got, want)
	}

	creation := unwrapToMap(t, creationOptionsFixture)

	if got, want := creation["challenge"], "Q2hhbGxlbmdlXy0tX3Rlc3Q"; got != want {
		t.Errorf("challenge = %q, want %q", got, want)
	}
	user, _ := creation["user"].(map[string]any)
	if got, want := user["id"], "NTM2Ng"; got != want {
		t.Errorf("user.id = %q, want %q (nessun padding, alfabeto url-safe)", got, want)
	}
	excluded, ok := creation["excludeCredentials"].([]any)
	if !ok || len(excluded) != 1 {
		t.Fatalf("excludeCredentials = %v, want una sola voce", creation["excludeCredentials"])
	}
	exCred, _ := excluded[0].(map[string]any)
	if got, want := exCred["id"], "ZXhpc3RpbmdfY3JlZA"; got != want {
		t.Errorf("excludeCredentials[0].id = %q, want %q", got, want)
	}
}

func TestUnwrapCredentialOptionsWithoutEnvelope(t *testing.T) {
	// Se Zitadel un giorno restituisse l'oggetto nudo, non dobbiamo rompere.
	m := unwrapToMap(t, `{"challenge": "bmFrZWQ", "rpId": "svc.mensa.it"}`)

	if got, want := m["challenge"], "bmFrZWQ"; got != want {
		t.Errorf("challenge = %q, want %q", got, want)
	}
}

func TestUnwrapCredentialOptionsErrors(t *testing.T) {
	if _, err := UnwrapCredentialOptions(nil); err == nil {
		t.Error("options nil: attesa un errore")
	}

	notAnObject := mustStruct(t, `{"publicKey": "non-sono-un-oggetto"}`)
	if _, err := UnwrapCredentialOptions(notAnObject); err == nil {
		t.Error(`"publicKey" scalare: atteso un errore`)
	}
}

func TestCredentialToStructRoundTrip(t *testing.T) {
	s, err := CredentialToStruct([]byte(assertionCredentialFixture))
	if err != nil {
		t.Fatalf("CredentialToStruct: %v", err)
	}

	raw, err := s.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("output non e` JSON valido: %v", err)
	}

	if got, want := m["type"], "public-key"; got != want {
		t.Errorf("type = %v, want %v", got, want)
	}
	if got, want := m["rawId"], "Zm9vYmFyX2NyZWRlbnRpYWxfaWQ"; got != want {
		t.Errorf("rawId = %q, want %q", got, want)
	}

	resp, ok := m["response"].(map[string]any)
	if !ok {
		t.Fatalf("response = %v, want un oggetto", m["response"])
	}
	for field, want := range map[string]string{
		"clientDataJSON":    "eyJ0eXBlIjoid2ViYXV0aG4uZ2V0In0",
		"authenticatorData": "YXV0aGVudGljYXRvckRhdGE",
		"signature":         "c2lnbmF0dXJl",
		"userHandle":        "NTM2Ng",
	} {
		if got := resp[field]; got != want {
			t.Errorf("response.%s = %q, want %q", field, got, want)
		}
	}
}

func TestCredentialToStructRejectsBadPayloads(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"vuoto", ``},
		{"json_non_valido", `{"type": `},
		{"envelope_per_errore", `{"publicKey": {"type": "public-key"}}`},
		{"response_mancante", `{"id": "abc", "type": "public-key"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := CredentialToStruct([]byte(c.raw)); err == nil {
				t.Errorf("CredentialToStruct(%q): atteso un errore", c.raw)
			}
		})
	}
}
