package generic

import "encoding/base64"

func PayloadFromBase64(payload string) string {
	decoded, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return ""
	}

	return string(decoded)
}
