package utilities

import "github.com/pocketbase/pocketbase/core"

type Target struct {
	Namespace              string   `json:"namespace"`
	PackageName            string   `json:"package_name"`
	Sha256CertFingerprints []string `json:"sha256_cert_fingerprints"`
}

type AssetLink struct {
	Relation []string `json:"relation"`
	Target   Target   `json:"target"`
}

func AssetLinksWellKnown(e *core.RequestEvent) error {
	assetLinks := []AssetLink{
		{
			Relation: []string{"delegate_permission/common.handle_all_urls"},
			Target: Target{
				Namespace:              "android_app",
				PackageName:            "it.mensa.app",
				Sha256CertFingerprints: []string{"AE:19:8E:4F:7C:14:7F:83:32:18:BE:00:08:F4:13:3B:5D:99:EA:0D:37:71:7D:26:06:67:93:E8:69:99:03:A0"},
			},
		},
	}

	return e.JSON(200, assetLinks)

}
