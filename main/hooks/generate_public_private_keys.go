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
	if err := e.App.Save(record); err != nil {
		e.App.Logger().Error("save record failed", "collection", record.Collection().Name, "addon", e.Record.Id, "err", err)
	}
	return e.Next()
}
