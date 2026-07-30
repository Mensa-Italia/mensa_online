package utilities

import (
	"mensadb/tools/env"

	"github.com/pocketbase/pocketbase/core"
)

// Fingerprint SHA-256 della chiave di firma di release di it.mensa.app.
const androidReleaseSHA256 = "AE:19:8E:4F:7C:14:7F:83:32:18:BE:00:08:F4:13:3B:5D:99:EA:0D:37:71:7D:26:06:67:93:E8:69:99:03:A0"

const androidPackageName = "it.mensa.app"

type Target struct {
	Namespace              string   `json:"namespace"`
	PackageName            string   `json:"package_name"`
	Sha256CertFingerprints []string `json:"sha256_cert_fingerprints"`
}

type AssetLink struct {
	Relation []string `json:"relation"`
	Target   Target   `json:"target"`
}

// AssetLinksWellKnown serve /.well-known/assetlinks.json.
//
// Due relation distinte, con significati diversi:
//   - handle_all_urls: app link, cioe` "questa app puo` aprire i nostri URL";
//   - get_login_creds: richiesto da Android Credential Manager perche` l'app
//     possa creare e usare passkey con rpId svc.mensa.it. Senza questa voce il
//     prompt di sistema non compare affatto, e l'errore lato app e` opaco.
//
// La relation delle passkey include anche gli eventuali fingerprint di debug
// configurati via ANDROID_DEBUG_SHA256, perche` le build di sviluppo sono
// firmate con un keystore diverso da quello di release. In produzione la
// variabile resta vuota e la lista contiene solo la chiave di release.
func AssetLinksWellKnown(e *core.RequestEvent) error {
	assetLinks := []AssetLink{
		{
			Relation: []string{"delegate_permission/common.handle_all_urls"},
			Target: Target{
				Namespace:              "android_app",
				PackageName:            androidPackageName,
				Sha256CertFingerprints: []string{androidReleaseSHA256},
			},
		},
		{
			Relation: []string{"delegate_permission/common.get_login_creds"},
			Target: Target{
				Namespace:              "android_app",
				PackageName:            androidPackageName,
				Sha256CertFingerprints: append([]string{androidReleaseSHA256}, env.GetAndroidDebugSHA256()...),
			},
		},
	}

	return e.JSON(200, assetLinks)
}
