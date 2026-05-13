package dbtools

import (
	"slices"

	"github.com/pocketbase/pocketbase/core"
)

// HasPower reports whether the user record has the named power, or the
// "super" override. Empty required string means no power needed.
func HasPower(userRec *core.Record, required string) bool {
	if required == "" {
		return true
	}
	if userRec == nil {
		return false
	}
	powers := userRec.GetStringSlice("powers")
	return slices.Contains(powers, "super") || slices.Contains(powers, required)
}
