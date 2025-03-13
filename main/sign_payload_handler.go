package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"mensadb/tools/dbtools"
	"mensadb/tools/signatures"
	"slices"
	"time"
)

func SignPayloadHandler(e *core.RequestEvent) error {

	isLogged, authUser := dbtools.UserIsLoggedIn(e)
	if !isLogged {
		return apis.NewUnauthorizedError("Unauthorized", errors.New("Unauthorized"))
	}

	addonsId := e.Request.PathValue("addon")

	user, err := e.App.FindRecordById("users", authUser.Id)
	if err != nil {
		return err
	}

	payloadJSON := map[string]interface{}{
		"id":                user.Get("id"),
		"email":             user.Get("email"),
		"name":              user.Get("name"),
		"avatar":            "https://svc.mensa.it/api/files/_pb_users_auth_/" + user.Get("id").(string) + "/" + user.Get("avatar").(string),
		"powers":            user.Get("powers"),
		"expire_membership": user.Get("expire_membership"),
		"addon_id":          addonsId,
		"signed_at":         time.Now().Format(time.RFC3339),
		"expires_at":        time.Now().Add(time.Minute * 5).Format(time.RFC3339),
	}

	if !slices.Contains(user.GetStringSlice("addons"), addonsId) {
		user.Set("addons", append(user.GetStringSlice("addons"), addonsId))
		err = e.App.Save(user)
		if err != nil {
			return err
		}
	}

	payload, _ := json.Marshal(payloadJSON)

	record, err := e.App.FindFirstRecordByData("addons_private_keys", "addon", addonsId)
	if err != nil {
		return apis.NewBadRequestError("Invalid addon", err)
	}

	payloadBase64 := payloadToBase64(string(payload))
	signature, err := signatures.SignData([]byte(payloadBase64), record.Get("private_key").(string))

	if err != nil {
		return apis.NewBadRequestError("Failed to sign payload", err)
	}

	return e.JSON(200, map[string]interface{}{
		"payload":   payloadBase64,
		"signature": signature,
	})
}

func payloadToBase64(payload string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func payloadFromBase64(payload string) string {
	decoded, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return ""
	}

	return string(decoded)
}
