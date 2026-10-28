package integrity

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"google.golang.org/api/option"
	"google.golang.org/api/playintegrity/v1"
)

// MaxTokenAge e` quanto puo` distare dall'ora del server il timestamp del
// token Play Integrity (requestDetails.timestampMillis).
const MaxTokenAge = 5 * time.Minute

// Verdict e` il sottoinsieme del payload decodificato di Play Integrity che ci
// serve. E` un tipo nostro e non quello della libreria Google cosi` il decode
// (rete + credenziali) sta dietro TokenDecoder e i controlli sul verdetto si
// provano con payload finti.
type Verdict struct {
	RequestPackageName string
	RequestHash        string
	TimestampMillis    int64

	AppRecognitionVerdict string
	AppPackageName        string
	CertSHA256Digests     []string

	DeviceVerdicts []string

	LicensingVerdict string
}

// TokenDecoder decodifica un token Play Integrity (decodeIntegrityToken).
type TokenDecoder interface {
	Decode(ctx context.Context, packageName, token string) (*Verdict, error)
}

// AndroidVerifier controlla il verdetto di Play Integrity contro la config.
type AndroidVerifier struct {
	Decoder         TokenDecoder
	PackageName     string
	CertDigests     []string // gia` normalizzati (base64url senza padding)
	RequireLicensed bool
	Now             func() time.Time
}

// Verify decodifica il token e controlla, nell'ordine, pacchetto della
// richiesta, requestHash, freschezza, riconoscimento dell'app, pacchetto e
// firma dell'app, integrita` del dispositivo e (se richiesto) la licenza.
// expectedHash e` il requestHash ricalcolato dal server per QUESTA richiesta.
func (v *AndroidVerifier) Verify(ctx context.Context, token, expectedHash string) error {
	if v.Decoder == nil {
		return errors.New("play integrity non configurato (PLAY_INTEGRITY_CREDENTIALS_JSON)")
	}
	if len(v.CertDigests) == 0 {
		return errors.New("play integrity non configurato (PLAY_INTEGRITY_CERT_SHA256 vuota o non valida)")
	}
	verdict, err := v.Decoder.Decode(ctx, v.PackageName, token)
	if err != nil {
		return fmt.Errorf("decodeIntegrityToken fallita: %w", err)
	}
	return v.CheckVerdict(verdict, expectedHash)
}

// CheckVerdict applica i controlli di contratto a un verdetto gia` decodificato.
func (v *AndroidVerifier) CheckVerdict(d *Verdict, expectedHash string) error {
	if d == nil {
		return errors.New("payload vuoto")
	}
	if d.RequestPackageName != v.PackageName {
		return fmt.Errorf("requestPackageName %q diverso da %q", d.RequestPackageName, v.PackageName)
	}
	if d.RequestHash != expectedHash {
		return errors.New("requestHash non corrisponde alla richiesta (metodo, path o challenge diversi)")
	}

	now := time.Now()
	if v.Now != nil {
		now = v.Now()
	}
	age := now.Sub(time.UnixMilli(d.TimestampMillis))
	if age < 0 {
		age = -age
	}
	if d.TimestampMillis == 0 || age > MaxTokenAge {
		return fmt.Errorf("timestamp del token fuori finestra (scarto %s)", age.Round(time.Second))
	}

	if d.AppRecognitionVerdict != "PLAY_RECOGNIZED" {
		return fmt.Errorf("appRecognitionVerdict %q", d.AppRecognitionVerdict)
	}
	if d.AppPackageName != v.PackageName {
		return fmt.Errorf("appIntegrity.packageName %q diverso da %q", d.AppPackageName, v.PackageName)
	}
	if !v.hasKnownCert(d.CertSHA256Digests) {
		return errors.New("certificateSha256Digest non fra quelli configurati")
	}
	if !slices.Contains(d.DeviceVerdicts, "MEETS_DEVICE_INTEGRITY") {
		return fmt.Errorf("deviceRecognitionVerdict %v senza MEETS_DEVICE_INTEGRITY", d.DeviceVerdicts)
	}
	if v.RequireLicensed && d.LicensingVerdict != "LICENSED" {
		return fmt.Errorf("appLicensingVerdict %q (richiesto LICENSED)", d.LicensingVerdict)
	}
	return nil
}

func (v *AndroidVerifier) hasKnownCert(reported []string) bool {
	for _, r := range reported {
		n, err := NormalizeCertDigest(r)
		if err != nil {
			continue
		}
		if slices.Contains(v.CertDigests, n) {
			return true
		}
	}
	return false
}

// GoogleDecoder e` il TokenDecoder reale: chiama
// playintegrity.googleapis.com con il service account configurato. Il client
// si crea al primo uso, non all'avvio: una credenziale sbagliata non deve
// impedire il boot (vale per tutta la feature), e il problema esce nei log di
// chi ha provato a usarla.
type GoogleDecoder struct {
	CredentialsJSON string

	once sync.Once
	svc  *playintegrity.Service
	err  error
}

// NewGoogleDecoder ritorna nil se non ci sono credenziali.
func NewGoogleDecoder(credentials string) *GoogleDecoder {
	if strings.TrimSpace(credentials) == "" {
		return nil
	}
	return &GoogleDecoder{CredentialsJSON: credentials}
}

func (g *GoogleDecoder) Decode(ctx context.Context, packageName, token string) (*Verdict, error) {
	g.once.Do(func() {
		raw, err := CredentialsBytes(g.CredentialsJSON)
		if err != nil {
			g.err = err
			return
		}
		g.svc, g.err = playintegrity.NewService(context.Background(),
			option.WithAuthCredentialsJSON(option.ServiceAccount, raw),
			option.WithScopes(playintegrity.PlayintegrityScope),
		)
	})
	if g.err != nil {
		return nil, fmt.Errorf("client Play Integrity non inizializzabile: %w", g.err)
	}

	resp, err := g.svc.V1.DecodeIntegrityToken(packageName, &playintegrity.DecodeIntegrityTokenRequest{
		IntegrityToken: token,
	}).Context(ctx).Do()
	if err != nil {
		return nil, err
	}
	return verdictFromPayload(resp.TokenPayloadExternal), nil
}

// verdictFromPayload appiattisce il payload Google (tutti i puntatori sono
// opzionali) in un Verdict.
func verdictFromPayload(p *playintegrity.TokenPayloadExternal) *Verdict {
	if p == nil {
		return nil
	}
	v := &Verdict{}
	if p.RequestDetails != nil {
		v.RequestPackageName = p.RequestDetails.RequestPackageName
		v.RequestHash = p.RequestDetails.RequestHash
		v.TimestampMillis = p.RequestDetails.TimestampMillis
	}
	if p.AppIntegrity != nil {
		v.AppRecognitionVerdict = p.AppIntegrity.AppRecognitionVerdict
		v.AppPackageName = p.AppIntegrity.PackageName
		v.CertSHA256Digests = p.AppIntegrity.CertificateSha256Digest
	}
	if p.DeviceIntegrity != nil {
		v.DeviceVerdicts = p.DeviceIntegrity.DeviceRecognitionVerdict
	}
	if p.AccountDetails != nil {
		v.LicensingVerdict = p.AccountDetails.AppLicensingVerdict
	}
	return v
}

// CredentialsBytes accetta il JSON del service account "grezzo" (inizia per
// '{') oppure in base64 standard, come FIREBASE_AUTH_KEY.
func CredentialsBytes(raw string) ([]byte, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil, errors.New("credenziali vuote")
	}
	if strings.HasPrefix(s, "{") {
		return []byte(s), nil
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("ne` JSON ne` base64: %w", err)
	}
	if !strings.HasPrefix(strings.TrimSpace(string(b)), "{") {
		return nil, errors.New("il base64 non contiene un JSON")
	}
	return b, nil
}
