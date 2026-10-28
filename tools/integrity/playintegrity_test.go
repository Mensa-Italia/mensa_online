package integrity

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
)

const testPkg = "it.mensa.app"

var testNow = time.Unix(1_800_000_000, 0)

func digestOf(b byte) string {
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = b
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

type fakeDecoder struct {
	verdict *Verdict
	err     error
	gotPkg  string
	gotTok  string
}

func (f *fakeDecoder) Decode(_ context.Context, pkg, token string) (*Verdict, error) {
	f.gotPkg, f.gotTok = pkg, token
	return f.verdict, f.err
}

func goodVerdict(hash string) *Verdict {
	return &Verdict{
		RequestPackageName:    testPkg,
		RequestHash:           hash,
		TimestampMillis:       testNow.UnixMilli(),
		AppRecognitionVerdict: "PLAY_RECOGNIZED",
		AppPackageName:        testPkg,
		CertSHA256Digests:     []string{digestOf(1)},
		DeviceVerdicts:        []string{"MEETS_BASIC_INTEGRITY", "MEETS_DEVICE_INTEGRITY"},
		LicensingVerdict:      "LICENSED",
	}
}

func newVerifier(d TokenDecoder) *AndroidVerifier {
	return &AndroidVerifier{
		Decoder:     d,
		PackageName: testPkg,
		CertDigests: []string{digestOf(1), digestOf(2)},
		Now:         func() time.Time { return testNow },
	}
}

func TestCheckVerdict(t *testing.T) {
	const hash = "HASH"
	cases := []struct {
		name    string
		mutate  func(v *Verdict)
		licence bool
		wantErr string
	}{
		{"ok", func(v *Verdict) {}, false, ""},
		{"ok con seconda firma configurata", func(v *Verdict) { v.CertSHA256Digests = []string{"altro", digestOf(2)} }, false, ""},
		{"ok licenza richiesta e presente", func(v *Verdict) {}, true, ""},
		{"ok senza licenza se non richiesta", func(v *Verdict) { v.LicensingVerdict = "UNLICENSED" }, false, ""},
		{"ok timestamp 4m59 nel passato", func(v *Verdict) { v.TimestampMillis = testNow.Add(-4*time.Minute - 59*time.Second).UnixMilli() }, false, ""},

		{"requestPackageName diverso", func(v *Verdict) { v.RequestPackageName = "evil.app" }, false, "requestPackageName"},
		{"requestHash diverso", func(v *Verdict) { v.RequestHash = "OTHER" }, false, "requestHash"},
		{"timestamp troppo vecchio", func(v *Verdict) { v.TimestampMillis = testNow.Add(-6 * time.Minute).UnixMilli() }, false, "timestamp"},
		{"timestamp nel futuro", func(v *Verdict) { v.TimestampMillis = testNow.Add(6 * time.Minute).UnixMilli() }, false, "timestamp"},
		{"timestamp assente", func(v *Verdict) { v.TimestampMillis = 0 }, false, "timestamp"},
		{"app non riconosciuta", func(v *Verdict) { v.AppRecognitionVerdict = "UNRECOGNIZED_VERSION" }, false, "appRecognitionVerdict"},
		{"app senza verdetto", func(v *Verdict) { v.AppRecognitionVerdict = "" }, false, "appRecognitionVerdict"},
		{"package app diverso", func(v *Verdict) { v.AppPackageName = "evil.app" }, false, "packageName"},
		{"firma sconosciuta", func(v *Verdict) { v.CertSHA256Digests = []string{digestOf(9)} }, false, "certificateSha256Digest"},
		{"nessuna firma", func(v *Verdict) { v.CertSHA256Digests = nil }, false, "certificateSha256Digest"},
		{"solo basic integrity", func(v *Verdict) { v.DeviceVerdicts = []string{"MEETS_BASIC_INTEGRITY"} }, false, "MEETS_DEVICE_INTEGRITY"},
		{"nessun verdetto dispositivo", func(v *Verdict) { v.DeviceVerdicts = nil }, false, "MEETS_DEVICE_INTEGRITY"},
		{"licenza richiesta e assente", func(v *Verdict) { v.LicensingVerdict = "UNLICENSED" }, true, "LICENSED"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := newVerifier(nil)
			v.RequireLicensed = c.licence
			verdict := goodVerdict(hash)
			c.mutate(verdict)
			err := v.CheckVerdict(verdict, hash)
			switch {
			case c.wantErr == "" && err != nil:
				t.Fatalf("atteso ok, errore: %v", err)
			case c.wantErr != "" && err == nil:
				t.Fatalf("atteso errore con %q, ok", c.wantErr)
			case c.wantErr != "" && !strings.Contains(err.Error(), c.wantErr):
				t.Fatalf("errore %q senza %q", err, c.wantErr)
			}
		})
	}
}

func TestCheckVerdictNil(t *testing.T) {
	if err := newVerifier(nil).CheckVerdict(nil, "h"); err == nil {
		t.Fatal("payload nil deve fallire")
	}
}

// Verify chiama il decoder con package e token, e fallisce chiuso se il
// decode fallisce o se manca la config.
func TestVerify(t *testing.T) {
	ctx := context.Background()

	d := &fakeDecoder{verdict: goodVerdict("H")}
	if err := newVerifier(d).Verify(ctx, "tok", "H"); err != nil {
		t.Fatal(err)
	}
	if d.gotPkg != testPkg || d.gotTok != "tok" {
		t.Errorf("decoder chiamato con (%q, %q)", d.gotPkg, d.gotTok)
	}

	if err := newVerifier(&fakeDecoder{err: errors.New("boom")}).Verify(ctx, "tok", "H"); err == nil {
		t.Error("errore di decode deve fallire")
	}
	if err := newVerifier(nil).Verify(ctx, "tok", "H"); err == nil || !strings.Contains(err.Error(), "CREDENTIALS") {
		t.Errorf("senza decoder deve fallire spiegando la config: %v", err)
	}
	noDigests := newVerifier(d)
	noDigests.CertDigests = nil
	if err := noDigests.Verify(ctx, "tok", "H"); err == nil || !strings.Contains(err.Error(), "CERT_SHA256") {
		t.Errorf("senza digest deve fallire spiegando la config: %v", err)
	}
}

func TestCredentialsBytes(t *testing.T) {
	js := `{"type":"service_account"}`
	if b, err := CredentialsBytes(js); err != nil || string(b) != js {
		t.Errorf("JSON grezzo: %q %v", b, err)
	}
	if b, err := CredentialsBytes(base64.StdEncoding.EncodeToString([]byte(js))); err != nil || string(b) != js {
		t.Errorf("base64: %q %v", b, err)
	}
	for _, bad := range []string{"", "   ", "!!!", base64.StdEncoding.EncodeToString([]byte("non json"))} {
		if _, err := CredentialsBytes(bad); err == nil {
			t.Errorf("%q doveva fallire", bad)
		}
	}
	if NewGoogleDecoder("  ") != nil {
		t.Error("senza credenziali il decoder deve essere nil")
	}
}

// Credenziali inutilizzabili: il boot non fallisce, il decode si': chiuso.
func TestGoogleDecoderBadCredentials(t *testing.T) {
	d := NewGoogleDecoder("non-json-ne-base64!!")
	if _, err := d.Decode(context.Background(), testPkg, "tok"); err == nil {
		t.Fatal("credenziali invalide devono dare errore")
	}
}
