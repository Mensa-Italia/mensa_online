package keys

import (
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

func GetAddonPublicKeysHandler(e *core.RequestEvent) error {
	addon := e.Request.PathValue("addon")
	record, err := e.App.FindRecordById("addons", addon)
	if err != nil {
		return apis.NewBadRequestError("Invalid addon", err)
	}

	return e.String(200, record.Get("public_key").(string))
}
