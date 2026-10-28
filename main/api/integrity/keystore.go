package integrity

import (
	"database/sql"
	"encoding/base64"
	"errors"
	"sync"

	"github.com/pocketbase/pocketbase/core"

	tools "mensadb/tools/integrity"
)

// pbKeyStore implementa tools.KeyStore sulla collection app_attest_keys
// (solo superuser: la legge e la scrive solo il codice del server).
type pbKeyStore struct {
	app core.App

	// mu serializza AdvanceCounter: due richieste concorrenti con la stessa
	// asserzione non devono passare entrambe il controllo "counter > salvato".
	// Il backend e` single-instance, quindi un mutex di processo basta.
	mu sync.Mutex
}

func newPBKeyStore(app core.App) *pbKeyStore { return &pbKeyStore{app: app} }

func (s *pbKeyStore) Find(keyID string) (*tools.Key, error) {
	rec, err := s.app.FindFirstRecordByData(appAttestKeysCollection, "key_id", keyID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, tools.ErrKeyNotFound
		}
		return nil, err
	}
	return keyFromRecord(rec)
}

func (s *pbKeyStore) Create(k *tools.Key) error {
	collection, err := s.app.FindCachedCollectionByNameOrId(appAttestKeysCollection)
	if err != nil {
		return err
	}
	der, err := tools.MarshalPublicKey(k.PublicKey)
	if err != nil {
		return err
	}
	rec := core.NewRecord(collection)
	rec.Set("user", k.UserID)
	rec.Set("key_id", k.KeyID)
	rec.Set("public_key", base64.StdEncoding.EncodeToString(der))
	rec.Set("sign_counter", int(k.Counter))
	rec.Set("environment", k.Environment)
	return s.app.Save(rec)
}

func (s *pbKeyStore) AdvanceCounter(keyID string, counter uint32) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec, err := s.app.FindFirstRecordByData(appAttestKeysCollection, "key_id", keyID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return tools.ErrKeyNotFound
		}
		return err
	}
	if counter <= uint32(rec.GetInt("sign_counter")) {
		return tools.ErrCounterNotAdvanced
	}
	rec.Set("sign_counter", int(counter))
	return s.app.Save(rec)
}

func keyFromRecord(rec *core.Record) (*tools.Key, error) {
	der, err := base64.StdEncoding.DecodeString(rec.GetString("public_key"))
	if err != nil {
		return nil, err
	}
	pub, err := tools.ParsePublicKey(der)
	if err != nil {
		return nil, err
	}
	return &tools.Key{
		KeyID:       rec.GetString("key_id"),
		UserID:      rec.GetString("user"),
		PublicKey:   pub,
		Counter:     uint32(rec.GetInt("sign_counter")),
		Environment: rec.GetString("environment"),
	}, nil
}
