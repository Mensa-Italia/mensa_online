package cs

import (
	"mensadb/tools/dbtools"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/tidwall/gjson"
)

func MembersHashedHandler(e *core.RequestEvent) error {
	app := e.App

	records, err := app.FindAllRecords("members_registry", dbx.NewExp("is_active = true"))
	if err != nil {
		return err
	}

	var finalData []map[string]any = make([]map[string]any, 0)

	for _, record := range records {
		json, err := record.MarshalJSON()
		if err != nil {
			return err
		}
		elems := gjson.ParseBytes(json)
		data := recurseMap(elems.Map(), record.Id)
		data["id"] = record.Id
		finalData = append(finalData, data)
	}

	return e.JSON(200, finalData)

}

func recurseMap(data map[string]gjson.Result, salt string) map[string]any {
	finalData := make(map[string]any)
	for key, value := range data {
		if value.IsObject() {
			finalData[dbtools.NormalizeTextForHash(key)] = recurseMap(value.Map(), salt)
		} else {
			finalData[dbtools.NormalizeTextForHash(key)] = dbtools.GetMD5Hash(value.String(), salt)
		}
	}
	return finalData
}
