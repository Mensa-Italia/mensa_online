package integrity

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/pem"
	"math/big"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
)

const (
	testAppID = "6WA5D3RJBU.it.mensa.app"
	testUser  = "user1"
)

// --- key store in memoria --------------------------------------------------

type memKeys struct {
	mu     sync.Mutex
	keys   map[string]*Key
	window CounterWindow
}

func newMemKeys() *memKeys { return &memKeys{keys: map[string]*Key{}} }

func (m *memKeys) Find(id string) (*Key, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k, ok := m.keys[id]
	if !ok {
		return nil, ErrKeyNotFound
	}
	cp := *k
	return &cp, nil
}

func (m *memKeys) Create(k *Key) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *k
	m.keys[k.KeyID] = &cp
	return nil
}

func (m *memKeys) AdvanceCounter(id string, c uint32) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k, ok := m.keys[id]
	if !ok {
		return ErrKeyNotFound
	}
	next, accepted := m.window.Accept(id, k.Counter, c)
	if !accepted {
		return ErrCounterNotAdvanced
	}
	k.Counter = next
	return nil
}

// --- PKI sintetica: root -> intermedia -> foglia ---------------------------

type pki struct {
	rootPool  *x509.CertPool
	rootCert  *x509.Certificate
	rootKey   *ecdsa.PrivateKey
	interCert *x509.Certificate
	interKey  *ecdsa.PrivateKey
}

func mustKey(t *testing.T, c elliptic.Curve) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(c, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func newPKI(t *testing.T) *pki {
	t.Helper()
	p := &pki{rootKey: mustKey(t, elliptic.P384()), interKey: mustKey(t, elliptic.P384())}

	rootTpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test App Attestation Root CA"},
		NotBefore:             time.Now().Add(-24 * time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, rootTpl, rootTpl, &p.rootKey.PublicKey, p.rootKey)
	if err != nil {
		t.Fatal(err)
	}
	p.rootCert, _ = x509.ParseCertificate(der)
	p.rootPool = x509.NewCertPool()
	p.rootPool.AddCert(p.rootCert)

	interTpl := &x509.Certificate{
		SerialNumber:          big.NewInt(2),
		Subject:               pkix.Name{CommonName: "Test App Attestation CA 1"},
		NotBefore:             time.Now().Add(-24 * time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err = x509.CreateCertificate(rand.Reader, interTpl, p.rootCert, &p.interKey.PublicKey, p.rootKey)
	if err != nil {
		t.Fatal(err)
	}
	p.interCert, _ = x509.ParseCertificate(der)
	return p
}

// leaf emette un certificato foglia con l'estensione nonce.
func (p *pki) leaf(t *testing.T, devKey *ecdsa.PrivateKey, nonce []byte, notAfter time.Time) []byte {
	t.Helper()
	ext, err := nonceExtensionFor(nonce)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{
		SerialNumber:    big.NewInt(3),
		Subject:         pkix.Name{CommonName: "device"},
		NotBefore:       time.Now().Add(-time.Hour),
		NotAfter:        notAfter,
		ExtraExtensions: []pkix.Extension{ext},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, p.interCert, &devKey.PublicKey, p.interKey)
	if err != nil {
		t.Fatal(err)
	}
	return der
}

// --- costruzione di attestazioni e asserzioni ------------------------------

func rpHash(appID string) []byte {
	h := sha256.Sum256([]byte(appID))
	return h[:]
}

func attestAuthData(appID string, counter uint32, aaguid, credID []byte) []byte {
	ad := append([]byte{}, rpHash(appID)...)
	ad = append(ad, 0x40) // flags: attested credential data
	ad = binary.BigEndian.AppendUint32(ad, counter)
	ad = append(ad, aaguid...)
	ad = binary.BigEndian.AppendUint16(ad, uint16(len(credID)))
	ad = append(ad, credID...)
	return append(ad, 0xa5, 0x01, 0x02) // finto inizio della chiave COSE
}

func keyIDOf(t *testing.T, pub *ecdsa.PublicKey) []byte {
	t.Helper()
	pt, err := uncompressedPoint(pub)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(pt)
	return h[:]
}

type attestOpts struct {
	appID      string
	aaguid     []byte
	counter    uint32
	nonceFor   string // challenge usata per il nonce del certificato
	credID     []byte // nil = keyID
	keyIDOverr []byte
	notAfter   time.Time
	signFormat string
}

type attestResult struct {
	keyID       []byte
	attestation []byte
	devKey      *ecdsa.PrivateKey
}

func (p *pki) attest(t *testing.T, challenge string, o attestOpts) attestResult {
	t.Helper()
	if o.appID == "" {
		o.appID = testAppID
	}
	if o.aaguid == nil {
		o.aaguid = aaguidProduction
	}
	if o.nonceFor == "" {
		o.nonceFor = challenge
	}
	if o.notAfter.IsZero() {
		o.notAfter = time.Now().Add(time.Hour)
	}
	if o.signFormat == "" {
		o.signFormat = "apple-appattest"
	}

	dev := mustKey(t, elliptic.P256())
	keyID := keyIDOf(t, &dev.PublicKey)
	credID := o.credID
	if credID == nil {
		credID = keyID
	}
	authData := attestAuthData(o.appID, o.counter, o.aaguid, credID)

	cdh := ClientDataHash(o.nonceFor)
	nonce := sha256.Sum256(append(append([]byte{}, authData...), cdh...))

	leafDER := p.leaf(t, dev, nonce[:], o.notAfter)
	obj, err := cbor.Marshal(attestationObject{
		Fmt:      o.signFormat,
		AttStmt:  attStmt{X5C: [][]byte{leafDER, p.interCert.Raw}, Receipt: []byte("receipt")},
		AuthData: authData,
	})
	if err != nil {
		t.Fatal(err)
	}
	if o.keyIDOverr != nil {
		keyID = o.keyIDOverr
	}
	return attestResult{keyID: keyID, attestation: obj, devKey: dev}
}

func newAppAttest(p *pki, env string) *AppAttest {
	return &AppAttest{AppID: testAppID, Environment: env, Roots: p.rootPool, Keys: newMemKeys()}
}

// --- attestazione ----------------------------------------------------------

func TestVerifyAttestation(t *testing.T) {
	p := newPKI(t)
	const ch = "challenge-1"
	cdh := ClientDataHash(ch)

	t.Run("ok produzione", func(t *testing.T) {
		r := p.attest(t, ch, attestOpts{})
		pub, err := newAppAttest(p, "production").VerifyAttestation(r.keyID, r.attestation, cdh)
		if err != nil {
			t.Fatal(err)
		}
		if !pub.Equal(&r.devKey.PublicKey) {
			t.Error("chiave restituita diversa dalla foglia")
		}
	})

	t.Run("ok sviluppo", func(t *testing.T) {
		r := p.attest(t, ch, attestOpts{aaguid: aaguidDevelopment})
		if _, err := newAppAttest(p, "development").VerifyAttestation(r.keyID, r.attestation, cdh); err != nil {
			t.Fatal(err)
		}
	})

	cases := []struct {
		name    string
		env     string
		opts    attestOpts
		cdh     []byte
		wantErr string
	}{
		{"aaguid sviluppo in produzione", "production", attestOpts{aaguid: aaguidDevelopment}, cdh, "aaguid"},
		{"aaguid produzione in sviluppo", "development", attestOpts{aaguid: aaguidProduction}, cdh, "aaguid"},
		{"rpIdHash di un'altra app", "production", attestOpts{appID: "XXXXXXXXXX.evil.app"}, cdh, "rpIdHash"},
		{"counter diverso da zero", "production", attestOpts{counter: 3}, cdh, "counter"},
		{"challenge diversa dal nonce", "production", attestOpts{nonceFor: "altra"}, cdh, "nonce"},
		{"clientDataHash diverso", "production", attestOpts{}, ClientDataHash("altra"), "nonce"},
		{"keyId non corrisponde", "production", attestOpts{keyIDOverr: make([]byte, 32)}, cdh, "keyId"},
		{"credentialId diverso", "production", attestOpts{credID: make([]byte, 32)}, cdh, "credentialId"},
		{"certificato scaduto", "production", attestOpts{notAfter: time.Now().Add(-time.Minute)}, cdh, "catena"},
		{"fmt sbagliato", "production", attestOpts{signFormat: "packed"}, cdh, "fmt"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := p.attest(t, ch, c.opts)
			_, err := newAppAttest(p, c.env).VerifyAttestation(r.keyID, r.attestation, c.cdh)
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("errore %v, atteso con %q", err, c.wantErr)
			}
		})
	}

	t.Run("root non fidata", func(t *testing.T) {
		other := newPKI(t)
		r := other.attest(t, ch, attestOpts{})
		if _, err := newAppAttest(p, "production").VerifyAttestation(r.keyID, r.attestation, cdh); err == nil {
			t.Fatal("una catena di un'altra root non deve passare")
		}
	})

	t.Run("senza intermedia la catena non si chiude", func(t *testing.T) {
		r := p.attest(t, ch, attestOpts{})
		var obj attestationObject
		if err := cbor.Unmarshal(r.attestation, &obj); err != nil {
			t.Fatal(err)
		}
		obj.AttStmt.X5C = obj.AttStmt.X5C[:1]
		bad, _ := cbor.Marshal(obj)
		if _, err := newAppAttest(p, "production").VerifyAttestation(r.keyID, bad, cdh); err == nil {
			t.Fatal("foglia senza intermedia deve fallire")
		}
	})

	t.Run("spazzatura", func(t *testing.T) {
		if _, err := newAppAttest(p, "production").VerifyAttestation(make([]byte, 32), []byte("non cbor"), cdh); err == nil {
			t.Fatal("CBOR invalido deve fallire")
		}
		empty, _ := cbor.Marshal(attestationObject{Fmt: "apple-appattest"})
		if _, err := newAppAttest(p, "production").VerifyAttestation(make([]byte, 32), empty, cdh); err == nil {
			t.Fatal("x5c vuoto deve fallire")
		}
	})
}

// --- asserzione ------------------------------------------------------------

func assertionAuthData(appID string, counter uint32) []byte {
	ad := append([]byte{}, rpHash(appID)...)
	ad = append(ad, 0x00)
	return binary.BigEndian.AppendUint32(ad, counter)
}

func makeAssertion(t *testing.T, key *ecdsa.PrivateKey, authData, cdh []byte) []byte {
	t.Helper()
	nonce := sha256.Sum256(append(append([]byte{}, authData...), cdh...))
	digest := sha256.Sum256(nonce[:])
	sig, err := ecdsa.SignASN1(rand.Reader, key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	out, err := cbor.Marshal(assertionObject{Signature: sig, AuthenticatorData: authData})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func registerKey(t *testing.T, aa *AppAttest, userID, keyID string, counter uint32) *ecdsa.PrivateKey {
	t.Helper()
	dev := mustKey(t, elliptic.P256())
	if err := aa.Keys.Create(&Key{KeyID: keyID, UserID: userID, PublicKey: &dev.PublicKey, Counter: counter}); err != nil {
		t.Fatal(err)
	}
	return dev
}

func TestVerifyAssertion(t *testing.T) {
	p := newPKI(t)
	cdh := ClientDataHash(ClientData("ch", "GET", "/api/cs/me"))

	t.Run("ok e il counter avanza", func(t *testing.T) {
		aa := newAppAttest(p, "production")
		dev := registerKey(t, aa, testUser, "k1", 0)
		if err := aa.VerifyAssertion(testUser, "k1", makeAssertion(t, dev, assertionAuthData(testAppID, 1), cdh), cdh); err != nil {
			t.Fatal(err)
		}
		k, _ := aa.Keys.Find("k1")
		if k.Counter != 1 {
			t.Errorf("counter salvato %d, atteso 1", k.Counter)
		}
		if err := aa.VerifyAssertion(testUser, "k1", makeAssertion(t, dev, assertionAuthData(testAppID, 5), cdh), cdh); err != nil {
			t.Fatalf("counter 5 dopo 1: %v", err)
		}
	})

	t.Run("asserzioni concorrenti in ordine sparso", func(t *testing.T) {
		aa := newAppAttest(p, "production")
		dev := registerKey(t, aa, testUser, "k1", 0)
		for _, c := range []uint32{3, 1, 2} {
			if err := aa.VerifyAssertion(testUser, "k1", makeAssertion(t, dev, assertionAuthData(testAppID, c), cdh), cdh); err != nil {
				t.Errorf("counter %d dopo uno piu` alto: %v", c, err)
			}
		}
		if k, _ := aa.Keys.Find("k1"); k.Counter != 3 {
			t.Errorf("il massimo salvato resta il piu` alto: %d", k.Counter)
		}
	})

	t.Run("replay con counter gia` usato o troppo vecchio", func(t *testing.T) {
		aa := newAppAttest(p, "production")
		dev := registerKey(t, aa, testUser, "k1", 4)
		if err := aa.VerifyAssertion(testUser, "k1", makeAssertion(t, dev, assertionAuthData(testAppID, 100), cdh), cdh); err != nil {
			t.Fatal(err)
		}
		// 100 appena usato; 4 e` piu` vecchio della finestra; 0 non e` mai valido.
		for _, c := range []uint32{100, 4, 0} {
			err := aa.VerifyAssertion(testUser, "k1", makeAssertion(t, dev, assertionAuthData(testAppID, c), cdh), cdh)
			if err == nil || !strings.Contains(err.Error(), "counter") {
				t.Errorf("counter %d: errore %v", c, err)
			}
		}
		if k, _ := aa.Keys.Find("k1"); k.Counter != 100 {
			t.Errorf("il counter non deve cambiare su un rifiuto: %d", k.Counter)
		}
	})

	t.Run("stessa asserzione due volte", func(t *testing.T) {
		aa := newAppAttest(p, "production")
		dev := registerKey(t, aa, testUser, "k1", 0)
		a := makeAssertion(t, dev, assertionAuthData(testAppID, 1), cdh)
		if err := aa.VerifyAssertion(testUser, "k1", a, cdh); err != nil {
			t.Fatal(err)
		}
		if err := aa.VerifyAssertion(testUser, "k1", a, cdh); err == nil {
			t.Fatal("il replay della stessa asserzione deve fallire")
		}
	})

	t.Run("firma di un'altra chiave", func(t *testing.T) {
		aa := newAppAttest(p, "production")
		registerKey(t, aa, testUser, "k1", 0)
		other := mustKey(t, elliptic.P256())
		err := aa.VerifyAssertion(testUser, "k1", makeAssertion(t, other, assertionAuthData(testAppID, 1), cdh), cdh)
		if err == nil || !strings.Contains(err.Error(), "firma") {
			t.Fatalf("errore %v", err)
		}
	})

	t.Run("clientDataHash diverso (altro path/metodo)", func(t *testing.T) {
		aa := newAppAttest(p, "production")
		dev := registerKey(t, aa, testUser, "k1", 0)
		a := makeAssertion(t, dev, assertionAuthData(testAppID, 1), cdh)
		otherCDH := ClientDataHash(ClientData("ch", "POST", "/api/cs/me"))
		if err := aa.VerifyAssertion(testUser, "k1", a, otherCDH); err == nil {
			t.Fatal("asserzione per un'altra richiesta deve fallire")
		}
	})

	t.Run("rpIdHash sbagliato", func(t *testing.T) {
		aa := newAppAttest(p, "production")
		dev := registerKey(t, aa, testUser, "k1", 0)
		err := aa.VerifyAssertion(testUser, "k1", makeAssertion(t, dev, assertionAuthData("XXXXXXXXXX.evil.app", 1), cdh), cdh)
		if err == nil || !strings.Contains(err.Error(), "rpIdHash") {
			t.Fatalf("errore %v", err)
		}
	})

	t.Run("chiave sconosciuta", func(t *testing.T) {
		aa := newAppAttest(p, "production")
		err := aa.VerifyAssertion(testUser, "nope", []byte{1}, cdh)
		if err != ErrKeyNotFound {
			t.Fatalf("errore %v, atteso ErrKeyNotFound", err)
		}
	})

	t.Run("chiave di un altro utente", func(t *testing.T) {
		aa := newAppAttest(p, "production")
		dev := registerKey(t, aa, "altro-utente", "k1", 0)
		err := aa.VerifyAssertion(testUser, "k1", makeAssertion(t, dev, assertionAuthData(testAppID, 1), cdh), cdh)
		if err != ErrKeyNotFound {
			t.Fatalf("errore %v, atteso ErrKeyNotFound", err)
		}
	})

	t.Run("spazzatura", func(t *testing.T) {
		aa := newAppAttest(p, "production")
		registerKey(t, aa, testUser, "k1", 0)
		if err := aa.VerifyAssertion(testUser, "k1", []byte("non cbor"), cdh); err == nil {
			t.Fatal("CBOR invalido deve fallire")
		}
		short, _ := cbor.Marshal(assertionObject{Signature: []byte{1}, AuthenticatorData: []byte{1, 2}})
		if err := aa.VerifyAssertion(testUser, "k1", short, cdh); err == nil {
			t.Fatal("authenticatorData corto deve fallire")
		}
	})
}

// Il PEM embedded e` davvero la Apple App Attestation Root CA.
func TestEmbeddedAppleRoot(t *testing.T) {
	block, _ := pem.Decode(appleAppAttestationRootPEM)
	if block == nil {
		t.Fatal("PEM non decodificabile")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if cert.Subject.CommonName != "Apple App Attestation Root CA" {
		t.Errorf("CN %q", cert.Subject.CommonName)
	}
	if !cert.IsCA || cert.NotAfter.Year() != 2045 {
		t.Errorf("IsCA=%v NotAfter=%v", cert.IsCA, cert.NotAfter)
	}
	if err := cert.CheckSignatureFrom(cert); err != nil {
		t.Errorf("non e` autofirmato: %v", err)
	}
	sum := sha256.Sum256(cert.Raw)
	const want = "1cb9823ba28ba6ad2d33a006941de2ae4f513ef1d4e831b9f7e0fa7b6242c932"
	if got := hexString(sum[:]); got != strings.ReplaceAll(want, " ", "") {
		t.Errorf("fingerprint %s", got)
	}
	only := x509.NewCertPool()
	only.AddCert(cert)
	if !AppleRoots().Equal(only) {
		t.Error("AppleRoots deve contenere la sola root Apple")
	}
}

func hexString(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, len(b)*2)
	for _, c := range b {
		out = append(out, digits[c>>4], digits[c&15])
	}
	return string(out)
}

func TestPublicKeyRoundTrip(t *testing.T) {
	k := mustKey(t, elliptic.P256())
	der, err := MarshalPublicKey(&k.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParsePublicKey(der)
	if err != nil || !back.Equal(&k.PublicKey) {
		t.Fatalf("round trip fallito: %v", err)
	}
	if _, err := ParsePublicKey([]byte("x")); err == nil {
		t.Error("DER invalido deve fallire")
	}
}

func TestCounterWindow(t *testing.T) {
	var w CounterWindow
	steps := []struct {
		counter uint32
		ok      bool
		max     uint32
	}{
		{0, false, 10},  // mai valido
		{10, false, 10}, // il massimo salvato e` gia` usato
		{12, true, 12},
		{11, true, 12}, // in ordine sparso
		{11, false, 12},
		{12, false, 12},
		{12 + CounterWindowSize, true, 12 + CounterWindowSize},
		{12, false, 12 + CounterWindowSize}, // uscito dalla finestra
		{13, true, 12 + CounterWindowSize},  // ultimo valore ancora dentro
	}
	stored := uint32(10)
	for i, s := range steps {
		next, ok := w.Accept("k", stored, s.counter)
		if ok != s.ok || next != s.max {
			t.Fatalf("passo %d (counter %d): ok=%v max=%d, attesi ok=%v max=%d", i, s.counter, ok, next, s.ok, s.max)
		}
		stored = next
	}
	// Le chiavi sono indipendenti.
	if _, ok := w.Accept("altra", 0, 1); !ok {
		t.Error("una chiave nuova parte da zero")
	}
}
