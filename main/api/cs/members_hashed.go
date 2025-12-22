package cs

import (
	"mensadb/tools/dbtools"

	"github.com/pocketbase/pocketbase/core"
	"github.com/tidwall/gjson"
)

func MembersHashedHandler(e *core.RequestEvent) error {
	app := e.App

	records, err := app.FindAllRecords("members_registry")
	if err != nil {
		return err
	}

	var finalData []map[string]any = make([]map[string]any, len(records))

	for _, record := range records {
		json, err := record.MarshalJSON()
		if err != nil {
			return err
		}
		elems := gjson.ParseBytes(json)
		finalData = append(finalData, recurseMap(elems.Map()))
	}

	return e.JSON(200, finalData)

}

func recurseMap(data map[string]gjson.Result) map[string]any {
	finalData := make(map[string]any)
	for key, value := range data {
		if value.IsObject() {
			finalData[key] = recurseMap(value.Map())
		} else {
			finalData[key] = dbtools.GetMD5Hash(value.String())
		}
	}
	return finalData
}
