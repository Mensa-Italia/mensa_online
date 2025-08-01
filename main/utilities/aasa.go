package utilities

import (
	"github.com/pocketbase/pocketbase/core"
)

type Detail struct {
	AppID string   `json:"appID"`
	Paths []string `json:"paths"`
}

type AppLinks struct {
	Details []Detail `json:"details"`
}

type AASA struct {
	AppLinks AppLinks `json:"applinks"`
}

func AASAWellKnown(e *core.RequestEvent) error {
	aasa := AASA{
		AppLinks: AppLinks{
			Details: []Detail{
				{
					AppID: "6WA5D3RJBU.it.mensa.app",
					Paths: []string{"/links/*"},
				},
			},
		},
	}

	return e.JSON(200, aasa)
}
