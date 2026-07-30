package utilities

import (
	"github.com/pocketbase/pocketbase/core"
)

// appID nel formato <TeamID>.<BundleID> richiesto da Apple.
const appleAppID = "6WA5D3RJBU.it.mensa.app"

type Detail struct {
	AppID string   `json:"appID"`
	Paths []string `json:"paths"`
}

type AppLinks struct {
	Details []Detail `json:"details"`
}

// WebCredentials abilita le passkey (e il password autofill) per questo dominio
// sull'app elencata. iOS lo legge tramite l'entitlement Associated Domains
// "webcredentials:svc.mensa.it": senza questo blocco, ASAuthorization non
// presenta alcun prompt e l'errore lato app non spiega il perche`.
type WebCredentials struct {
	Apps []string `json:"apps"`
}

type AASA struct {
	AppLinks       AppLinks       `json:"applinks"`
	WebCredentials WebCredentials `json:"webcredentials"`
}

func AASAWellKnown(e *core.RequestEvent) error {
	aasa := AASA{
		AppLinks: AppLinks{
			Details: []Detail{
				{
					AppID: appleAppID,
					Paths: []string{"/links/*"},
				},
			},
		},
		WebCredentials: WebCredentials{
			Apps: []string{appleAppID},
		},
	}

	return e.JSON(200, aasa)
}
