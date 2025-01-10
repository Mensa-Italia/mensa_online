package main

import (
	"context"
	"github.com/go-resty/resty/v2"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
	"github.com/tidwall/gjson"
	"sync"
	"time"
)

var lockAddonsData sync.Mutex

func updateAddonsData() {
	successLock := lockAddonsData.TryLock()
	if !successLock { // not able to lock so is already running, abort this run
		return
	}
	defer lockAddonsData.Unlock()
	app.Logger().Info("Updating addons data, this may take a while. Waiting 1 minute before starting for security reasons.")
	time.Sleep(1 * time.Minute)
	app.Logger().Info("Starting to update addons data.")
	query := app.RecordQuery("addons")
	records := []*core.Record{}
	if err := query.All(&records); err != nil {
		return
	}

	for _, record := range records {
		urlToCheck := record.Get("url").(string) + "/mensadata.json"
		if urlToCheck == "" {
			setInvalid(record)
			continue
		}
		get, err := resty.New().R().Get(urlToCheck)
		if err != nil {
			setInvalid(record)
			return
		}
		if get.StatusCode() != 200 {
			setInvalid(record)
			return
		}
		if get.Body() == nil {
			setInvalid(record)
			return
		}
		dataToUse := gjson.ParseBytes(get.Body())
		if dataToUse.Get("id").String() != record.GetString("id") {
			setInvalid(record)
			return
		}
		record.Set("name", dataToUse.Get("name").String())
		record.Set("description", dataToUse.Get("description").String())
		record.Set("version", dataToUse.Get("version").String())

		err = app.Save(record)
		if err != nil {
			setInvalid(record)
			return
		}

		fileImage, err := filesystem.NewFileFromURL(context.Background(), dataToUse.Get("icon").String())
		if err == nil {
			record.Set("icon", fileImage)
		}

		record2, err := app.FindRecordById("addons", record.Id)
		if err != nil {
			setInvalid(record)
			return
		}

		if record2.GetString("name") != "" && record2.GetString("description") != "" && record2.GetString("version") != "" && record2.GetString("icon") != "" {
			record2.Set("is_ready", true)
			err = app.Save(record2)
			if err != nil {
				return
			}
		} else {
			record2.Set("is_ready", false)
			err = app.Save(record2)
			if err != nil {
				return
			}
		}
	}

}

func setInvalid(record *core.Record) {
	record.Set("is_ready", false)
	err := app.Save(record)
	if err != nil {
		return
	}
}
