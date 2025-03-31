package hooks

import (
	"github.com/pocketbase/pocketbase/core"
	"mensadb/tools/signatures"
)

func GeneratePublicPrivateKeys(e *core.RecordEvent) error {
	keyPub, keyPriv := signatures.GenerateKeyPairs()
	e.Record.Set("public_key", keyPub)
	if err := e.App.Save(e.Record); err != nil {
		return e.Next()
	}

	collection, err := e.App.FindCollectionByNameOrId("addons_private_keys")
	if err != nil {
		return err
	}
	record := core.NewRecord(collection)
	record.Set("private_key", keyPriv)
	record.Set("addon", e.Record.Id)
	err = e.App.Save(record)
	return e.Next()
}
