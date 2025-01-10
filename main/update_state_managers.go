package main

import (
	"mensadb/importers"
	"slices"
	"sync"
	"time"
)

var lockStateManagers sync.Mutex

func updateStateManagers() {
	successLock := lockStateManagers.TryLock()
	if !successLock { // not able to lock so is already running, abort this run
		return
	}
	defer lockStateManagers.Unlock()
	app.Logger().Info("Updating states managers permissions, this may take a while. Waiting 1 minute before starting for security reasons.")
	time.Sleep(1 * time.Minute)

	records, err := app.FindRecordsByFilter("users", "powers:length > -1", "-created", -1, 0)
	if err != nil {
		return
	}
	segretari := importers.RetrieveForwardedMail("segretari")
	for _, record := range records {
		powers := record.GetStringSlice("powers")
		newPowers := []string{}
		hadEventsPower := false
		hasEventsPower := slices.Contains(segretari, record.GetString("email"))
		for _, power := range powers {
			if power == "events" {
				hadEventsPower = true
				continue
			}
			newPowers = append(newPowers, power)
		}
		if hasEventsPower {
			newPowers = append(newPowers, "events")
		}
		if hasEventsPower != hadEventsPower {
			record.Set("powers", newPowers)
			_ = app.Save(record)
		}
	}
}
