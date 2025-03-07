package main

import (
	"github.com/pocketbase/dbx"
	"strings"
)

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
