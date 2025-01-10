package main

import (
	"github.com/pocketbase/pocketbase/core"
	"mensadb/tools/spatial"
	"strconv"
)

func PositionSetState(e *core.RecordEvent) error {
	lat := e.Record.Get("lat").(float64)
	lon := e.Record.Get("lon").(float64)
	state := spatial.LoadState(lat, lon)
	e.Record.Set("state", state)
	return e.Next()
}

func GetStateHandler(e *core.RequestEvent) error {
	latS := e.Request.URL.Query().Get("lat")
	lonS := e.Request.URL.Query().Get("lon")
	lat, err := strconv.ParseFloat(latS, 64)
	if err != nil {
		return e.String(400, "Invalid latitude")
	}
	lon, err := strconv.ParseFloat(lonS, 64)
	if err != nil {
		return e.String(400, "Invalid longitude")
	}
	state := spatial.LoadState(lat, lon)
	return e.JSON(200, map[string]interface{}{
		"state": state,
	})
}
