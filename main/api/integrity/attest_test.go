package integrity

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/pocketbase/pocketbase/core"

	_ "mensadb/migrations"
	tools "mensadb/tools/integrity"
)

// Questi test usano un DB PocketBase vero (migrazioni incluse) per il key
// store e per l'endpoint di attest. Con Go 1.27 serve GOEXPERIMENT=nojsonv2
// (vedi migrations).

const attestAppID = "6WA5D3RJBU.it.mensa.app"

func migratedApp(t *testing.T) core.App {
	t.Helper()
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.ResetBootstrapState() })
	if err := app.RunAllMigrations(); err != nil {
		t.Fatal(err)
	}
	return app
}

func createUser(t *testing.T, app core.App, id string) *core.Record {
	t.Helper()
	users, _ := app.FindCollectionByNameOrId("users")
	r := core.NewRecord(users)
	r.Id = id
	r.SetEmail(id + "@example.com")
	r.SetPassword("password-di-prova")
	r.Set("username", "user_"+id)
	r.Set("name", "Utente "+id)
	if err := app.Save(r); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestPBKeyStore(t *testing.T) {
	app := migratedApp(t)
	user := createUser(t, app, "usera0000000001")
	store := newPBKeyStore(app)

	if _, err := store.Find("nope"); err != tools.ErrKeyNotFound {
		t.Fatalf("chiave assente: %v", err)
	}

	dev, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	key := &tools.Key{KeyID: "a2V5LWlk", UserID: user.Id, PublicKey: &dev.PublicKey, Counter: 0, Environment: "production"}
	if err := store.Create(key); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(key); err == nil {
		t.Error("key_id e` unico: la seconda Create deve fallire")
	}

	got, err := store.Find("a2V5LWlk")
	if err != nil || got.UserID != user.Id || !got.PublicKey.Equal(&dev.PublicKey) || got.Counter != 0 || got.Environment != "production" {
		t.Fatalf("Find: %+v %v", got, err)
	}

	if err := store.AdvanceCounter("a2V5LWlk", 5); err != nil {
		t.Fatal(err)
	}
	for _, c := range []uint32{5, 4, 0} {
		if err := store.AdvanceCounter("a2V5LWlk", c); err != tools.ErrCounterNotAdvanced {
			t.Errorf("counter %d: %v, atteso ErrCounterNotAdvanced", c, err)
		}
	}
	if got, _ := store.Find("a2V5LWlk"); got.Counter != 5 {
		t.Errorf("counter salvato %d", got.Counter)
	}
	if err := store.AdvanceCounter("nope", 1); err != tools.ErrKeyNotFound {
		t.Errorf("chiave assente: %v", err)
	}
}

// --- attestazione sintetica ------------------------------------------------

type nonceExt struct {
	Nonce []byte `asn1:"tag:1,explicit"`
}

func syntheticAttestation(t *testing.T, challenge string) (keyID, attestation []byte, rootPool *x509.CertPool) {
	t.Helper()
	newKey := func(c elliptic.Curve) *ecdsa.PrivateKey {
		k, err := ecdsa.GenerateKey(c, rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		return k
	}
	create := func(tpl, parent *x509.Certificate, pub *ecdsa.PublicKey, signer *ecdsa.PrivateKey) *x509.Certificate {
		der, err := x509.CreateCertificate(rand.Reader, tpl, parent, pub, signer)
		if err != nil {
			t.Fatal(err)
		}
		c, _ := x509.ParseCertificate(der)
		return c
	}
	ca := func(serial int64, cn string) *x509.Certificate {
		return &x509.Certificate{
			SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: cn},
			NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
			IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
		}
	}

	rootKey, interKey, dev := newKey(elliptic.P384()), newKey(elliptic.P384()), newKey(elliptic.P256())
	rootTpl := ca(1, "root")
	root := create(rootTpl, rootTpl, &rootKey.PublicKey, rootKey)
	inter := create(ca(2, "inter"), root, &interKey.PublicKey, rootKey)
	rootPool = x509.NewCertPool()
	rootPool.AddCert(root)

	point, _ := dev.PublicKey.ECDH()
	pt := point.Bytes()
	h := sha256.Sum256(pt)
	keyID = h[:]

	authData := append([]byte{}, func() []byte { s := sha256.Sum256([]byte(attestAppID)); return s[:] }()...)
	authData = append(authData, 0x40, 0, 0, 0, 0)
	authData = append(authData, append([]byte("appattest"), make([]byte, 7)...)...)
	authData = binary.BigEndian.AppendUint16(authData, uint16(len(keyID)))
	authData = append(authData, keyID...)

	cdh := sha256.Sum256([]byte(challenge))
	nonce := sha256.Sum256(append(append([]byte{}, authData...), cdh[:]...))
	extVal, _ := asn1.Marshal(nonceExt{Nonce: nonce[:]})
	leaf := create(&x509.Certificate{
		SerialNumber: big.NewInt(3), Subject: pkix.Name{CommonName: "device"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		ExtraExtensions: []pkix.Extension{{Id: asn1.ObjectIdentifier{1, 2, 840, 113635, 100, 8, 2}, Value: extVal}},
	}, inter, &dev.PublicKey, interKey)

	obj, err := cbor.Marshal(map[string]any{
		"fmt":      "apple-appattest",
		"attStmt":  map[string]any{"x5c": [][]byte{leaf.Raw, inter.Raw}, "receipt": []byte("r")},
		"authData": authData,
	})
	if err != nil {
		t.Fatal(err)
	}
	return keyID, obj, rootPool
}

func postJSON(t *testing.T, app core.App, h func(*core.RequestEvent) error, auth *core.Record, body map[string]any) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/integrity/x", strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e := new(core.RequestEvent)
	e.App, e.Request, e.Response, e.Auth = app, req, rec, auth
	if err := h(e); err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

// Challenge + attest end-to-end: la chiave attestata viene registrata per
// l'utente e poi usata per un'asserzione; i rifiuti sono 403 integrity_failed.
func TestChallengeAndAttestEndpoints(t *testing.T) {
	app := migratedApp(t)
	user := createUser(t, app, "usera0000000001")
	other := createUser(t, app, "userb0000000001")

	// Il Gate usa una root di prova al posto di quella Apple (impostata in attest).
	gate := &tools.Gate{Challenges: tools.NewChallengeStore()}
	current.Store(gate)
	t.Cleanup(func() { current.Store(nil) })

	issue := func(u *core.Record) string {
		code, out := postJSON(t, app, challengeHandler, u, map[string]any{})
		if code != 200 || out["expires_in"] != float64(120) {
			t.Fatalf("challenge: %d %v", code, out)
		}
		ch, _ := out["challenge"].(string)
		if raw, err := base64.RawURLEncoding.DecodeString(ch); err != nil || len(raw) != 32 {
			t.Fatalf("challenge malformata: %q", ch)
		}
		return ch
	}

	attest := func(u *core.Record, challenge string, keyID, att []byte, pool *x509.CertPool) (int, map[string]any) {
		gate.IOS = &tools.AppAttest{AppID: attestAppID, Environment: "production", Roots: pool, Keys: newPBKeyStore(app)}
		return postJSON(t, app, iosAttestHandler, u, map[string]any{
			"key_id":      base64.StdEncoding.EncodeToString(keyID),
			"attestation": base64.StdEncoding.EncodeToString(att),
			"challenge":   challenge,
		})
	}

	// successo
	ch := issue(user)
	keyID, att, pool := syntheticAttestation(t, ch)
	if code, out := attest(user, ch, keyID, att, pool); code != 200 || out["ok"] != true {
		t.Fatalf("attest: %d %v", code, out)
	}
	stored, err := newPBKeyStore(app).Find(base64.StdEncoding.EncodeToString(keyID))
	if err != nil || stored.UserID != user.Id || stored.Counter != 0 || stored.Environment != "production" {
		t.Fatalf("chiave salvata: %+v %v", stored, err)
	}

	// la challenge e` monouso: stesso body di nuovo
	if code, out := attest(user, ch, keyID, att, pool); code != 403 || out["code"] != "integrity_failed" {
		t.Errorf("replay: %d %v", code, out)
	}
	// challenge di un altro utente
	ch2 := issue(user)
	keyID2, att2, pool2 := syntheticAttestation(t, ch2)
	if code, out := attest(other, ch2, keyID2, att2, pool2); code != 403 || out["code"] != "integrity_failed" {
		t.Errorf("challenge altrui: %d %v", code, out)
	}
	// attestazione costruita su un'altra challenge (nonce non corrisponde)
	ch3 := issue(user)
	keyID3, att3, pool3 := syntheticAttestation(t, "un-altra-challenge")
	if code, out := attest(user, ch3, keyID3, att3, pool3); code != 403 || out["code"] != "integrity_failed" {
		t.Errorf("nonce sbagliato: %d %v", code, out)
	}
	// root non fidata
	ch4 := issue(user)
	keyID4, att4, _ := syntheticAttestation(t, ch4)
	if code, out := attest(user, ch4, keyID4, att4, x509.NewCertPool()); code != 403 || out["code"] != "integrity_failed" {
		t.Errorf("root non fidata: %d %v", code, out)
	}
	// spazzatura
	ch5 := issue(user)
	if code, out := attest(user, ch5, []byte("corto"), []byte("x"), pool); code != 403 || out["code"] != "integrity_failed" {
		t.Errorf("key_id malformato: %d %v", code, out)
	}
	// I rifiuti non toccano la chiave gia` registrata.
	if _, err := newPBKeyStore(app).Find(base64.StdEncoding.EncodeToString(keyID)); err != nil {
		t.Errorf("la chiave registrata deve restare: %v", err)
	}
}
