package env

import (
	"reflect"
	"testing"
)

// Senza variabili la feature deve avere i default del contratto e
// MustValidate non deve lamentare niente di nuovo: il backend parte anche
// senza credenziali Play Integrity.
func TestIntegrityDefaults(t *testing.T) {
	if GetPlayIntegrityPackageName() != "it.mensa.app" {
		t.Errorf("package %q", GetPlayIntegrityPackageName())
	}
	if GetAppAttestAppID() != "6WA5D3RJBU.it.mensa.app" {
		t.Errorf("app id %q", GetAppAttestAppID())
	}
	if GetAppAttestEnvironment() != "production" {
		t.Errorf("environment %q", GetAppAttestEnvironment())
	}
	if GetPlayIntegrityRequireLicensed() {
		t.Error("REQUIRE_LICENSED deve essere false di default")
	}
	if len(GetPlayIntegrityCertSHA256()) != 0 || GetPlayIntegrityCredentialsJSON() != "" {
		t.Error("cert e credenziali vuoti di default")
	}
}

func TestIntegrityGetters(t *testing.T) {
	old := cfg
	t.Cleanup(func() { cfg = old })

	cfg.PlayIntegrityCertSHA256 = " AB:CD , ,xyz=,, "
	if got, want := GetPlayIntegrityCertSHA256(), []string{"AB:CD", "xyz="}; !reflect.DeepEqual(got, want) {
		t.Errorf("cert: %v", got)
	}

	for in, want := range map[string]string{
		"development": "development", " Development ": "development",
		"production": "production", "": "production", "prod": "production", "dev": "production",
	} {
		cfg.AppAttestEnvironment = in
		if got := GetAppAttestEnvironment(); got != want {
			t.Errorf("environment %q -> %q, atteso %q", in, got, want)
		}
	}
}
