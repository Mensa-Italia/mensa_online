package integrity

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	_ "embed"
	"encoding/asn1"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/fxamacker/cbor/v2"
)

// appleAppAttestationRootPEM e` la Apple App Attestation Root CA
// (CN=Apple App Attestation Root CA, ECDSA P-384, valida fino al 15/03/2045),
// scaricata da https://www.apple.com/certificateauthority/private/
// (file: https://www.apple.com/certificateauthority/Apple_App_Attestation_Root_CA.pem).
// SHA-256 del certificato:
// 1C:B9:82:3B:A2:8B:A6:AD:2D:33:A0:06:94:1D:E2:AE:4F:51:3E:F1:D4:E8:31:B9:F7:E0:FA:7B:62:42:C9:32
//
//go:embed apple_app_attestation_root_ca.pem
var appleAppAttestationRootPEM []byte

// oidAppAttestNonce e` l'estensione del certificato foglia che contiene il
// nonce: SEQUENCE { [1] EXPLICIT OCTET STRING }.
var oidAppAttestNonce = asn1.ObjectIdentifier{1, 2, 840, 113635, 100, 8, 2}

// AAGUID (16 byte) dell'attestazione: "appattest" + 7 byte a zero in
// produzione, "appattestdevelop" con l'entitlement di sviluppo.
var (
	aaguidProduction  = append([]byte("appattest"), make([]byte, 7)...)
	aaguidDevelopment = []byte("appattestdevelop")
)

const (
	envProduction  = "production"
	envDevelopment = "development"

	// authData: rpIdHash(32) | flags(1) | counter(4, big endian)
	authDataHeaderLen = 37
)

// ErrKeyNotFound: la chiave App Attest non e` registrata.
var ErrKeyNotFound = errors.New("chiave App Attest non trovata")

// ErrCounterNotAdvanced: il contatore dell'asserzione e` gia` stato usato o
// e` fuori dalla finestra di CounterWindowSize (replay).
var ErrCounterNotAdvanced = errors.New("contatore App Attest non avanzato")

// Key e` una chiave App Attest registrata per un utente.
type Key struct {
	KeyID       string // base64 standard del keyId
	UserID      string
	PublicKey   *ecdsa.PublicKey
	Counter     uint32
	Environment string
}

// KeyStore e` la persistenza delle chiavi (in produzione la collection
// superuser-only app_attest_keys, nei test una mappa).
type KeyStore interface {
	// Find ritorna ErrKeyNotFound se la chiave non esiste.
	Find(keyID string) (*Key, error)
	Create(k *Key) error
	// AdvanceCounter accetta `counter` se CounterWindow lo accetta (maggiore
	// del massimo salvato, oppure dentro la finestra e mai usato), in modo
	// atomico, e salva il nuovo massimo; altrimenti ErrCounterNotAdvanced.
	AdvanceCounter(keyID string, counter uint32) error
}

// AppAttest verifica attestazioni e asserzioni di App Attest.
type AppAttest struct {
	AppID       string
	Environment string
	Roots       *x509.CertPool // default: Apple App Attestation Root CA
	Keys        KeyStore
	Now         func() time.Time
}

// AppleRoots ritorna il pool con la sola Apple App Attestation Root CA.
func AppleRoots() *x509.CertPool {
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(appleAppAttestationRootPEM) {
		// Il PEM e` embedded e coperto da test: se arriva qui il binario e`
		// rotto, e un pool vuoto fa fallire chiuse tutte le attestazioni.
		return x509.NewCertPool()
	}
	return pool
}

func (a *AppAttest) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a *AppAttest) roots() *x509.CertPool {
	if a.Roots != nil {
		return a.Roots
	}
	return AppleRoots()
}

func (a *AppAttest) expectedAAGUID() []byte {
	if a.Environment == envDevelopment {
		return aaguidDevelopment
	}
	return aaguidProduction
}

type attestationObject struct {
	Fmt      string  `cbor:"fmt"`
	AttStmt  attStmt `cbor:"attStmt"`
	AuthData []byte  `cbor:"authData"`
}

type attStmt struct {
	X5C     [][]byte `cbor:"x5c"`
	Receipt []byte   `cbor:"receipt"`
}

// VerifyAttestation controlla un'attestazione di App Attest (i passi della
// documentazione Apple "Validating apps that connect to your server") e
// ritorna la chiave pubblica da salvare. keyID e` il keyId (byte grezzi) e
// clientDataHash l'hash passato dal client ad attestKey.
func (a *AppAttest) VerifyAttestation(keyID, attestation, clientDataHash []byte) (*ecdsa.PublicKey, error) {
	var obj attestationObject
	if err := cbor.Unmarshal(attestation, &obj); err != nil {
		return nil, fmt.Errorf("attestazione CBOR non valida: %w", err)
	}
	if obj.Fmt != "apple-appattest" {
		return nil, fmt.Errorf("fmt %q diverso da apple-appattest", obj.Fmt)
	}
	if len(obj.AttStmt.X5C) < 1 {
		return nil, errors.New("x5c vuoto")
	}

	// 1. Catena x5c fino alla root Apple (verificata all'ora del server: i
	//    certificati foglia di App Attest durano pochi giorni).
	leaf, err := x509.ParseCertificate(obj.AttStmt.X5C[0])
	if err != nil {
		return nil, fmt.Errorf("certificato foglia non valido: %w", err)
	}
	intermediates := x509.NewCertPool()
	for _, der := range obj.AttStmt.X5C[1:] {
		c, err := x509.ParseCertificate(der)
		if err != nil {
			return nil, fmt.Errorf("certificato intermedio non valido: %w", err)
		}
		intermediates.AddCert(c)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots:         a.roots(),
		Intermediates: intermediates,
		CurrentTime:   a.now(),
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	}); err != nil {
		return nil, fmt.Errorf("catena x5c non valida: %w", err)
	}

	// 2. nonce = SHA256(authData || clientDataHash) nell'estensione.
	nonce := sha256.Sum256(append(append([]byte{}, obj.AuthData...), clientDataHash...))
	certNonce, err := extractNonce(leaf)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(certNonce, nonce[:]) {
		return nil, errors.New("nonce del certificato non corrisponde (challenge diversa)")
	}

	// 3. SHA256(chiave pubblica non compressa) == keyId.
	pub, ok := leaf.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, errors.New("la chiave del certificato non e` ECDSA")
	}
	point, err := uncompressedPoint(pub)
	if err != nil {
		return nil, err
	}
	if h := sha256.Sum256(point); !bytes.Equal(h[:], keyID) {
		return nil, errors.New("SHA256 della chiave pubblica diverso dal keyId")
	}

	// 4. authData: rpIdHash, counter == 0, aaguid, credentialId == keyId.
	if len(obj.AuthData) < 55 {
		return nil, errors.New("authData troppo corto")
	}
	if err := a.checkRPID(obj.AuthData); err != nil {
		return nil, err
	}
	if c := binary.BigEndian.Uint32(obj.AuthData[33:37]); c != 0 {
		return nil, fmt.Errorf("counter %d diverso da 0", c)
	}
	if !bytes.Equal(obj.AuthData[37:53], a.expectedAAGUID()) {
		return nil, fmt.Errorf("aaguid %q non ammesso in ambiente %s", obj.AuthData[37:53], a.envName())
	}
	credLen := int(binary.BigEndian.Uint16(obj.AuthData[53:55]))
	if len(obj.AuthData) < 55+credLen {
		return nil, errors.New("authData troncato (credentialId)")
	}
	if !bytes.Equal(obj.AuthData[55:55+credLen], keyID) {
		return nil, errors.New("credentialId diverso dal keyId")
	}
	return pub, nil
}

type assertionObject struct {
	Signature         []byte `cbor:"signature"`
	AuthenticatorData []byte `cbor:"authenticatorData"`
}

// VerifyAssertion controlla un'asserzione per-richiesta e avanza il
// contatore. La chiave deve esistere ed essere dell'utente: altrimenti
// ritorna ErrKeyNotFound, che il chiamante traduce in 428 attest_key_required.
func (a *AppAttest) VerifyAssertion(userID, keyID string, assertion, clientDataHash []byte) error {
	key, err := a.Keys.Find(keyID)
	if err != nil {
		return err
	}
	// Chiave di un altro utente: per il client e` come se non esistesse.
	if key.UserID != userID {
		return ErrKeyNotFound
	}

	var obj assertionObject
	if err := cbor.Unmarshal(assertion, &obj); err != nil {
		return fmt.Errorf("asserzione CBOR non valida: %w", err)
	}
	if len(obj.AuthenticatorData) < authDataHeaderLen {
		return errors.New("authenticatorData troppo corto")
	}

	nonce := sha256.Sum256(append(append([]byte{}, obj.AuthenticatorData...), clientDataHash...))
	// ES256: la firma e` sull'hash del nonce, quindi SHA256(nonce).
	digest := sha256.Sum256(nonce[:])
	if !ecdsa.VerifyASN1(key.PublicKey, digest[:], obj.Signature) {
		return errors.New("firma dell'asserzione non valida")
	}

	if err := a.checkRPID(obj.AuthenticatorData); err != nil {
		return err
	}
	counter := binary.BigEndian.Uint32(obj.AuthenticatorData[33:37])
	// Niente confronto con key.Counter qui: le asserzioni concorrenti arrivano
	// in ordine sparso, la regola e` quella di CounterWindow e la applica
	// AdvanceCounter sotto lock.
	if err := a.Keys.AdvanceCounter(keyID, counter); err != nil {
		if errors.Is(err, ErrCounterNotAdvanced) {
			return fmt.Errorf("counter %d gia` usato o fuori finestra (salvato %d): %w", counter, key.Counter, err)
		}
		return err
	}
	return nil
}

func (a *AppAttest) checkRPID(authData []byte) error {
	want := sha256.Sum256([]byte(a.AppID))
	if !bytes.Equal(authData[:32], want[:]) {
		return errors.New("rpIdHash diverso da SHA256(APP_ATTEST_APP_ID)")
	}
	return nil
}

func (a *AppAttest) envName() string {
	if a.Environment == envDevelopment {
		return envDevelopment
	}
	return envProduction
}

type nonceExtension struct {
	Nonce []byte `asn1:"tag:1,explicit"`
}

func extractNonce(cert *x509.Certificate) ([]byte, error) {
	for _, ext := range cert.Extensions {
		if !ext.Id.Equal(oidAppAttestNonce) {
			continue
		}
		var v nonceExtension
		rest, err := asn1.Unmarshal(ext.Value, &v)
		if err != nil || len(rest) != 0 {
			return nil, errors.New("estensione nonce malformata")
		}
		return v.Nonce, nil
	}
	return nil, errors.New("estensione nonce assente nel certificato")
}

func uncompressedPoint(pub *ecdsa.PublicKey) ([]byte, error) {
	k, err := pub.ECDH()
	if err != nil {
		return nil, fmt.Errorf("chiave pubblica non valida: %w", err)
	}
	return k.Bytes(), nil
}

// nonceExtensionFor costruisce l'estensione nonce: serve ai test per forgiare
// certificati foglia sintetici.
func nonceExtensionFor(nonce []byte) (pkix.Extension, error) {
	der, err := asn1.Marshal(nonceExtension{Nonce: nonce})
	if err != nil {
		return pkix.Extension{}, err
	}
	return pkix.Extension{Id: oidAppAttestNonce, Value: der}, nil
}

// MarshalPublicKey / ParsePublicKey: serializzazione della chiave per lo
// store (DER PKIX).
func MarshalPublicKey(pub *ecdsa.PublicKey) ([]byte, error) { return x509.MarshalPKIXPublicKey(pub) }

func ParsePublicKey(der []byte) (*ecdsa.PublicKey, error) {
	k, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, err
	}
	pub, ok := k.(*ecdsa.PublicKey)
	if !ok {
		return nil, errors.New("la chiave salvata non e` ECDSA")
	}
	return pub, nil
}
