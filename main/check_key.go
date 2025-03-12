package main

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"strings"
)

func GetKeyAppId(key string) (string, error) {
	key = strings.ReplaceAll(key, "Bearer ", "")
	key = strings.TrimSpace(key)
	collection, err := app.FindCollectionByNameOrId("ex_keys")
	if err != nil {
		return "", err
	}
	record, err := app.FindAllRecords(collection,
		dbx.NewExp(`key = {:key}`, dbx.Params{"key": key}),
	)
	if err != nil {
		return "", err
	}
	if len(record) == 0 {
		return "", nil
	}
	return record[0].GetString("ex_app"), nil
}

func CheckKey(key, requiredPerm string) bool {
	key = strings.ReplaceAll(key, "Bearer ", "")
	key = strings.TrimSpace(key)
	collection, err := app.FindCollectionByNameOrId("ex_keys")
	if err != nil {
		return false
	}
	record, err := app.FindAllRecords(collection,
		dbx.NewExp(`key = {:key}`, dbx.Params{"key": key}),
	)
	if err != nil {
		return false
	}
	if len(record) == 0 {
		return false
	}
	permsOnRecord := record[0].GetStringSlice("permissions")
	for _, perm := range permsOnRecord {
		if strings.ToUpper(perm) == strings.ToUpper(requiredPerm) {
			return true
		}
	}
	return false
}

func OnKeyCreated(e *core.RecordEvent) error {
	uuidUnique := uuid.New().String()
	e.Record.Set("key", "sk_"+Sha256Hash(uuidUnique))
	return e.Next()
}

func Sha256Hash(input string) string {
	hash := sha256.Sum256([]byte(input))
	return hex.EncodeToString(hash[:])
}
