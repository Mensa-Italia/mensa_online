package main

import (
	"github.com/pocketbase/pocketbase/core"
	"mensadb/tools/signatures"
)

func GeneratePublicPrivateKeys(e *core.RecordEvent) error {
	keyPub, keyPriv := signatures.GenerateKeyPairs()
	e.Record.Set("public_key", keyPub)
	if err := app.Save(e.Record); err != nil {
		return e.Next()
	}

	collection, err := app.FindCollectionByNameOrId("addons_private_keys")
	if err != nil {
		return err
	}
	record := core.NewRecord(collection)
	record.Set("private_key", keyPriv)
	record.Set("addon", e.Record.Id)
	err = app.Save(record)
	return e.Next()
}
