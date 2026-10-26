package verify_signature

import (
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/tidwall/gjson"
	"mensadb/tools/generic"
	"mensadb/tools/signatures"
	"time"
)

func VerifySignatureHandler(e *core.RequestEvent) error {
	addonId := e.Request.PathValue("addon")
	signature := e.Request.FormValue("signature")
	payload := e.Request.FormValue("payload")

	record, err := e.App.FindRecordById("addons", addonId)
	if err != nil {
		return apis.NewBadRequestError("Invalid addon", err)
	}

	isValid := signatures.ValidateSignature(payload, signature, record.Get("public_key").(string))

	payloadPure := generic.PayloadFromBase64(payload)

	if !gjson.ValidBytes([]byte(payloadPure)) {
		return apis.NewBadRequestError("Invalid payload", nil)
	}

	dataToUse := gjson.ParseBytes([]byte(payloadPure))

	if dataToUse.Get("expires_at").Time().After(time.Now()) &&
		dataToUse.Get("addon_id").String() == addonId &&
		isValid {
		return e.String(200, "OK")
	}
	return apis.NewBadRequestError("Invalid signature", nil)

}
