package position

import (
	"github.com/pocketbase/pocketbase/core"
	"mensadb/tools/spatial"
	"strconv"
)

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
